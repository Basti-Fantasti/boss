package installer_test

import (
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/installer"
	"github.com/basti-fantasti/bossy/pkg/consts"
)

// TestEnsureDependency_PreservesDeclaredVersion is the regression guard for the
// bug where `bossy update <dep>` destroyed the version declared in bossy.json.
// LocalInstall calls EnsureDependency unconditionally with the command's args;
// normalizeDepArg defaulted a missing "@version" to ">0.0.0" and AddDependency
// overwrote the entry, after which resolveReference rewrote it again to the
// resolved semver. A branch or tag pin was silently replaced by a version
// range.
func TestEnsureDependency_PreservesDeclaredVersion(t *testing.T) {
	const repo = "github.com/hashload/horse"

	tests := []struct {
		name     string
		existing map[string]string
		args     []string
		wantKey  string
		wantVer  string
	}{
		{
			name:     "bare arg keeps a branch pin",
			existing: map[string]string{repo: "master"},
			args:     []string{repo},
			wantKey:  repo,
			wantVer:  "master",
		},
		{
			name:     "bare arg keeps a tag pin",
			existing: map[string]string{repo: "v3.2.0"},
			args:     []string{repo},
			wantKey:  repo,
			wantVer:  "v3.2.0",
		},
		{
			name:     "bare arg keeps a caret range",
			existing: map[string]string{repo: "^1.5.0"},
			args:     []string{repo},
			wantKey:  repo,
			wantVer:  "^1.5.0",
		},
		{
			name:     "bare arg keeps a raw SHA pin",
			existing: map[string]string{repo: "0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21"},
			args:     []string{repo},
			wantKey:  repo,
			wantVer:  "0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21",
		},
		{
			name:     "shorthand arg keeps the pin of the canonical entry",
			existing: map[string]string{repo: "develop"},
			args:     []string{"horse"},
			wantKey:  repo,
			wantVer:  "develop",
		},
		{
			name:     "explicit version still overrides",
			existing: map[string]string{repo: "master"},
			args:     []string{repo + "@^2.0.0"},
			wantKey:  repo,
			wantVer:  "^2.0.0",
		},
		{
			name:     "explicit branch still overrides",
			existing: map[string]string{repo: "master"},
			args:     []string{repo + "@develop"},
			wantKey:  repo,
			wantVer:  "develop",
		},
		{
			name:     "new dependency still gets the minimal range",
			existing: map[string]string{},
			args:     []string{repo},
			wantKey:  repo,
			wantVer:  consts.MinimalDependencyVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := &domain.Package{Dependencies: map[string]string{}}
			for k, v := range tt.existing {
				pkg.Dependencies[k] = v
			}

			installer.EnsureDependency(pkg, tt.args)

			if len(pkg.Dependencies) != 1 {
				t.Fatalf("Dependencies = %v, want exactly one entry", pkg.Dependencies)
			}
			got, ok := pkg.Dependencies[tt.wantKey]
			if !ok {
				t.Fatalf("Dependencies has no key %q: %v", tt.wantKey, pkg.Dependencies)
			}
			if got != tt.wantVer {
				t.Errorf("Dependencies[%q] = %q, want %q", tt.wantKey, got, tt.wantVer)
			}
		})
	}
}

// TestEnsureDependency_ExistenceCheckIsCaseInsensitive pins the existence check
// to the same case-insensitive matching AddDependency uses. A differently-cased
// entry in bossy.json must be recognised as existing, keep its declared
// version, and keep its original key spelling — not gain a second entry.
func TestEnsureDependency_ExistenceCheckIsCaseInsensitive(t *testing.T) {
	const declared = "github.com/HashLoad/Horse"

	pkg := &domain.Package{Dependencies: map[string]string{declared: "master"}}

	installer.EnsureDependency(pkg, []string{"github.com/hashload/horse"})

	if len(pkg.Dependencies) != 1 {
		t.Fatalf("Dependencies = %v, want exactly one entry", pkg.Dependencies)
	}
	if got := pkg.Dependencies[declared]; got != "master" {
		t.Errorf("Dependencies[%q] = %q, want %q", declared, got, "master")
	}
}

// TestNormalizeDepKey_StillDiscardsVersion guards the other caller of the
// argument parser: NormalizeDepKey must keep returning the canonical key
// regardless of whether a version suffix was supplied.
func TestNormalizeDepKey_StillDiscardsVersion(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"github.com/hashload/horse", "github.com/hashload/horse"},
		{"github.com/hashload/horse@master", "github.com/hashload/horse"},
		{"github.com/hashload/horse@^1.0.0", "github.com/hashload/horse"},
		{"horse", "github.com/hashload/horse"},
	}
	for _, tt := range tests {
		got, ok := installer.NormalizeDepKey(tt.raw)
		if !ok {
			t.Errorf("NormalizeDepKey(%q) reported failure", tt.raw)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeDepKey(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
