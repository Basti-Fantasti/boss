package installer_test

import (
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/installer"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// setupIsolatedConfig redirects BOSS_HOME to a temp dir and reloads the
// global configuration so tests can freely mutate it without touching
// the user's real config. Returns the freshly loaded configuration.
func setupIsolatedConfig(t *testing.T) *env.Configuration {
	t.Helper()
	t.Setenv("BOSS_HOME", t.TempDir())
	env.ReloadGlobalConfiguration()
	cfg := env.GlobalConfiguration()
	cfg.Aliases = map[string]string{}
	cfg.HostProtocols = map[string]string{}
	return cfg
}

func TestNormalizeDepArg_Alias_SSH(t *testing.T) {
	cfg := setupIsolatedConfig(t)
	cfg.Aliases["gtr"] = "gitlab.mydomain.com"
	cfg.HostProtocols["gitlab.mydomain.com"] = "ssh"

	key, ok := installer.NormalizeDepKey("gtr:foo/bar")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "git@gitlab.mydomain.com:foo/bar"; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestNormalizeDepArg_Alias_HTTPS(t *testing.T) {
	cfg := setupIsolatedConfig(t)
	cfg.Aliases["gtr"] = "gitlab.mydomain.com"
	cfg.HostProtocols["gitlab.mydomain.com"] = "https"

	key, ok := installer.NormalizeDepKey("gtr:foo/bar")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "gitlab.mydomain.com/foo/bar"; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestNormalizeDepArg_Alias_DefaultsToHTTPS(t *testing.T) {
	// No HostProtocols entry for the alias's host. auth.Resolve defaults to
	// HTTPS (see internal/core/services/auth/auth.go Layer 6), so the alias
	// expansion must default the same way.
	cfg := setupIsolatedConfig(t)
	cfg.Aliases["gtr"] = "gitlab.mydomain.com"

	key, ok := installer.NormalizeDepKey("gtr:foo/bar")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "gitlab.mydomain.com/foo/bar"; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestNormalizeDepArg_Alias_StripsDotGit(t *testing.T) {
	cfg := setupIsolatedConfig(t)
	cfg.Aliases["gtr"] = "gitlab.mydomain.com"
	cfg.HostProtocols["gitlab.mydomain.com"] = "ssh"

	key, ok := installer.NormalizeDepKey("gtr:foo/bar.git")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "git@gitlab.mydomain.com:foo/bar"; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestNormalizeDepArg_SSHURL_DoesNotTriggerAliasExpansion(t *testing.T) {
	cfg := setupIsolatedConfig(t)
	// Even with a "git" alias configured, "git@host:path" must short-circuit
	// because the "git@" prefix marks it as an SSH URL already.
	cfg.Aliases["git"] = "should.not.be.used"

	key, ok := installer.NormalizeDepKey("git@gitlab.mydomain.com:foo/bar")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "git@gitlab.mydomain.com:foo/bar"; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestNormalizeDepArg_HTTPSURL_DoesNotTriggerAliasExpansion(t *testing.T) {
	cfg := setupIsolatedConfig(t)
	cfg.Aliases["https"] = "should.not.be.used"

	key, ok := installer.NormalizeDepKey("https://github.com/HashLoad/horse")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "github.com/HashLoad/horse"; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

// Defensive: when an alias-shaped prefix has no corresponding alias entry,
// the parser must reject it rather than misclassify it as owner/repo or
// some other shape.
func TestNormalizeDepArg_UnknownAliasPrefix_Rejected(t *testing.T) {
	setupIsolatedConfig(t)
	// No aliases configured.

	if _, ok := installer.NormalizeDepKey("unknown:foo/bar"); ok {
		t.Error("expected ok=false for unconfigured alias prefix")
	}
}

func TestParseDependency(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name adds hashload prefix",
			input:    "horse",
			expected: "github.com/hashload/horse",
		},
		{
			name:     "owner/repo adds github.com prefix",
			input:    "basti-fantasti/bossy",
			expected: "github.com/basti-fantasti/bossy",
		},
		{
			name:     "full path unchanged",
			input:    "github.com/hashload/horse",
			expected: "github.com/hashload/horse",
		},
		{
			name:     "gitlab path unchanged",
			input:    "gitlab.com/user/repo",
			expected: "gitlab.com/user/repo",
		},
		{
			name:     "with version suffix",
			input:    "github.com/hashload/horse@1.0.0",
			expected: "github.com/hashload/horse@1.0.0",
		},
		{
			name:     "bitbucket path unchanged",
			input:    "bitbucket.org/user/repo",
			expected: "bitbucket.org/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := installer.ParseDependency(tt.input)
			if result != tt.expected {
				t.Errorf("ParseDependency(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEnsureDependency(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedDeps map[string]string
	}{
		{
			name: "simple dependency",
			args: []string{"horse"},
			expectedDeps: map[string]string{
				"github.com/hashload/horse": ">0.0.0",
			},
		},
		{
			name: "dependency with version",
			args: []string{"github.com/hashload/horse@2.0.0"},
			expectedDeps: map[string]string{
				"github.com/hashload/horse": "2.0.0",
			},
		},
		{
			name: "dependency with caret version",
			args: []string{"github.com/hashload/horse@^1.5.0"},
			expectedDeps: map[string]string{
				"github.com/hashload/horse": "^1.5.0",
			},
		},
		{
			name: "multiple dependencies",
			args: []string{"horse", "boss-ide"},
			expectedDeps: map[string]string{
				"github.com/hashload/horse":    ">0.0.0",
				"github.com/hashload/boss-ide": ">0.0.0",
			},
		},
		{
			name: "dependency with .git suffix",
			args: []string{"github.com/hashload/horse.git"},
			expectedDeps: map[string]string{
				"github.com/hashload/horse": ">0.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := &domain.Package{
				Dependencies: make(map[string]string),
			}

			installer.EnsureDependency(pkg, tt.args)

			if len(pkg.Dependencies) != len(tt.expectedDeps) {
				t.Errorf("Dependencies count = %d, want %d", len(pkg.Dependencies), len(tt.expectedDeps))
			}

			for dep, ver := range tt.expectedDeps {
				if pkg.Dependencies[dep] != ver {
					t.Errorf("Dependencies[%q] = %q, want %q", dep, pkg.Dependencies[dep], ver)
				}
			}
		})
	}
}

func TestEnsureDependency_OwnerRepo(t *testing.T) {
	pkg := &domain.Package{
		Dependencies: make(map[string]string),
	}

	installer.EnsureDependency(pkg, []string{"basti-fantasti/bossy"})

	expected := "github.com/basti-fantasti/bossy"
	if _, ok := pkg.Dependencies[expected]; !ok {
		t.Errorf("Should add dependency for %q", expected)
	}
}

func TestEnsureDependency_TildeVersion(t *testing.T) {
	pkg := &domain.Package{
		Dependencies: make(map[string]string),
	}

	installer.EnsureDependency(pkg, []string{"github.com/hashload/horse@~1.0.0"})

	if ver := pkg.Dependencies["github.com/hashload/horse"]; ver != "~1.0.0" {
		t.Errorf("Version = %q, want ~1.0.0", ver)
	}
}

func TestEnsureDependency_HTTPSUrl(t *testing.T) {
	pkg := &domain.Package{
		Dependencies: make(map[string]string),
	}

	installer.EnsureDependency(pkg, []string{"https://github.com/hashload/horse"})

	// Should strip https:// and add to dependencies
	if len(pkg.Dependencies) == 0 {
		t.Error("Should add dependency for HTTPS URL")
	}
}

func TestEnsureDependency_GitAtURL(t *testing.T) {
	pkg := domain.NewPackage()
	installer.EnsureDependency(pkg, []string{"git@gitlab.mydomain.com:group/repo.git"})
	if _, ok := pkg.Dependencies["git@gitlab.mydomain.com:group/repo"]; !ok {
		t.Errorf("expected dep key to be canonical SSH form without .git; got %v", pkg.Dependencies)
	}
}

func TestEnsureDependency_HTTPSURL(t *testing.T) {
	pkg := domain.NewPackage()
	installer.EnsureDependency(pkg, []string{"https://github.com/HashLoad/horse"})
	if _, ok := pkg.Dependencies["github.com/HashLoad/horse"]; !ok {
		t.Errorf("expected canonical host/path key; got %v", pkg.Dependencies)
	}
}

// TestNormalizeDepArg_VersionSuffix is the regression guard for the bug where
// an SSH dependency argument could never carry a version. The splitter skipped
// the extraction entirely for anything starting with "git@", so
// "git@host:path@main" kept "@main" in the clone URL and in the module
// directory name. The separator must be searched for *after* the leading
// "git@", not skipped because of it.
//
// The three outputs of the parser are observed through EnsureDependency: the
// key by where the entry lands, the version by its value, and explicit by
// whether an already-declared pin is overwritten.
func TestNormalizeDepArg_VersionSuffix(t *testing.T) {
	const declared = "already-declared"

	tests := []struct {
		name    string
		raw     string
		wantKey string
		// wantVer empty means the argument carried no explicit version, so the
		// already-declared pin must survive untouched.
		wantVer string
	}{
		{
			name:    "ssh url with branch suffix",
			raw:     "git@gitlab.gtr.de:delphi/libraries/gtrlib@main",
			wantKey: "git@gitlab.gtr.de:delphi/libraries/gtrlib",
			wantVer: "main",
		},
		{
			name:    "ssh url without version",
			raw:     "git@gitlab.gtr.de:delphi/libraries/gtrlib",
			wantKey: "git@gitlab.gtr.de:delphi/libraries/gtrlib",
			wantVer: "",
		},
		{
			name:    "ssh url with slashed branch name",
			raw:     "git@host:path@feature/some-branch",
			wantKey: "git@host:path",
			wantVer: "feature/some-branch",
		},
		{
			name:    "ssh url with raw sha",
			raw:     "git@host:path@c880c29afebd1770b37eb5857c34fc9607681c4d",
			wantKey: "git@host:path",
			wantVer: "c880c29afebd1770b37eb5857c34fc9607681c4d",
		},
		{
			// Malformed: the "@" sits immediately after the "git@" prefix, so
			// splitting there would leave an empty URL part. The parser must
			// leave such input exactly as it was before this fix — no version,
			// nothing stripped.
			name:    "malformed double at is left alone",
			raw:     "git@@host:path",
			wantKey: "git@@host:path",
			wantVer: "",
		},
		{
			name:    "bare name with version",
			raw:     "horse@1.0.0",
			wantKey: "github.com/hashload/horse",
			wantVer: "1.0.0",
		},
		{
			name:    "bare name without version",
			raw:     "horse",
			wantKey: "github.com/hashload/horse",
			wantVer: "",
		},
		{
			name:    "https url with version",
			raw:     "https://host/owner/repo@v1.2.3",
			wantKey: "host/owner/repo",
			wantVer: "v1.2.3",
		},
		{
			name:    "owner/repo without version",
			raw:     "owner/repo",
			wantKey: "github.com/owner/repo",
			wantVer: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupIsolatedConfig(t)

			key, ok := installer.NormalizeDepKey(tt.raw)
			if !ok {
				t.Fatalf("NormalizeDepKey(%q) reported failure", tt.raw)
			}
			if key != tt.wantKey {
				t.Errorf("NormalizeDepKey(%q) = %q, want %q", tt.raw, key, tt.wantKey)
			}

			pkg := &domain.Package{Dependencies: map[string]string{tt.wantKey: declared}}
			installer.EnsureDependency(pkg, []string{tt.raw})

			if len(pkg.Dependencies) != 1 {
				t.Fatalf("Dependencies = %v, want exactly one entry", pkg.Dependencies)
			}
			wantVer := tt.wantVer
			if wantVer == "" {
				wantVer = declared
			}
			if got := pkg.Dependencies[tt.wantKey]; got != wantVer {
				t.Errorf("Dependencies[%q] = %q, want %q", tt.wantKey, got, wantVer)
			}
		})
	}
}
