//nolint:testpackage // Drives unexported installer internals end to end
package installer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitCache "github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitStorage "github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/filesystem"
	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	lockService "github.com/basti-fantasti/bossy/internal/core/services/lock"
	"github.com/basti-fantasti/bossy/internal/core/services/packages"
	"github.com/basti-fantasti/bossy/internal/core/services/tracker"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// fixtureRepoURL uses the reserved .invalid TLD (RFC 6761), so the ref-listing
// fetch inside resolveReference fails at DNS resolution instead of reaching a
// real host. getVersionsEmbedded only warns on a failed fetch and then reads
// refs from the local cache storer, which is what keeps this test hermetic.
const fixtureRepoURL = "https://fixture.invalid/owner/horse"

// fixture is an on-disk repository laid out exactly the way the installer
// expects one after a clone: the object store under $BOSS_HOME/cache/<hash>
// and the worktree under <cwd>/modules/<name>.
type fixture struct {
	dep  domain.Dependency
	repo *goGit.Repository
	wt   *goGit.Worktree
	dir  string
}

// newFixture isolates BOSS_HOME and the working directory, then builds the
// repository. Nothing outside the two temp directories is touched.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	t.Setenv("BOSS_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	dep := domain.ParseDependency(fixtureRepoURL, "develop")

	cacheDir := filepath.Join(env.GetCacheDir(), dep.HashName())
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	workDir := filepath.Join(env.GetModulesDir(), dep.Name())
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	storer := gitStorage.NewStorage(osfs.New(cacheDir), gitCache.NewObjectLRUDefault())
	repo, err := goGit.Init(storer, osfs.New(workDir))
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	if errRef := repo.Storer.SetReference(
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)); errRef != nil {
		t.Fatalf("set HEAD: %v", errRef)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	return &fixture{dep: dep, repo: repo, wt: wt, dir: workDir}
}

// commit writes a file and commits it on the currently checked-out ref.
func (f *fixture) commit(t *testing.T, name, content string) plumbing.Hash {
	t.Helper()

	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := f.wt.Add(name); err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	h, err := f.wt.Commit("commit "+name, &goGit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("commit %s: %v", name, err)
	}
	return h
}

// fixtureBranch is the branch the fixtures pin to. It is deliberately not
// "master", so a checkout that silently falls back to the default branch cannot
// be mistaken for a successful branch resolution.
const fixtureBranch = "develop"

// setDevelop points the fixture branch at a commit without moving the worktree,
// which is how these tests simulate upstream advancing.
func (f *fixture) setDevelop(t *testing.T, at plumbing.Hash) {
	t.Helper()
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(fixtureBranch), at)
	if err := f.repo.Storer.SetReference(ref); err != nil {
		t.Fatalf("set branch %s: %v", fixtureBranch, err)
	}
}

// head returns the commit the worktree is currently on.
func (f *fixture) head(t *testing.T) plumbing.Hash {
	t.Helper()
	head, err := f.repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	return head.Hash()
}

// newContext builds an installContext over the fixture, wired to the real lock
// service and on-disk repositories.
func newContext(t *testing.T, lock *domain.PackageLock, useLocked bool) *installContext {
	t.Helper()

	fs := filesystem.NewOSFileSystem()
	return &installContext{
		config:           env.GlobalConfiguration(),
		rootLocked:       lock,
		useLockedVersion: useLocked,
		progress:         &ProgressTracker{Tracker: tracker.NewNull[DependencyStatus]()},
		lockSvc:          lockService.NewLockService(repository.NewFileLockRepository(fs), fs),
		modulesDir:       env.GetModulesDir(),
		warnings:         make([]string, 0),
		visited:          map[string]bool{},
	}
}

func emptyLock() *domain.PackageLock {
	return &domain.PackageLock{Installed: map[string]domain.LockedDependency{}}
}

// TestEndToEnd_RawSHAPinChecksOutThatCommit is claim 1: a dependency pinned to
// a raw SHA in bossy.json must land the worktree on exactly that commit. The
// assertion is on the worktree HEAD, not on the lock — before the reference
// fix the lock could look plausible while the checkout had silently gone to
// master.
func TestEndToEnd_RawSHAPinChecksOutThatCommit(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")
	hashB := f.commit(t, "b.txt", "beta")
	if hashA == hashB {
		t.Fatalf("fixture produced identical commits: %s", hashA)
	}

	// Pin to the *older* commit so that "checked out master" and "honoured the
	// pin" are distinguishable: the worktree currently sits on B.
	if got := f.head(t); got != hashB {
		t.Fatalf("precondition: worktree is on %s, want tip %s", got, hashB)
	}

	dep := domain.ParseDependency(fixtureRepoURL, hashA.String())
	pkg := &domain.Package{Dependencies: map[string]string{fixtureRepoURL: hashA.String()}}
	lock := emptyLock()
	ic := newContext(t, lock, false)

	ref := ic.resolveReference(pkg, dep, f.repo)
	if ref.Hash() != hashA {
		t.Fatalf("resolved hash = %s, want %s", ref.Hash(), hashA)
	}

	if err := ic.checkoutAndUpdate(dep, f.repo, ref); err != nil {
		t.Fatalf("checkoutAndUpdate: %v", err)
	}

	if got := f.head(t); got != hashA {
		t.Errorf("worktree HEAD = %s, want the pinned commit %s", got, hashA)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "b.txt")); err == nil {
		t.Error("b.txt is still present: the worktree was not moved off the tip")
	}

	locked := lock.GetInstalled(dep)
	if locked.Commit != hashA.String() {
		t.Errorf("lock commit = %q, want %q", locked.Commit, hashA.String())
	}
	if locked.Version != hashA.String() {
		t.Errorf("lock version = %q, want the pinned SHA %q", locked.Version, hashA.String())
	}
}

// TestEndToEnd_BranchPinRecordsTipInLock is claim 2: a dependency pinned to a
// branch resolves through the real version listing, checks out the branch, and
// records the branch tip SHA under the branch name as the version.
func TestEndToEnd_BranchPinRecordsTipInLock(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")
	f.setDevelop(t, hashA)

	dep := domain.ParseDependency(fixtureRepoURL, "develop")
	pkg := &domain.Package{Dependencies: map[string]string{fixtureRepoURL: "develop"}}
	lock := emptyLock()
	ic := newContext(t, lock, false)

	ref := ic.resolveReference(pkg, dep, f.repo)
	if got := ref.Name().Short(); got != "develop" {
		t.Fatalf("resolved reference = %q, want the develop branch", ref.Name())
	}

	if err := ic.checkoutAndUpdate(dep, f.repo, ref); err != nil {
		t.Fatalf("checkoutAndUpdate: %v", err)
	}

	locked := lock.GetInstalled(dep)
	if locked.Version != "develop" {
		t.Errorf("lock version = %q, want %q", locked.Version, "develop")
	}
	if locked.Commit != hashA.String() {
		t.Errorf("lock commit = %q, want the branch tip %q", locked.Commit, hashA.String())
	}

	// bossy.json must keep the branch pin: only a MinimalDependencyVersion
	// declaration may be rewritten to a resolved range.
	if got := pkg.Dependencies[fixtureRepoURL]; got != "develop" {
		t.Errorf("bossy.json version = %q, want %q", got, "develop")
	}
}

// TestEndToEnd_LockReplayReproducesLockedCommit is claim 3, and the property
// the whole CI goal rests on: with a lock in hand, re-installing after the
// upstream branch has advanced must reproduce the *locked* commit, not the new
// tip. Before the fixes this could not hold — the lock was never read back
// (bug 1) and, even when it was, the pinned hash was discarded on the way to
// the checkout (bug 2).
func TestEndToEnd_LockReplayReproducesLockedCommit(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")
	f.setDevelop(t, hashA)

	dep := domain.ParseDependency(fixtureRepoURL, "develop")
	pkg := &domain.Package{Dependencies: map[string]string{fixtureRepoURL: "develop"}}

	// First install: records develop@A.
	lock := emptyLock()
	first := newContext(t, lock, false)
	ref := first.resolveReference(pkg, dep, f.repo)
	if err := first.checkoutAndUpdate(dep, f.repo, ref); err != nil {
		t.Fatalf("first checkoutAndUpdate: %v", err)
	}
	if got := lock.GetInstalled(dep).Commit; got != hashA.String() {
		t.Fatalf("first install locked %q, want %q", got, hashA)
	}

	// Upstream advances: develop now points at B.
	hashB := f.commit(t, "b.txt", "beta")
	f.setDevelop(t, hashB)
	if hashA == hashB {
		t.Fatalf("fixture produced identical commits: %s", hashA)
	}

	// Persist and reload the lock through the same services `bossy install`
	// uses, so this also covers the filename agreement from bug 1.
	fs := filesystem.NewOSFileSystem()
	lockSvc := lockService.NewLockService(repository.NewFileLockRepository(fs), fs)
	if err := lockSvc.Save(lock, env.GetCurrentDir()); err != nil {
		t.Fatalf("save lock: %v", err)
	}
	manifest := filepath.Join(env.GetCurrentDir(), consts.FilePackage)
	if err := os.WriteFile(manifest, []byte(`{"name":"test"}`), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	pkgSvc := packages.NewPackageService(repository.NewFilePackageRepository(fs),
		repository.NewFileLockRepository(fs))
	reloaded, err := pkgSvc.Load(manifest)
	if err != nil {
		t.Fatalf("reload package: %v", err)
	}
	if got := reloaded.Lock.GetInstalled(dep).Commit; got != hashA.String() {
		t.Fatalf("lock did not survive the disk round trip: commit = %q, want %q", got, hashA)
	}

	// Second install, replaying the reloaded lock.
	second := newContext(t, &reloaded.Lock, true)
	replayRef := second.resolveReference(pkg, dep, f.repo)
	if replayRef.Hash() != hashA {
		t.Fatalf("replay resolved %s, want the locked commit %s", replayRef.Hash(), hashA)
	}
	if err := second.checkoutAndUpdate(dep, f.repo, replayRef); err != nil {
		t.Fatalf("second checkoutAndUpdate: %v", err)
	}

	if got := f.head(t); got != hashA {
		t.Errorf("worktree HEAD = %s, want the locked commit %s (branch tip is %s)", got, hashA, hashB)
	}
	replayed := reloaded.Lock.GetInstalled(dep)
	if replayed.Commit != hashA.String() {
		t.Errorf("lock commit after replay = %q, want %q", replayed.Commit, hashA)
	}
	if replayed.Version != "develop" {
		t.Errorf("lock version after replay = %q, want %q — the label must not be rewritten",
			replayed.Version, "develop")
	}
}

// TestEndToEnd_UpdateMovesToNewTip is claim 4: `bossy update` resolves afresh
// rather than replaying the lock, so it moves to the new branch tip and
// rewrites the lock.
func TestEndToEnd_UpdateMovesToNewTip(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")
	f.setDevelop(t, hashA)

	dep := domain.ParseDependency(fixtureRepoURL, "develop")
	pkg := &domain.Package{Dependencies: map[string]string{fixtureRepoURL: "develop"}}

	lock := emptyLock()
	lock.Installed[dep.GetKey()] = domain.LockedDependency{
		Name: dep.Name(), Version: "develop", Commit: hashA.String(),
	}

	hashB := f.commit(t, "b.txt", "beta")
	f.setDevelop(t, hashB)

	// useLockedVersion=false is what `bossy update` passes.
	ic := newContext(t, lock, false)
	ref := ic.resolveReference(pkg, dep, f.repo)
	if ref.Hash() == hashA {
		t.Fatalf("update resolved to the locked commit %s; it must resolve afresh", hashA)
	}
	if err := ic.checkoutAndUpdate(dep, f.repo, ref); err != nil {
		t.Fatalf("checkoutAndUpdate: %v", err)
	}

	if got := f.head(t); got != hashB {
		t.Errorf("worktree HEAD = %s, want the new tip %s", got, hashB)
	}
	locked := lock.GetInstalled(dep)
	if locked.Commit != hashB.String() {
		t.Errorf("lock commit = %q, want the new tip %q", locked.Commit, hashB)
	}
	if locked.Version != "develop" {
		t.Errorf("lock version = %q, want %q", locked.Version, "develop")
	}
}
