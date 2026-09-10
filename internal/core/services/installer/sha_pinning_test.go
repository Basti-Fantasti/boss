//nolint:testpackage // Testing internal reference resolution
package installer

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

// TestResolveReference_RawSHAKeepsHash guards the bug where the resolved
// reference was reduced to its name before reaching checkoutAndUpdate. The name
// was plumbing.HEAD, the checkout hash was derived from that name, and
// plumbing.NewHash("HEAD") silently yields the zero hash — which go-git's
// CheckoutOptions.Validate() turns into a checkout of master. A SHA pin
// therefore built whatever master pointed at, without a warning.
//
// The IsZero assertion is deliberate: NewHash never errors on garbage, so the
// zero hash is precisely how the defect stayed invisible.
func TestResolveReference_RawSHAKeepsHash(t *testing.T) {
	const sha = "0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21"

	dep := domain.ParseDependency("github.com/example/repo", sha)
	pkg := &domain.Package{Dependencies: map[string]string{"github.com/example/repo": sha}}
	ctx := &installContext{
		rootLocked: &domain.PackageLock{Installed: map[string]domain.LockedDependency{}},
		warnings:   make([]string, 0),
	}

	// nil repository: a raw SHA is terminal and must never trigger resolution.
	ref := ctx.resolveReference(pkg, dep, nil)
	if ref == nil {
		t.Fatal("resolveReference returned nil for a raw SHA pin")
	}

	if ref.Hash().IsZero() {
		t.Fatalf("hash is the zero hash; reference name is %q", ref.Name())
	}
	if got := ref.Hash().String(); got != sha {
		t.Errorf("hash = %q, want %q", got, sha)
	}

	// checkoutAndUpdate dispatches on the name's shape and writes Short() into
	// the lock as the version. For a raw-SHA pin the SHA is the correct label.
	if ref.Name().IsTag() || ref.Name().IsBranch() || ref.Name().IsRemote() {
		t.Errorf("name %q must classify as a hash reference", ref.Name())
	}
	if got := ref.Name().Short(); got != sha {
		t.Errorf("lock version label = %q, want %q", got, sha)
	}

	// bossy.json must not be rewritten for an explicit pin.
	if got := pkg.Dependencies["github.com/example/repo"]; got != sha {
		t.Errorf("bossy.json version = %q, want %q", got, sha)
	}
}

// TestResolveReference_LockedCommitKeepsHashAndLabel covers the lock-replay
// path, which is what reproducible CI installs depend on. Here the name and the
// hash genuinely differ: the name must stay the declared version because
// checkoutAndUpdate writes it back as the lock's version field, while the hash
// must be the locked commit.
func TestResolveReference_LockedCommitKeepsHashAndLabel(t *testing.T) {
	const sha = "fedcba9876543210fedcba9876543210fedcba98"

	dep := domain.ParseDependency("github.com/example/repo", "develop")
	pkg := &domain.Package{Dependencies: map[string]string{"github.com/example/repo": "develop"}}
	ctx := &installContext{
		rootLocked: &domain.PackageLock{
			Installed: map[string]domain.LockedDependency{
				dep.GetKey(): {Name: "repo", Version: "develop", Commit: sha},
			},
		},
		useLockedVersion: true,
		warnings:         make([]string, 0),
	}

	ref := ctx.resolveReference(pkg, dep, nil)
	if ref == nil {
		t.Fatal("resolveReference returned nil for a locked commit")
	}

	if ref.Hash().IsZero() {
		t.Fatalf("hash is the zero hash; reference name is %q", ref.Name())
	}
	if got := ref.Hash().String(); got != sha {
		t.Errorf("hash = %q, want locked commit %q", got, sha)
	}
	if got := ref.Name().Short(); got != "develop" {
		t.Errorf("lock version label = %q, want %q", got, "develop")
	}
	if ref.Name().IsTag() || ref.Name().IsBranch() || ref.Name().IsRemote() {
		t.Errorf("name %q must classify as a hash reference so the pin is honoured", ref.Name())
	}
}

// TestIsHashReference covers the predicate checkoutAndUpdate dispatches on,
// including the shape resolveReference mints for its main-branch fallback. The
// fallback carries no resolved commit, so it must classify as a named ref —
// otherwise the checkout would be attempted against its zero hash.
func TestIsHashReference(t *testing.T) {
	tests := []struct {
		name string
		ref  plumbing.ReferenceName
		want bool
	}{
		{
			name: "main-branch fallback as resolveReference mints it",
			ref:  plumbing.NewBranchReferenceName("master"),
			want: false,
		},
		{name: "tag", ref: plumbing.NewTagReferenceName("v1.2.3"), want: false},
		{name: "remote-tracking", ref: plumbing.NewRemoteReferenceName("origin", "develop"), want: false},
		{
			name: "raw SHA label",
			ref:  plumbing.ReferenceName("0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21"),
			want: true,
		},
		{name: "locked branch label", ref: plumbing.ReferenceName("develop"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHashReference(tt.ref); got != tt.want {
				t.Errorf("isHashReference(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

// TestShortenSHA covers the debug-output abbreviation for both reference
// shapes: a full commit hash is truncated, a short version label is not.
func TestShortenSHA(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21", "0dd7b27"},
		{"develop", "develop"},
		{"v1.0", "v1.0"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortenSHA(tt.in); got != tt.want {
			t.Errorf("shortenSHA(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
