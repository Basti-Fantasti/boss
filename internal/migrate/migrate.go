// Package migrate handles one-shot upgrades from upstream `boss` state
// to the renamed `bossy` layout.
package migrate

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MigrateHome copies $home/.boss to $home/.bossy if the legacy dir exists
// and the new dir does not. Returns true if migration ran.
func MigrateHome(home string) (bool, error) {
	legacy := filepath.Join(home, ".boss")
	target := filepath.Join(home, ".bossy")
	if _, err := os.Stat(target); err == nil {
		// Target exists — still rename the legacy config file inside it if present.
		_, _ = RenameIfMissing(filepath.Join(target, "boss.cfg.json"), filepath.Join(target, "bossy.cfg.json"))
		return false, nil
	}
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return false, nil
	}
	if err := copyTree(legacy, target); err != nil {
		return false, err
	}
	// After copy, rename the config file to the new name.
	_, _ = RenameIfMissing(filepath.Join(target, "boss.cfg.json"), filepath.Join(target, "bossy.cfg.json"))
	return true, nil
}

// RenameIfMissing renames src → dst when src exists and dst does not. Returns
// true if a rename occurred. No-op (returns false, nil) otherwise.
func RenameIfMissing(src, dst string) (bool, error) {
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.Rename(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

// MigrateProjectFiles renames legacy project files in dir to their bossy-* names:
//   - boss.json      → bossy.json
//   - boss-lock.json → bossy-lock.json
//
// Each rename only happens if the legacy file exists and the new one does not.
// Returns true if anything was renamed.
func MigrateProjectFiles(dir string) (bool, error) {
	changed := false
	for _, pair := range [...][2]string{
		{"boss.json", "bossy.json"},
		{"boss-lock.json", "bossy-lock.json"},
	} {
		renamed, err := RenameIfMissing(filepath.Join(dir, pair[0]), filepath.Join(dir, pair[1]))
		if err != nil {
			return changed, err
		}
		if renamed {
			changed = true
		}
	}
	return changed, nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, info.Mode())
		}
		in, err := os.Open(path) // #nosec G304 -- migrating known config tree
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		f, err := os.Create(out) // #nosec G304
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, in)
		return err
	})
}

// MigrateBossJSON rewrites legacy `:ssh` version-suffix entries in a
// bossy.json (or legacy boss.json) file to canonical SSH-form keys. Returns
// true if the file was modified. Idempotent.
func MigrateBossJSON(path string) (bool, error) {
	raw, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, err
	}
	depsRaw, ok := doc["dependencies"]
	if !ok {
		return false, nil
	}
	var deps map[string]string
	if err := json.Unmarshal(depsRaw, &deps); err != nil {
		return false, err
	}
	changed := false
	newDeps := make(map[string]string, len(deps))
	for k, v := range deps {
		if strings.Contains(v, ":ssh") {
			// Strip the `:ssh` suffix (and any trailing colon) from the
			// version string; convert the `host/path` key to `git@host:path`.
			cleanedVer := strings.TrimSuffix(strings.ReplaceAll(v, ":ssh", ""), ":")
			if cleanedVer == "" {
				cleanedVer = "*"
			}
			host, p, ok := strings.Cut(k, "/")
			if ok {
				newKey := "git@" + host + ":" + p
				newDeps[newKey] = cleanedVer
				changed = true
				continue
			}
		}
		newDeps[k] = v
	}
	if !changed {
		return false, nil
	}
	newDepsRaw, err := json.MarshalIndent(newDeps, "", "  ")
	if err != nil {
		return false, err
	}
	doc["dependencies"] = newDepsRaw
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, out, 0600)
}
