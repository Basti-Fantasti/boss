//nolint:testpackage // Testing internal functions
package gitadapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
)

// The TUI shows a branch and tag list for whichever dependency the cursor is
// on. Cloning to answer that would mean fetching entire repositories to render
// a list, so the embedded backend must list refs straight from the remote.
func TestListRemoteRefsEmbedded_SeesBranchesAndTags(t *testing.T) {
	fixture := buildNativeFixture(t)
	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{URL: fixture.url, Transport: auth.TransportHTTPS}

	refs, err := listRemoteRefsEmbedded(dep, decision)
	if err != nil {
		t.Fatalf("listRemoteRefsEmbedded: %v", err)
	}

	got := map[string]string{}
	for _, r := range refs {
		name := r.Name().String()
		if prev, dup := got[name]; dup {
			t.Fatalf("ref %q returned twice (%s and %s)", name, prev, r.Hash())
		}
		got[name] = r.Hash().String()
	}

	if len(got) != len(fixture.refs) {
		t.Fatalf("ref set mismatch:\n got %v\nwant %v", got, fixture.refs)
	}
	for name, want := range fixture.refs {
		if got[name] != want {
			t.Errorf("ref %q: got %q, want %q", name, got[name], want)
		}
	}

	// Clone-free is the whole point: nothing may appear beside the source.
	entries, err := os.ReadDir(fixture.dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != ".git" && e.Name() != "f.txt" {
			t.Errorf("unexpected entry %q created by the listing", e.Name())
		}
	}
}

// Both backends must return the same shape, so the embedded one applies the
// same refs/heads + refs/tags allowlist parseLsRemote uses. Server-advertised
// refs such as refs/pull/* satisfy the installer's raw-SHA sentinel
// (!IsTag && !IsBranch && !IsRemote) and would be offered as a pin.
func TestListRemoteRefsEmbedded_FiltersNonBranchTagRefs(t *testing.T) {
	fixture := buildNativeFixture(t)
	gitEnv := hermeticGitEnv(t)

	mainSHA := runGit(t, gitEnv, fixture.dir, "rev-parse", "main")
	for _, ref := range []string{"refs/pull/12/head", "refs/merge-requests/7/head", "refs/keep-around/x"} {
		runGit(t, gitEnv, fixture.dir, "update-ref", ref, mainSHA)
	}

	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{URL: fixture.url, Transport: auth.TransportHTTPS}

	refs, err := listRemoteRefsEmbedded(dep, decision)
	if err != nil {
		t.Fatalf("listRemoteRefsEmbedded: %v", err)
	}

	for _, r := range refs {
		name := r.Name().String()
		if !isBranchOrTagRef(name) {
			t.Errorf("ref %q must not be returned", name)
		}
	}
	if len(refs) != len(fixture.refs) {
		t.Errorf("got %d refs, want %d", len(refs), len(fixture.refs))
	}
}

// An empty listing is indistinguishable from "this repository has no refs",
// and the caller would fall back to the main branch — the silent-wrong-branch
// bug removed in dda87ee. A failure has to be an error.
func TestListRemoteRefsEmbedded_UnreachableRemoteErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{
		URL:       "file:///" + filepath.ToSlash(missing),
		Transport: auth.TransportHTTPS,
	}

	refs, err := listRemoteRefsEmbedded(dep, decision)
	if err == nil {
		t.Fatalf("listing an unreachable remote returned %d refs and no error", len(refs))
	}
	if refs != nil {
		t.Errorf("refs = %v, want nil alongside the error", refs)
	}
}

// A repository with no commits advertises no refs. That is a legitimate empty
// result, not a failure, and must not be reported as one.
func TestListRemoteRefsEmbedded_EmptyRepositoryIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	gitEnv := hermeticGitEnv(t)
	runGit(t, gitEnv, dir, "init", "-q", "-b", "main", ".")

	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{
		URL:       "file:///" + filepath.ToSlash(dir),
		Transport: auth.TransportHTTPS,
	}

	refs, err := listRemoteRefsEmbedded(dep, decision)
	if err != nil {
		t.Fatalf("listing an empty repository returned %v, want no error", err)
	}
	if len(refs) != 0 {
		t.Errorf("got %d refs from an empty repository", len(refs))
	}
}

func TestIsBranchOrTagRef(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"refs/heads/main":            true,
		"refs/heads/feature/x":       true,
		"refs/tags/v1.0.0":           true,
		"refs/pull/12/head":          false,
		"refs/merge-requests/7/head": false,
		"refs/keep-around/x":         false,
		"refs/remotes/origin/main":   false,
		"HEAD":                       false,
		"":                           false,
	}

	for name, want := range cases {
		if got := isBranchOrTagRef(name); got != want {
			t.Errorf("isBranchOrTagRef(%q) = %v, want %v", name, got, want)
		}
	}
}

// ListRemoteRefs routes through auth.Resolve, so a dependency it cannot resolve
// must surface that error rather than an empty listing.
func TestListRemoteRefs_SurfacesAuthResolutionFailure(t *testing.T) {
	t.Parallel()

	refs, err := ListRemoteRefs(domain.Dependency{Repository: "http://:invalid"})
	if err == nil {
		t.Fatalf("an unresolvable dependency returned %d refs and no error", len(refs))
	}
	if refs != nil {
		t.Errorf("refs = %v, want nil alongside the error", refs)
	}
}
