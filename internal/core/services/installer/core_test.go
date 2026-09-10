//nolint:testpackage // Testing internal implementation details
package installer

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

func TestCollectAllDependencies(t *testing.T) {
	tests := []struct {
		name     string
		pkg      *domain.Package
		expected int
	}{
		{
			name: "empty dependencies",
			pkg: &domain.Package{
				Dependencies: nil,
			},
			expected: 0,
		},
		{
			name: "single dependency",
			pkg: &domain.Package{
				Dependencies: map[string]string{
					"dep1": "github.com/example/dep1",
				},
			},
			expected: 1,
		},
		{
			name: "multiple dependencies",
			pkg: &domain.Package{
				Dependencies: map[string]string{
					"dep1": "github.com/example/dep1",
					"dep2": "github.com/example/dep2",
					"dep3": "github.com/example/dep3",
				},
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collectAllDependencies(tt.pkg)
			if len(result) != tt.expected {
				t.Errorf("Expected %d dependencies, got %d", tt.expected, len(result))
			}
		})
	}
}

// TestShouldReconcile guards the fix for the bug where running
// `bossy install <pkg>` or `bossy update <pkg>` with explicit args caused
// every other installed module and lock entry to be wiped, because the
// post-install reconciliation walked the args-filtered dependency subset
// instead of the full tree. Targeted invocations must skip reconciliation.
func TestShouldReconcile(t *testing.T) {
	tests := []struct {
		name string
		opts InstallOptions
		want bool
	}{
		{name: "no args reconciles", opts: InstallOptions{}, want: true},
		{name: "single arg skips", opts: InstallOptions{Args: []string{"dep1"}}, want: false},
		{name: "multiple args skip", opts: InstallOptions{Args: []string{"dep1", "dep2"}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldReconcile(tt.opts); got != tt.want {
				t.Errorf("shouldReconcile(%+v) = %v, want %v", tt.opts, got, tt.want)
			}
		})
	}
}

func TestAddWarning(t *testing.T) {
	ctx := &installContext{
		warnings: make([]string, 0),
	}

	initialLen := len(ctx.warnings)
	ctx.addWarning("Test warning")

	if len(ctx.warnings) != initialLen+1 {
		t.Errorf("Expected %d warnings, got %d", initialLen+1, len(ctx.warnings))
	}

	if ctx.warnings[0] != "Test warning" {
		t.Errorf("Expected warning 'Test warning', got %q", ctx.warnings[0])
	}
}

// TestGetVersion_RawSHAFastPath verifies that when bossy.json pins a dependency
// to a raw 40-char SHA, getVersion returns a hash reference immediately without
// touching the repository (we pass nil to enforce that).
func TestGetVersion_RawSHAFastPath(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"

	dep := domain.ParseDependency("github.com/example/repo", sha)
	ctx := &installContext{
		rootLocked: &domain.PackageLock{Installed: map[string]domain.LockedDependency{}},
		warnings:   make([]string, 0),
	}

	ref := ctx.getVersion(dep, nil)
	if ref == nil {
		t.Fatalf("Expected non-nil reference for raw SHA, got nil")
	}
	if got := ref.Hash().String(); got != sha {
		t.Errorf("Hash = %q, want %q", got, sha)
	}
	if got := ref.Name().Short(); got != sha {
		t.Errorf("Name = %q, want the SHA itself as the lock version label", got)
	}
}

// TestIsObjectNotFound verifies the error-classification used by the shallow-
// clone deepening retry. Both the go-git sentinel and the native-binary wording
// must classify as "object not found"; unrelated errors must not.
func TestIsObjectNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{
			name: "wrapped go-git ErrObjectNotFound",
			err:  fmt.Errorf("checkout failed: %w", plumbing.ErrObjectNotFound),
			want: true,
		},
		{
			name: "native unknown revision",
			err:  errors.New("unknown revision or path not in the working tree"),
			want: true,
		},
		{
			name: "unrelated bad-object error",
			err:  errors.New("fatal: bad object"),
			want: false,
		},
		{
			name: "mixed-case object not found",
			err:  errors.New("Object Not Found"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isObjectNotFound(tt.err); got != tt.want {
				t.Errorf("isObjectNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestGetVersion_LockedCommitFastPath verifies that when useLockedVersion is set
// and the lock entry has a Commit, getVersion returns a hash reference at that
// commit. Passing nil for the repository proves we never query it.
func TestGetVersion_LockedCommitFastPath(t *testing.T) {
	const sha = "fedcba9876543210fedcba9876543210fedcba98"

	dep := domain.ParseDependency("github.com/example/repo", "^1.0.0")
	ctx := &installContext{
		rootLocked: &domain.PackageLock{
			Installed: map[string]domain.LockedDependency{
				dep.GetKey(): {
					Name:    "repo",
					Version: "1.2.3",
					Commit:  sha,
				},
			},
		},
		useLockedVersion: true,
		warnings:         make([]string, 0),
	}

	ref := ctx.getVersion(dep, nil)
	if ref == nil {
		t.Fatalf("Expected non-nil reference for locked commit, got nil")
	}
	if got := ref.Hash().String(); got != sha {
		t.Errorf("Hash = %q, want %q", got, sha)
	}
	if got := ref.Name().Short(); got != "1.2.3" {
		t.Errorf("Name = %q, want the locked version label %q", got, "1.2.3")
	}
}
