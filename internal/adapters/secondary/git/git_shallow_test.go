//nolint:testpackage // Testing internal behaviour of the native clone flags
package gitadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// TestShallowCloneFlags_KeepEveryBranchReachable is the regression test for the
// shallow-clone defect: git implies --single-branch with --depth, which writes
// a remote refspec restricted to the default branch. Every other branch is then
// permanently unreachable — `fetch --all` brings nothing, `fetch --unshallow`
// does not recover it, and the checkout fails with "pathspec did not match".
//
// The test drives the real git binary through the exact flags doClone passes
// when env.GetGitShallow() is on, so it fails against the old flags without
// needing bossy's env and cache plumbing. It asserts the repository is still
// shallow as well, so the bug cannot be traded for a full clone.
func TestShallowCloneFlags_KeepEveryBranchReachable(t *testing.T) {
	fixture := buildNativeFixture(t)
	gitEnv := hermeticGitEnv(t)
	dest := filepath.Join(t.TempDir(), "clone")

	// The shallow flags doClone appends. --separate-git-dir is omitted: it moves
	// where the git directory lives and has no bearing on the refspec.
	runGit(t, gitEnv, "", "clone", "--depth", "1", "--no-single-branch", fixture.url, dest)

	if refspec := runGit(t, gitEnv, dest, "config", "remote.origin.fetch"); !strings.Contains(refspec, "refs/heads/*") {
		t.Fatalf("remote refspec is restricted to a single branch: %q, want one covering refs/heads/*", refspec)
	}

	if shallow := runGit(t, gitEnv, dest, "rev-parse", "--is-shallow-repository"); shallow != "true" {
		t.Errorf("clone is no longer shallow: rev-parse --is-shallow-repository = %q, want \"true\"", shallow)
	}

	// The payoff: a branch other than the default must be checkoutable.
	runGit(t, gitEnv, dest, "checkout", "-f", "develop")

	if head := runGit(t, gitEnv, dest, "rev-parse", "HEAD"); head != fixture.refs["refs/heads/develop"] {
		t.Errorf("HEAD after checkout develop = %q, want %q", head, fixture.refs["refs/heads/develop"])
	}
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

	dep := domain.Dependency{Repository: "example.com/owner/fixture-repo"}
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	gitDir := filepath.Join(env.GetCacheDir(), dep.HashName())

	// git clone will not create the parent of a --separate-git-dir target; the
	// real cache directory is created by bossy's setup long before a clone runs.
	if err := os.MkdirAll(env.GetCacheDir(), 0o750); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}

	// A cache as an older bossy left it: separate git dir, cloned single-branch.
	runGit(t, gitEnv, "", "clone", "--separate-git-dir="+gitDir,
		"--depth", "1", "--single-branch", fixture.url, dirModule)
	// doClone removes the .git pointer once the clone finishes; getWrapperFetch
	// is responsible for putting it back, so start from the state it expects.
	if err := os.Remove(filepath.Join(dirModule, ".git")); err != nil {
		t.Fatalf("remove .git pointer: %v", err)
	}

	if err := getWrapperFetch(dep); err != nil {
		t.Fatalf("getWrapperFetch: %v", err)
	}

	if refspec := runGit(t, gitEnv, "", "--git-dir", gitDir, "config",
		"remote.origin.fetch"); refspec != "+refs/heads/*:refs/remotes/origin/*" {
		t.Errorf("refspec not repaired: %q", refspec)
	}

	got := runGit(t, gitEnv, "", "--git-dir", gitDir, "rev-parse", "refs/remotes/origin/develop")
	if got != fixture.refs["refs/heads/develop"] {
		t.Errorf("develop tip after repair = %q, want %q", got, fixture.refs["refs/heads/develop"])
	}
}
