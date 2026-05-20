package installer_test

import (
	"path/filepath"
	"testing"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/filesystem"
	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/installer"
	"github.com/basti-fantasti/bossy/internal/core/services/packages"
	"github.com/basti-fantasti/bossy/pkg/consts"
)

// TestEnsureDependency_AliasRoundTripToDisk verifies the full alias-expansion
// pipeline: user-supplied "gtr:foo/bar" → EnsureDependency → PackageService.Save
// to bossy.json → PackageService.Load. The dep key on disk and after reload
// must be the canonical form (SSH or host/path), never the alias prefix.
//
// This complements the in-memory unit tests in utils_test.go by exercising
// the JSON persistence layer with the real FilePackageRepository, the real
// PackageService, and a real on-disk filesystem in a temp directory.
func TestEnsureDependency_AliasRoundTripToDisk(t *testing.T) {
	cases := []struct {
		name     string
		protocol string // "" means absent (defaults to https)
		wantKey  string
	}{
		{
			name:     "ssh protocol writes git@host:path",
			protocol: "ssh",
			wantKey:  "git@gitlab.example.com:foo/bar",
		},
		{
			name:     "https protocol writes host/path",
			protocol: "https",
			wantKey:  "gitlab.example.com/foo/bar",
		},
		{
			name:     "absent protocol defaults to host/path",
			protocol: "",
			wantKey:  "gitlab.example.com/foo/bar",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runAliasRoundTrip(t, tc.protocol, tc.wantKey)
		})
	}
}

func runAliasRoundTrip(t *testing.T, protocol, wantKey string) {
	t.Helper()
	cfg := setupIsolatedConfig(t)
	cfg.Aliases["gtr"] = "gitlab.example.com"
	if protocol != "" {
		cfg.HostProtocols["gitlab.example.com"] = protocol
	}

	// Wire the real PackageService against a real on-disk filesystem
	// rooted at a fresh temp project directory.
	projectDir := t.TempDir()
	bossyPath := filepath.Join(projectDir, consts.FilePackage)

	fs := filesystem.NewOSFileSystem()
	svc := packages.NewPackageService(
		repository.NewFilePackageRepository(fs),
		repository.NewFileLockRepository(fs),
	)

	pkg := domain.NewPackage()
	installer.EnsureDependency(pkg, []string{"gtr:foo/bar"})

	// Sanity: the alias prefix must not be present even in memory.
	if _, leaked := pkg.Dependencies["gtr:foo/bar"]; leaked {
		t.Fatalf("alias prefix leaked into in-memory Dependencies: %v", pkg.Dependencies)
	}

	if err := svc.Save(pkg, bossyPath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := svc.Load(bossyPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertCanonicalDep(t, reloaded.Dependencies, wantKey)
}

func assertCanonicalDep(t *testing.T, deps map[string]string, wantKey string) {
	t.Helper()
	if _, ok := deps[wantKey]; !ok {
		t.Errorf("reloaded Dependencies missing canonical key %q; got %v", wantKey, deps)
	}
	if _, leaked := deps["gtr:foo/bar"]; leaked {
		t.Errorf("alias prefix %q survived persistence round-trip; deps=%v", "gtr:foo/bar", deps)
	}
	if len(deps) != 1 {
		t.Errorf("expected exactly 1 dependency after reload, got %d: %v", len(deps), deps)
	}
}
