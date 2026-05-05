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
		return false, nil
	}
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return false, nil
	}
	if err := copyTree(legacy, target); err != nil {
		return false, err
	}
	return true, nil
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
// boss.json file to canonical SSH-form keys. Returns true if the file
// was modified. Idempotent.
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
