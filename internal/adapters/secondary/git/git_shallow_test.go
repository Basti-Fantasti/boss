//nolint:testpackage // Testing internal behaviour of the native clone flags
package gitadapter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// hermeticGitEnv returns an environment that detaches git from the user's
// global and system configuration and drops the repository-selecting
// variables, mirroring what buildNativeFixture does for the fixture itself.
// The clone under test needs the same isolation: an ambient GIT_DIR would
// redirect the clone, and a global remote.origin.fetch or clone.* setting
// would make the refspec assertions read the developer's config rather than
// the flags.
func hermeticGitEnv(t *testing.T) []string {
	t.Helper()

	// A path that does not exist: git >= 2.32 treats an unreadable
	// GIT_CONFIG_GLOBAL/GIT_CONFIG_SYSTEM as "no such config file".
	noConfig := filepath.Join(t.TempDir(), "absent-gitconfig")

	gitEnv := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE":
			continue
		}
		gitEnv = append(gitEnv, entry)
	}
	return append(gitEnv,
		"GIT_CONFIG_GLOBAL="+noConfig,
		"GIT_CONFIG_SYSTEM="+noConfig,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
}

// runGit runs git in dir with the hermetic environment and returns trimmed
// stdout, failing the test if git exits non-zero.
func runGit(t *testing.T, gitEnv []string, dir string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s%s", args, err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

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
