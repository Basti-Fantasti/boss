package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateHome_Copies(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".boss")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "boss.cfg.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	moved, err := MigrateHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("expected migration to run")
	}
	if _, err := os.Stat(filepath.Join(home, ".bossy", "bossy.cfg.json")); err != nil {
		t.Fatalf("expected file copied & renamed to .bossy/bossy.cfg.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bossy", "boss.cfg.json")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy boss.cfg.json to be removed, stat err=%v", err)
	}
}

func TestMigrateProjectFiles_RenamesBothFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "boss.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "boss-lock.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateProjectFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rename to occur")
	}
	if _, err := os.Stat(filepath.Join(dir, "bossy.json")); err != nil {
		t.Errorf("bossy.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bossy-lock.json")); err != nil {
		t.Errorf("bossy-lock.json missing: %v", err)
	}
}

func TestMigrateProjectFiles_NoLegacy(t *testing.T) {
	dir := t.TempDir()
	changed, err := MigrateProjectFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no-op when legacy files absent")
	}
}

func TestMigrateProjectFiles_NewExistsTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "boss.json"), []byte(`{"legacy":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bossy.json"), []byte(`{"new":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateProjectFiles(dir); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "bossy.json"))
	if !contains(string(out), `"new":true`) {
		t.Errorf("bossy.json must not be overwritten by legacy: %s", out)
	}
}

func TestMigrateHome_Idempotent(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".bossy"), 0700); err != nil {
		t.Fatal(err)
	}
	moved, err := MigrateHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Fatal("expected no migration when target already exists")
	}
}

func TestMigrateHome_NoLegacy(t *testing.T) {
	home := t.TempDir()
	moved, err := MigrateHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Fatal("nothing to migrate")
	}
}

func TestMigrateBossJSON_RewritesSSHSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boss.json")
	input := `{
        "name":"app",
        "version":"1.0.0",
        "dependencies": {
            "gitlab.mydomain.com/g/r": "*:ssh",
            "horse": "^1.0.0"
        }
    }`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateBossJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rewrite")
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	if !contains(s, `"git@gitlab.mydomain.com:g/r"`) {
		t.Errorf("expected SSH key rewrite; got %s", s)
	}
	if contains(s, ":ssh") {
		t.Errorf(":ssh suffix not stripped: %s", s)
	}
	if !contains(s, `"horse"`) {
		t.Errorf("non-affected dep dropped: %s", s)
	}
}

func TestMigrateBossJSON_NoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boss.json")
	input := `{"name":"x","version":"1","dependencies":{"horse":"*"}}`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateBossJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change")
	}
}

// TestReloadAfterMigration documents the expected behavior when MigrateHome runs
// after package init in pkg/env has already loaded a fresh default config from
// the not-yet-existing ~/.bossy directory. The fix calls env.ReloadGlobalConfiguration()
// inside runMigrations (internal/adapters/primary/cli/root.go) immediately after
// MigrateHome returns moved == true, so the in-memory globalConfiguration is
// replaced with the just-migrated on-disk data before any command runs.
// A true integration test is impractical here because pkg/env globals cannot be
// safely reset between test runs in the same process; the real guard is the call
// site in root.go.
func TestReloadAfterMigration(t *testing.T) {
	t.Skip("integration test — see runMigrations in internal/adapters/primary/cli/root.go")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
