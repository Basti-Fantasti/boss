//nolint:testpackage // Testing internal functions
package gitadapter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
	goGit "github.com/go-git/go-git/v5"
)

// submoduleFixture is a superproject whose single submodule is pinned at an
// older commit than its branch tip, which is exactly the situation the remote
// policy exists to resolve.
//
// It is laid out the way bossy expects on disk — <project>/modules/<name> — so
// the adapter's own moduleDir() finds it, and the submodule source sits outside
// that tree so nothing moves out from under the URL .gitmodules records.
//
//	root/child            the submodule source
//	root/project/modules/super   the superproject worktree
type submoduleFixture struct {
	// superDir is the superproject worktree at project/modules/super.
	superDir string
	// pinnedCommit is what the superproject records for the submodule.
	pinnedCommit string
	// tipCommit is where the submodule's branch has since moved on to.
	tipCommit string
}

// buildSubmoduleFixture creates a submodule repository with two commits on its
// default branch, and a superproject pinning it at the first.
func buildSubmoduleFixture(t *testing.T) submoduleFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}

	gitEnv := hermeticGitEnv(t)
	root := t.TempDir()

	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	mkdir := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}

	allowFileSubmodules(t)

	subDir := filepath.Join(root, "child")
	mkdir(subDir)
	runGit(t, gitEnv, subDir, "init", "-q", "-b", "main", ".")
	write(subDir, "lib.txt", "v1")
	runGit(t, gitEnv, subDir, "add", ".")
	runGit(t, gitEnv, subDir, "commit", "-q", "-m", "child v1")
	pinned := runGit(t, gitEnv, subDir, "rev-parse", "HEAD")

	superDir := filepath.Join(root, "project", "modules", "super")
	mkdir(superDir)
	runGit(t, gitEnv, superDir, "init", "-q", "-b", "main", ".")
	write(superDir, "top.txt", "top")
	runGit(t, gitEnv, superDir, "add", ".")
	runGit(t, gitEnv, superDir, "commit", "-q", "-m", "super 1")

	subURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(subDir), "/")
	// The fixture's own git calls run under hermeticGitEnv, which does not
	// inherit the process environment, so they carry the opt-in as a flag.
	runGit(t, gitEnv, superDir, "-c", "protocol.file.allow=always",
		"submodule", "add", "-b", "main", subURL, "child")
	runGit(t, gitEnv, superDir, "commit", "-q", "-m", "add child")

	// The submodule's branch moves on; the superproject keeps pointing at v1.
	write(subDir, "lib.txt", "v2")
	runGit(t, gitEnv, subDir, "commit", "-q", "-am", "child v2")
	tip := runGit(t, gitEnv, subDir, "rev-parse", "HEAD")

	if pinned == tip {
		t.Fatal("fixture is broken: the pinned commit equals the branch tip")
	}

	return submoduleFixture{superDir: superDir, pinnedCommit: pinned, tipCommit: tip}
}

// allowFileSubmodules lets git use the file transport for submodules.
//
// git has refused it since CVE-2022-39253. Real submodules are https or ssh and
// unaffected, but a local fixture needs the opt-in. It goes through git's
// environment-config mechanism rather than the superproject's .git/config
// because `git submodule update` spawns a fetch inside the *submodule*
// repository, which has no such config; the environment reaches that child
// process, and gitSafeEnv passes os.Environ() through to it.
//
// It covers the adapter's commands only. The fixture's own calls go through
// hermeticGitEnv, which does not inherit the process environment, so those pass
// the setting as a -c flag instead.
func allowFileSubmodules(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
}

// useFixtureAsModule makes env.GetModulesDir resolve to the fixture's modules
// directory, so the adapter's moduleDir() finds the superproject.
func useFixtureAsModule(t *testing.T, fixture submoduleFixture) domain.Dependency {
	t.Helper()
	t.Setenv("BOSS_HOME", t.TempDir())
	// superDir is <project>/modules/super, so the project root is two up.
	t.Chdir(filepath.Dir(filepath.Dir(fixture.superDir)))
	return domain.Dependency{Repository: "example.com/owner/super"}
}

// submoduleHead reports where the fixture's submodule currently sits.
func submoduleHead(t *testing.T, superDir string) string {
	t.Helper()
	return runGit(t, hermeticGitEnv(t), filepath.Join(superDir, "child"), "rev-parse", "HEAD")
}

// This is the behaviour the batch script's `submodule update --remote` has and
// bossy did not: the submodule ends up at its branch tip, not the commit the
// superproject recorded.
func TestAdvanceSubmodulesToBranchTips_MovesToTheTip(t *testing.T) {
	fixture := buildSubmoduleFixture(t)
	dep := useFixtureAsModule(t, fixture)

	commits, err := AdvanceSubmodulesToBranchTips(dep)
	if err != nil {
		t.Fatalf("AdvanceSubmodulesToBranchTips: %v", err)
	}

	if got := commits["child"]; got != fixture.tipCommit {
		t.Errorf("reported commit for child = %q, want the branch tip %q", got, fixture.tipCommit)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.tipCommit {
		t.Errorf("submodule HEAD = %q, want the branch tip %q", got, fixture.tipCommit)
	}
}

// The resolved commits are what make the floating policy reproducible: a later
// install replays them rather than chasing the tip again.
func TestCheckoutSubmodules_ReplaysRecordedCommits(t *testing.T) {
	fixture := buildSubmoduleFixture(t)
	dep := useFixtureAsModule(t, fixture)

	if _, err := AdvanceSubmodulesToBranchTips(dep); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.tipCommit {
		t.Fatalf("setup: submodule is at %q, want the tip", got)
	}

	// Replaying the originally pinned commit must move it back, which is what
	// an install from a lock recorded before the branch moved has to do.
	if err := CheckoutSubmodules(dep, map[string]string{"child": fixture.pinnedCommit}); err != nil {
		t.Fatalf("CheckoutSubmodules: %v", err)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.pinnedCommit {
		t.Errorf("submodule HEAD = %q, want the replayed commit %q", got, fixture.pinnedCommit)
	}
}

func TestSubmoduleCommits_ReportsCurrentHeads(t *testing.T) {
	fixture := buildSubmoduleFixture(t)
	dep := useFixtureAsModule(t, fixture)

	if err := CheckoutSubmodules(dep, map[string]string{"child": fixture.pinnedCommit}); err != nil {
		t.Fatalf("CheckoutSubmodules: %v", err)
	}

	commits, err := SubmoduleCommits(dep)
	if err != nil {
		t.Fatalf("SubmoduleCommits: %v", err)
	}
	if got := commits["child"]; got != fixture.pinnedCommit {
		t.Errorf("child = %q, want %q", got, fixture.pinnedCommit)
	}
}

// bossy-lock.json is a file in the repository, so in a project you did not
// write its contents are attacker-influenced. A commit that is not a plain SHA
// must never reach a git command line.
func TestCheckoutSubmodules_RejectsNonSHACommits(t *testing.T) {
	t.Parallel()

	dep := domain.Dependency{Repository: "example.com/owner/super"}

	for _, bad := range []string{
		"--upload-pack=touch /tmp/pwned",
		"main",
		"deadbeef",
		"",
		strings.Repeat("z", 40),
	} {
		err := CheckoutSubmodules(dep, map[string]string{"child": bad})
		if err == nil {
			t.Errorf("commit %q was accepted", bad)
			continue
		}
		if !strings.Contains(err.Error(), "invalid commit") {
			t.Errorf("commit %q: error = %v", bad, err)
		}
	}
}

func TestCheckoutSubmodules_NoCommitsIsANoOp(t *testing.T) {
	t.Parallel()

	dep := domain.Dependency{Repository: "example.com/owner/super"}
	if err := CheckoutSubmodules(dep, nil); err != nil {
		t.Errorf("CheckoutSubmodules(nil) = %v, want no error", err)
	}
}

func TestParseSubmoduleStatus(t *testing.T) {
	t.Parallel()

	const (
		shaA = "1111111111111111111111111111111111111111"
		shaB = "2222222222222222222222222222222222222222"
	)

	stdout := strings.Join([]string{
		" " + shaA + " lib/indy (v10.6.2)",
		"-" + shaB + " lib/other",
		"+" + shaA + " nested/deep (heads/main)",
		// Short SHAs are dropped: plumbing.NewHash zero-pads them into a
		// well-formed but wrong commit, and a wrong commit in the lock is worse
		// than a missing one.
		" deadbeef lib/truncated",
		"garbage",
		"",
	}, "\n")

	got := parseSubmoduleStatus(stdout)

	want := map[string]string{
		"lib/indy":    shaA,
		"lib/other":   shaB,
		"nested/deep": shaA,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for path, sha := range want {
		if got[path] != sha {
			t.Errorf("%s = %q, want %q", path, got[path], sha)
		}
	}
}

// openFixtureWorktree opens the superproject directly, bypassing bossy's
// separate-git-dir cache layout, so the go-git implementation can be exercised
// against the same fixture the native one uses.
func openFixtureWorktree(t *testing.T, fixture submoduleFixture) *goGit.Worktree {
	t.Helper()
	repo, err := goGit.PlainOpen(fixture.superDir)
	if err != nil {
		t.Fatalf("PlainOpen %s: %v", fixture.superDir, err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	return worktree
}

// go-git has no --remote, so the embedded backend resolves each submodule's
// branch tip itself. It has to land on the same commit the native path does,
// or the two backends would install different trees from the same manifest.
func TestAdvanceSubmodulesInWorktree_MovesToTheTip(t *testing.T) {
	fixture := buildSubmoduleFixture(t)

	resolved, err := advanceSubmodulesInWorktree(openFixtureWorktree(t, fixture))
	if err != nil {
		t.Fatalf("advanceSubmodulesInWorktree: %v", err)
	}

	if got := resolved["child"]; got != fixture.tipCommit {
		t.Errorf("reported commit for child = %q, want the branch tip %q", got, fixture.tipCommit)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.tipCommit {
		t.Errorf("submodule HEAD = %q, want the branch tip %q", got, fixture.tipCommit)
	}
}

func TestCheckoutSubmodulesInWorktree_ReplaysRecordedCommits(t *testing.T) {
	fixture := buildSubmoduleFixture(t)

	if _, err := advanceSubmodulesInWorktree(openFixtureWorktree(t, fixture)); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.tipCommit {
		t.Fatalf("setup: submodule is at %q, want the tip", got)
	}

	err := checkoutSubmodulesInWorktree(openFixtureWorktree(t, fixture),
		map[string]string{"child": fixture.pinnedCommit})
	if err != nil {
		t.Fatalf("checkoutSubmodulesInWorktree: %v", err)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.pinnedCommit {
		t.Errorf("submodule HEAD = %q, want the replayed commit %q", got, fixture.pinnedCommit)
	}
}

func TestSubmoduleCommitsInWorktree_ReportsCurrentHeads(t *testing.T) {
	fixture := buildSubmoduleFixture(t)

	err := checkoutSubmodulesInWorktree(openFixtureWorktree(t, fixture),
		map[string]string{"child": fixture.pinnedCommit})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	commits, err := submoduleCommitsInWorktree(openFixtureWorktree(t, fixture))
	if err != nil {
		t.Fatalf("submoduleCommitsInWorktree: %v", err)
	}
	if got := commits["child"]; got != fixture.pinnedCommit {
		t.Errorf("child = %q, want %q", got, fixture.pinnedCommit)
	}
}

// The two backends must agree. If they resolved a floating submodule
// differently, the same bossy.json would produce different trees depending on
// whether the git binary happened to be on PATH.
func TestSubmoduleBackendsAgreeOnTheTip(t *testing.T) {
	native := buildSubmoduleFixture(t)
	dep := useFixtureAsModule(t, native)
	nativeCommits, err := AdvanceSubmodulesToBranchTips(dep)
	if err != nil {
		t.Fatalf("native advance: %v", err)
	}

	embedded := buildSubmoduleFixture(t)
	embeddedCommits, err := advanceSubmodulesInWorktree(openFixtureWorktree(t, embedded))
	if err != nil {
		t.Fatalf("embedded advance: %v", err)
	}

	// Different fixtures have different SHAs, so the agreement is asserted
	// against each fixture's own tip rather than against each other.
	if nativeCommits["child"] != native.tipCommit {
		t.Errorf("native resolved %q, want %q", nativeCommits["child"], native.tipCommit)
	}
	if embeddedCommits["child"] != embedded.tipCommit {
		t.Errorf("embedded resolved %q, want %q", embeddedCommits["child"], embedded.tipCommit)
	}
}

// useFixtureAsSeparateGitDir reproduces the layout a real install leaves
// behind: the worktree in modules/<name> with no .git of its own, and the git
// directory in the per-user cache.
//
// buildSubmoduleFixture makes a plain repository, where every git command finds
// its .git by walking up from the working directory. bossy's dependencies are
// not laid out that way, so a submodule command that passes against the plain
// fixture can still fail on a real install with "not a git repository".
func useFixtureAsSeparateGitDir(t *testing.T, fixture submoduleFixture) domain.Dependency {
	t.Helper()

	dep := useFixtureAsModule(t, fixture)
	gitDir := filepath.Join(env.GetCacheDir(), dep.HashName())
	if err := os.MkdirAll(filepath.Dir(gitDir), 0o755); err != nil {
		t.Fatalf("MkdirAll cache: %v", err)
	}
	if err := os.Rename(filepath.Join(fixture.superDir, ".git"), gitDir); err != nil {
		t.Fatalf("move git dir into the cache: %v", err)
	}

	// The submodule's own .git points at the superproject's git dir relatively,
	// so it has to follow the move. A real install never has this problem: its
	// submodules are initialised after the pointer is in place, and git writes
	// their paths against the cache to begin with.
	subPointer := filepath.Join(fixture.superDir, "child", ".git")
	if _, err := os.Stat(subPointer); err == nil {
		contents := "gitdir: " + filepath.Join(gitDir, "modules", "child") + "\n"
		if err := os.WriteFile(subPointer, []byte(contents), 0o600); err != nil {
			t.Fatalf("rewrite submodule pointer: %v", err)
		}
	}
	return dep
}

// The regression this guards: with the git directory in the cache, every
// `git submodule` command ran in a directory git did not recognise as a
// repository, so the remote policy failed on every dependency it was declared
// for while the plain-fixture tests above stayed green.
func TestAdvanceSubmodulesToBranchTips_WorksWithTheGitDirInTheCache(t *testing.T) {
	fixture := buildSubmoduleFixture(t)
	dep := useFixtureAsSeparateGitDir(t, fixture)

	commits, err := AdvanceSubmodulesToBranchTips(dep)
	if err != nil {
		t.Fatalf("AdvanceSubmodulesToBranchTips: %v", err)
	}
	if got := commits["child"]; got != fixture.tipCommit {
		t.Errorf("reported commit for child = %q, want the branch tip %q", got, fixture.tipCommit)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.tipCommit {
		t.Errorf("submodule HEAD = %q, want the branch tip %q", got, fixture.tipCommit)
	}
}

// The pointer is bossy's to manage, not the module tree's: the rest of the
// installer expects modules/<name> to carry no .git, so it must be gone again
// once the submodule work is done.
func TestAdvanceSubmodulesToBranchTips_LeavesNoGitPointerBehind(t *testing.T) {
	fixture := buildSubmoduleFixture(t)
	dep := useFixtureAsSeparateGitDir(t, fixture)

	if _, err := AdvanceSubmodulesToBranchTips(dep); err != nil {
		t.Fatalf("AdvanceSubmodulesToBranchTips: %v", err)
	}

	if _, err := os.Stat(filepath.Join(fixture.superDir, ".git")); !os.IsNotExist(err) {
		t.Errorf("modules/super/.git still exists after the advance (stat error: %v)", err)
	}
}

// Replaying a lock has to work in the same layout, which is the path every
// install after the first one takes.
func TestCheckoutSubmodules_WorksWithTheGitDirInTheCache(t *testing.T) {
	fixture := buildSubmoduleFixture(t)
	dep := useFixtureAsSeparateGitDir(t, fixture)

	if err := CheckoutSubmodules(dep, map[string]string{"child": fixture.pinnedCommit}); err != nil {
		t.Fatalf("CheckoutSubmodules: %v", err)
	}
	if got := submoduleHead(t, fixture.superDir); got != fixture.pinnedCommit {
		t.Errorf("submodule HEAD = %q, want the replayed commit %q", got, fixture.pinnedCommit)
	}
}
