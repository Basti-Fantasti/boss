//nolint:testpackage // Testing internal behaviour of the native clone flags
package gitadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// TestCloneCacheNative_ShallowKeepsEveryBranchReachable is the regression test
// for the shallow-clone defect: git implies --single-branch with --depth, which
// writes a remote refspec restricted to the default branch. Every other branch
// is then permanently unreachable - `fetch --all` brings nothing,
// `fetch --unshallow` does not recover it, and the checkout fails with
// "pathspec did not match".
//
// The test drives the real CloneCacheNative with shallow mode on, so the flags
// under assertion are the ones doClone actually builds rather than a copy of
// them retyped here. It asserts the clone is still shallow as well, so the bug
// cannot be traded away for a full clone.
func TestCloneCacheNative_ShallowKeepsEveryBranchReachable(t *testing.T) {
	fixture := buildNativeFixture(t)
	gitEnv := hermeticGitEnv(t)
	// Point the cache and the modules dir at throwaway locations. This mirrors
	// git_checkouthash_test.go's withFixtureEnv, which lives in the external
	// test package and so cannot be reused for an unexported function.
	t.Setenv("BOSS_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	// The switch doClone reads. env.GetGitShallow prefers this over the
	// persisted config, so no boss config file has to be written.
	t.Setenv("BOSS_GIT_SHALLOW", "true")

	// doClone shells out through gitSafeEnv, which deliberately passes the
	// config-selecting variables through, so the process environment is where
	// its git calls have to be detached from the developer's config.
	noConfig := filepath.Join(t.TempDir(), "absent-gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", noConfig)
	t.Setenv("GIT_CONFIG_SYSTEM", noConfig)

	dep := domain.Dependency{Repository: "example.com/owner/shallow-fixture"}
	gitDir := filepath.Join(env.GetCacheDir(), dep.HashName())

	// git clone will not create the parent of a --separate-git-dir target; the
	// real cache directory is created by bossy's setup long before a clone runs.
	if err := os.MkdirAll(env.GetCacheDir(), 0o750); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}

	if _, err := CloneCacheNative(dep, auth.Decision{URL: fixture.url, Transport: auth.TransportSSH}); err != nil {
		t.Fatalf("CloneCacheNative: %v", err)
	}

	// doClone removes the .git pointer from the module dir once the clone
	// finishes, so the cache has to be addressed through --git-dir.
	if refspec := runGit(t, gitEnv, "", "--git-dir", gitDir, "config",
		"remote.origin.fetch"); !strings.Contains(refspec, "refs/heads/*") {
		t.Fatalf("remote refspec is restricted to a single branch: %q, want one covering refs/heads/*", refspec)
	}

	if shallow := runGit(t, gitEnv, "", "--git-dir", gitDir,
		"rev-parse", "--is-shallow-repository"); shallow != "true" {
		t.Errorf("clone is not shallow: rev-parse --is-shallow-repository = %q, want \"true\"", shallow)
	}

	// The payoff: a branch other than the default must have been fetched. With
	// --single-branch this ref does not exist at all and rev-parse fails.
	got := runGit(t, gitEnv, "", "--git-dir", gitDir, "rev-parse", "refs/remotes/origin/develop")
	if got != fixture.refs["refs/heads/develop"] {
		t.Errorf("develop tip in the shallow cache = %q, want %q", got, fixture.refs["refs/heads/develop"])
	}
}

// seedSingleBranchCache builds a cache in the shape an older bossy left behind
// - separate git dir, cloned --single-branch - and returns the dependency and
// the cache git dir. It leaves the module directory without a .git pointer,
// which is the state doClone hands to getWrapperFetch.
func seedSingleBranchCache(t *testing.T, fixture nativeFixture, gitEnv []string, name string) (
	domain.Dependency, string,
) {
	t.Helper()
	// Point the cache and the modules dir at throwaway locations. This mirrors
	// git_checkouthash_test.go's withFixtureEnv, which lives in the external
	// test package and so cannot be reused for an unexported function.
	t.Setenv("BOSS_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	// getWrapperFetch shells out through gitSafeEnv, which deliberately passes
	// the config-selecting variables through, so the process environment is
	// where its git calls have to be detached from the developer's config.
	noConfig := filepath.Join(t.TempDir(), "absent-gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", noConfig)
	t.Setenv("GIT_CONFIG_SYSTEM", noConfig)

	dep := domain.Dependency{Repository: "example.com/owner/" + name}
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	gitDir := filepath.Join(env.GetCacheDir(), dep.HashName())

	// git clone will not create the parent of a --separate-git-dir target; the
	// real cache directory is created by bossy's setup long before a clone runs.
	if err := os.MkdirAll(env.GetCacheDir(), 0o750); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}

	runGit(t, gitEnv, "", "clone", "--separate-git-dir="+gitDir,
		"--depth", "1", "--single-branch", fixture.url, dirModule)
	// doClone removes the .git pointer once the clone finishes; getWrapperFetch
	// is responsible for putting it back, so start from the state it expects.
	if err := os.Remove(filepath.Join(dirModule, ".git")); err != nil {
		t.Fatalf("remove .git pointer: %v", err)
	}

	return dep, gitDir
}

// TestGetWrapperFetch_RepairsRestrictedRefspec covers the other half of the
// defect: caches that an older bossy already cloned with --single-branch. Their
// stored refspec is pinned to the default branch, so `fetch --all` silently
// brings nothing for every other branch and `fetch --unshallow` does not
// recover it either — the cache stays broken for as long as it exists.
//
// The test builds exactly such a cache and drives the real getWrapperFetch over
// it, so it fails if the refspec normalisation is dropped.
func TestGetWrapperFetch_RepairsRestrictedRefspec(t *testing.T) {
	fixture := buildNativeFixture(t)
	gitEnv := hermeticGitEnv(t)
	dep, gitDir := seedSingleBranchCache(t, fixture, gitEnv, "fixture-repo")

	if err := getWrapperFetch(dep); err != nil {
		t.Fatalf("getWrapperFetch: %v", err)
	}

	if refspec := runGit(t, gitEnv, "", "--git-dir", gitDir, "config",
		"remote.origin.fetch"); refspec != wildcardHeadsRefspec {
		t.Errorf("refspec not repaired: %q", refspec)
	}

	got := runGit(t, gitEnv, "", "--git-dir", gitDir, "rev-parse", "refs/remotes/origin/develop")
	if got != fixture.refs["refs/heads/develop"] {
		t.Errorf("develop tip after repair = %q, want %q", got, fixture.refs["refs/heads/develop"])
	}
}

// TestGetWrapperFetch_RepairsMultiValuedRefspec is the regression test for a
// repair that silently did nothing. `git config <key> <value>` refuses to touch
// a multi-valued key -
//
//	error: cannot overwrite multiple values with a single value
//
// - and getWrapperFetch swallows that failure at msg.Debug, so a cache whose
// remote carried more than one refspec, none of them covering refs/heads/*,
// stayed broken forever with nothing to show for it.
func TestGetWrapperFetch_RepairsMultiValuedRefspec(t *testing.T) {
	fixture := buildNativeFixture(t)
	gitEnv := hermeticGitEnv(t)
	dep, gitDir := seedSingleBranchCache(t, fixture, gitEnv, "multivalued-repo")

	// Two values, neither covering refs/heads/*: the state a single-value write
	// cannot repair.
	runGit(t, gitEnv, "", "--git-dir", gitDir, "config", "--replace-all",
		"remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	runGit(t, gitEnv, "", "--git-dir", gitDir, "config", "--add",
		"remote.origin.fetch", "+refs/tags/*:refs/tags/*")

	// Guard the premise: if git ever stopped accepting a multi-valued
	// remote.origin.fetch, the test would still pass while proving nothing.
	before := runGit(t, gitEnv, "", "--git-dir", gitDir, "config", "--get-all", "remote.origin.fetch")
	if len(strings.Split(before, "\n")) != 2 {
		t.Fatalf("fixture did not produce a multi-valued refspec, got %q", before)
	}

	if err := getWrapperFetch(dep); err != nil {
		t.Fatalf("getWrapperFetch: %v", err)
	}

	if refspec := runGit(t, gitEnv, "", "--git-dir", gitDir, "config", "--get-all",
		"remote.origin.fetch"); refspec != wildcardHeadsRefspec {
		t.Errorf("multi-valued refspec not repaired: %q, want %q", refspec, wildcardHeadsRefspec)
	}

	got := runGit(t, gitEnv, "", "--git-dir", gitDir, "rev-parse", "refs/remotes/origin/develop")
	if got != fixture.refs["refs/heads/develop"] {
		t.Errorf("develop tip after repair = %q, want %q", got, fixture.refs["refs/heads/develop"])
	}
}

// TestGetWrapperFetch_PreservesCoveringRefspec is the other side of the same
// change: the repair must not rewrite a refspec that already works. An
// unconditional write discards whatever the user deliberately added alongside
// the wildcard, which was never "harmless on healthy caches" for any cache
// bossy did not clone itself.
func TestGetWrapperFetch_PreservesCoveringRefspec(t *testing.T) {
	fixture := buildNativeFixture(t)
	gitEnv := hermeticGitEnv(t)
	dep, gitDir := seedSingleBranchCache(t, fixture, gitEnv, "covering-repo")

	const extra = "+refs/tags/*:refs/tags/*"
	runGit(t, gitEnv, "", "--git-dir", gitDir, "config", "--replace-all",
		"remote.origin.fetch", wildcardHeadsRefspec)
	runGit(t, gitEnv, "", "--git-dir", gitDir, "config", "--add", "remote.origin.fetch", extra)

	if err := getWrapperFetch(dep); err != nil {
		t.Fatalf("getWrapperFetch: %v", err)
	}

	got := runGit(t, gitEnv, "", "--git-dir", gitDir, "config", "--get-all", "remote.origin.fetch")
	want := wildcardHeadsRefspec + "\n" + extra
	if got != want {
		t.Errorf("refspec was rewritten:\n got %q\nwant %q", got, want)
	}
}
