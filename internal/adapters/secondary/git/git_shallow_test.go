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

// TestCloneCacheEmbedded_ShallowKeepsEveryBranchReachable is the embedded
// counterpart. go-git has the same trap as the git binary and it is one token
// away: with CloneOptions.SingleBranch set, go-git substitutes
// "+HEAD:refs/remotes/%s/HEAD" for the wildcard refspec and persists that into
// the cache's config, after which every branch but the cloned one is
// unreachable exactly as in the native case.
//
// Asserting the persisted refspec is what makes this a test of bossy rather
// than of go-git: it reads back what CloneCacheEmbedded left on disk.
func TestCloneCacheEmbedded_ShallowKeepsEveryBranchReachable(t *testing.T) {
	fixture := buildNativeFixture(t)
	t.Setenv("BOSS_HOME", t.TempDir())
	t.Setenv("BOSS_GIT_SHALLOW", "true")

	dep := domain.Dependency{Repository: "https://example.com/owner/shallow-embedded"}
	decision := auth.Decision{URL: fixture.url, Transport: auth.TransportHTTPS}

	repository, err := CloneCacheEmbedded(dep, decision)
	if err != nil {
		t.Fatalf("CloneCacheEmbedded: %v", err)
	}

	cfg, err := repository.Config()
	if err != nil {
		t.Fatalf("read cache config: %v", err)
	}
	origin, ok := cfg.Remotes["origin"]
	if !ok {
		t.Fatalf("clone persisted no origin remote; the assertion below would be vacuous (remotes: %v)", cfg.Remotes)
	}
	if len(origin.Fetch) == 0 {
		t.Fatal("origin remote persisted no refspec; every branch but HEAD would be unreachable")
	}
	covered := false
	for _, refspec := range origin.Fetch {
		if strings.Contains(string(refspec), "refs/heads/*") {
			covered = true
		}
	}
	if !covered {
		t.Errorf("origin refspec %v covers no branch wildcard, want one covering refs/heads/*", origin.Fetch)
	}

	// Without this the test would still pass if shallow mode were quietly
	// ignored, which would make the refspec assertion prove nothing about the
	// shallow path in particular.
	shallow, err := repository.Storer.Shallow()
	if err != nil {
		t.Fatalf("read shallow list: %v", err)
	}
	if len(shallow) == 0 {
		t.Error("clone is not shallow: the shallow list is empty, so Depth was not honoured")
	}

	// The payoff: the non-default branch must have a remote-tracking ref.
	ref, err := repository.Reference("refs/remotes/origin/develop", true)
	if err != nil {
		t.Fatalf("develop is unreachable in the shallow cache: %v", err)
	}
	if ref.Hash().String() != fixture.refs["refs/heads/develop"] {
		t.Errorf("develop tip = %s, want %s", ref.Hash(), fixture.refs["refs/heads/develop"])
	}
}
