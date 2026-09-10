//nolint:testpackage // Drives the unexported skip decision directly
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// lockAt registers dep in lock at the given version and commit, and fails the
// test if the entry is not readable back. Every "must not skip" assertion below
// is vacuous unless the dependency really is in the lock — that is the only
// reason shouldSkipDependency gets as far as comparing anything.
func lockAt(t *testing.T, lock *domain.PackageLock, dep domain.Dependency, version, commit string) {
	t.Helper()

	lock.Installed[dep.GetKey()] = domain.LockedDependency{
		Name:    dep.Name(),
		Version: version,
		Commit:  commit,
	}

	installed, exists := lock.Installed[dep.GetKey()]
	if !exists {
		t.Fatalf("precondition: %s is not in the lock under key %q", dep.Name(), dep.GetKey())
	}
	if installed.Version != version || installed.Commit != commit {
		t.Fatalf("precondition: lock entry = %+v, want version %q commit %q", installed, version, commit)
	}
}

// TestShouldSkip_WorktreeAtLockedCommit is the happy path the whole skip
// optimisation exists for: the lock names a commit, the worktree is on it, so
// there is nothing to do.
func TestShouldSkip_WorktreeAtLockedCommit(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")

	dep := domain.ParseDependency(fixtureRepoURL, "^1.0.0")
	lock := emptyLock()
	lockAt(t, lock, dep, "1.0.0", hashA.String())

	if got := f.head(t); got != hashA {
		t.Fatalf("precondition: worktree is on %s, want %s", got, hashA)
	}

	ic := newContext(t, lock, true)
	if !ic.shouldSkipDependency(dep) {
		t.Error("dependency was not skipped although the worktree is at the locked commit")
	}
	if len(ic.warnings) != 0 {
		t.Errorf("skipping emitted warnings: %v", ic.warnings)
	}
}

// TestShouldSkip_DriftedWorktreeIsNotSkipped is the correctness fix. The lock
// entry is perfectly satisfiable on version strings alone — 1.0.0 against
// ^1.0.0 — but the worktree has been moved to another commit. Deciding on the
// version would build the wrong code and report success, so the commit has to
// be the test that counts.
func TestShouldSkip_DriftedWorktreeIsNotSkipped(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")
	hashB := f.commit(t, "b.txt", "beta")
	if hashA == hashB {
		t.Fatalf("fixture produced identical commits: %s", hashA)
	}

	dep := domain.ParseDependency(fixtureRepoURL, "^1.0.0")
	lock := emptyLock()
	lockAt(t, lock, dep, "1.0.0", hashA.String())

	// The worktree sits on B while the lock says A: drift.
	if got := f.head(t); got != hashB {
		t.Fatalf("precondition: worktree is on %s, want the drifted commit %s", got, hashB)
	}

	ic := newContext(t, lock, true)
	if ic.shouldSkipDependency(dep) {
		t.Errorf("drifted worktree was skipped: HEAD is %s, lock says %s", hashB, hashA)
	}
}

// TestShouldSkip_MissingModuleIsNotSkipped covers the ordinary first install on
// a machine that has the lock but no modules/ yet: the object cache can still
// be warm, so nothing but the module directory itself proves the checkout is
// there. Reinstalling is correct, and it is not a fault worth reporting.
func TestShouldSkip_MissingModuleIsNotSkipped(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")

	dep := domain.ParseDependency(fixtureRepoURL, "^1.0.0")
	lock := emptyLock()
	lockAt(t, lock, dep, "1.0.0", hashA.String())

	if err := os.RemoveAll(f.dir); err != nil {
		t.Fatalf("remove module dir: %v", err)
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Fatalf("precondition: module dir %s still present", f.dir)
	}

	ic := newContext(t, lock, true)
	if ic.shouldSkipDependency(dep) {
		t.Error("a dependency with no module directory was skipped")
	}
	if len(ic.warnings) != 0 {
		t.Errorf("a missing module is the normal first-install case, but it warned: %v", ic.warnings)
	}
}

// TestShouldSkip_NonRepositoryModuleIsNotSkipped covers a module directory that
// exists but carries no git metadata — an interrupted clone, or a directory a
// user dropped in by hand. HEAD is unreadable, so the commit claim in the lock
// cannot be confirmed and the dependency must be reinstalled, quietly.
func TestShouldSkip_NonRepositoryModuleIsNotSkipped(t *testing.T) {
	newFixture(t)

	// A second repository URL, so its object cache is absent too.
	dep := domain.ParseDependency("https://fixture.invalid/owner/donkey", "^1.0.0")
	moduleDir := filepath.Join(env.GetModulesDir(), dep.Name())
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}

	lock := emptyLock()
	lockAt(t, lock, dep, "1.0.0", "0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21")

	ic := newContext(t, lock, true)
	if ic.shouldSkipDependency(dep) {
		t.Error("a module directory that is not a repository was skipped")
	}
	if len(ic.warnings) != 0 {
		t.Errorf("an unreadable module warned instead of just reinstalling: %v", ic.warnings)
	}
}

// TestShouldSkip_LegacyLockNonSemverPinsDoNotWarn covers the two supported pin
// shapes that are not version ranges. Against a lock written before commits
// were recorded there is nothing to compare them to, so the answer is
// "reinstall" — which is safe and correct. What must not happen is the pin
// being reported to the user as an error on every single install.
func TestShouldSkip_LegacyLockNonSemverPinsDoNotWarn(t *testing.T) {
	const sha = "0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21"

	tests := []struct {
		name          string
		declared      string
		lockedVersion string
	}{
		{name: "branch pin", declared: "master", lockedVersion: "master"},
		{name: "raw SHA pin", declared: sha, lockedVersion: sha},
		{name: "range declared, branch locked", declared: "^1.0.0", lockedVersion: "master"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newFixture(t)

			dep := domain.ParseDependency(fixtureRepoURL, tt.declared)
			lock := emptyLock()
			// No Commit: a lock written before commit tracking existed.
			lockAt(t, lock, dep, tt.lockedVersion, "")

			ic := newContext(t, lock, true)
			if ic.shouldSkipDependency(dep) {
				t.Error("a legacy lock entry with no commit was skipped without any check")
			}
			if len(ic.warnings) != 0 {
				t.Errorf("a supported pin was reported as an error: %v", ic.warnings)
			}
			for _, w := range ic.warnings {
				if strings.Contains(w, "Error") {
					t.Errorf("warning text calls a supported pin an error: %q", w)
				}
			}
		})
	}
}

// TestShouldSkip_LegacyLockSemverComparison pins the fallback's behaviour so
// the commit-first rewrite cannot quietly change it for locks that predate the
// commit field.
func TestShouldSkip_LegacyLockSemverComparison(t *testing.T) {
	tests := []struct {
		name          string
		declared      string
		lockedVersion string
		wantSkip      bool
	}{
		{name: "installed above required", declared: "^1.0.0", lockedVersion: "1.2.0", wantSkip: true},
		{name: "installed equal to required", declared: "^1.2.0", lockedVersion: "1.2.0", wantSkip: true},
		{name: "installed below required", declared: "^1.3.0", lockedVersion: "1.2.0", wantSkip: false},
		{name: "tilde range, installed above", declared: "~1.2.0", lockedVersion: "1.2.5", wantSkip: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newFixture(t)

			dep := domain.ParseDependency(fixtureRepoURL, tt.declared)
			lock := emptyLock()
			lockAt(t, lock, dep, tt.lockedVersion, "")

			ic := newContext(t, lock, true)
			if got := ic.shouldSkipDependency(dep); got != tt.wantSkip {
				t.Errorf("shouldSkipDependency = %v, want %v (declared %q, locked %q)",
					got, tt.wantSkip, tt.declared, tt.lockedVersion)
			}
			if len(ic.warnings) != 0 {
				t.Errorf("unexpected warnings: %v", ic.warnings)
			}
		})
	}
}

// TestShouldSkip_GatesAreUnchanged keeps the three early exits honest: a forced
// update, a resolve-fresh install, and an unlocked dependency all reinstall
// regardless of what the worktree looks like.
func TestShouldSkip_GatesAreUnchanged(t *testing.T) {
	f := newFixture(t)
	hashA := f.commit(t, "a.txt", "alpha")

	dep := domain.ParseDependency(fixtureRepoURL, "^1.0.0")

	t.Run("force update wins over a matching commit", func(t *testing.T) {
		lock := emptyLock()
		lockAt(t, lock, dep, "1.0.0", hashA.String())
		ic := newContext(t, lock, true)
		// The repository key, not dep.Name(): see
		// TestShouldSkip_ForceUpdateMatchesTheRepositoryKey.
		ic.options = InstallOptions{ForceUpdate: []string{dep.Repository}}
		if ic.shouldSkipDependency(dep) {
			t.Error("a forced dependency was skipped")
		}
	})

	t.Run("resolve-fresh install never skips", func(t *testing.T) {
		lock := emptyLock()
		lockAt(t, lock, dep, "1.0.0", hashA.String())
		ic := newContext(t, lock, false)
		if ic.shouldSkipDependency(dep) {
			t.Error("a dependency was skipped although the lock is not being replayed")
		}
	})

	t.Run("dependency absent from the lock never skips", func(t *testing.T) {
		lock := emptyLock()
		if _, exists := lock.Installed[dep.GetKey()]; exists {
			t.Fatalf("precondition: lock should be empty")
		}
		ic := newContext(t, lock, true)
		if ic.shouldSkipDependency(dep) {
			t.Error("a dependency missing from the lock was skipped")
		}
	})
}

// TestShouldSkip_LegacyLockNeedsTheModuleOnDisk is the counterpart to
// TestShouldSkip_MissingModuleIsNotSkipped for lock entries written before the
// commit field existed. Such an entry offers nothing but a version string, and
// a version string says what was asked for, never what is on disk. Any lock
// produced by an older bossy looks like this, so trusting it on a clean CI
// checkout skips the dependency, never creates modules/, and leaves nothing to
// build — while reporting success.
func TestShouldSkip_LegacyLockNeedsTheModuleOnDisk(t *testing.T) {
	tests := []struct {
		name          string
		moduleOnDisk  bool
		wantSkip      bool
		wantNoWarning bool
	}{
		{name: "module present", moduleOnDisk: true, wantSkip: true},
		{name: "module absent", moduleOnDisk: false, wantSkip: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)

			// ^3.0.0 declared, 3.3.5 locked: satisfied on version strings alone,
			// so the version comparison on its own would answer "skip".
			dep := domain.ParseDependency(fixtureRepoURL, "^3.0.0")
			lock := emptyLock()
			lockAt(t, lock, dep, "3.3.5", "")

			if !tt.moduleOnDisk {
				if err := os.RemoveAll(f.dir); err != nil {
					t.Fatalf("remove module dir: %v", err)
				}
			}
			_, statErr := os.Stat(f.dir)
			if present := statErr == nil; present != tt.moduleOnDisk {
				t.Fatalf("precondition: module dir %s present = %v, want %v", f.dir, present, tt.moduleOnDisk)
			}

			ic := newContext(t, lock, true)
			if got := ic.shouldSkipDependency(dep); got != tt.wantSkip {
				t.Errorf("shouldSkipDependency = %v, want %v (module on disk = %v)",
					got, tt.wantSkip, tt.moduleOnDisk)
			}
			if len(ic.warnings) != 0 {
				t.Errorf("a legacy lock entry is not a fault, but it warned: %v", ic.warnings)
			}
		})
	}
}

// TestShouldSkip_ForceUpdateMatchesTheRepositoryKey pins the shape of the
// ForceUpdate entries. Every producer of ForceUpdate — the `update --select`
// picker is the only one — collects repository keys, the same strings that key
// bossy.json and InstallOptions.Args. Matching them against the short module
// name instead never fired, so selecting a dependency had no effect at all.
//
// The short name is also the wrong key on its own terms: two hosts can each
// publish a "horse", and only the repository tells them apart.
func TestShouldSkip_ForceUpdateMatchesTheRepositoryKey(t *testing.T) {
	tests := []struct {
		name        string
		forceUpdate []string
		wantSkip    bool
	}{
		{name: "repository key forces", forceUpdate: []string{fixtureRepoURL}, wantSkip: false},
		{
			name:        "repository key is matched case-insensitively",
			forceUpdate: []string{strings.ToUpper(fixtureRepoURL)},
			wantSkip:    false,
		},
		{
			name:        "a bare name forces the dependency it expands to",
			forceUpdate: []string{"github.com/hashload/horse"},
			wantSkip:    true, // expands to a different host than the fixture
		},
		{name: "another repository does not force", forceUpdate: []string{"github.com/hashload/jhonson"}, wantSkip: true},
		{name: "nothing forced", forceUpdate: nil, wantSkip: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			hashA := f.commit(t, "a.txt", "alpha")

			dep := domain.ParseDependency(fixtureRepoURL, "^1.0.0")
			lock := emptyLock()
			lockAt(t, lock, dep, "1.0.0", hashA.String())

			// Without ForceUpdate this is the happy path: worktree at the
			// locked commit, so anything but a force means "skip".
			if got := f.head(t); got != hashA {
				t.Fatalf("precondition: worktree is on %s, want %s", got, hashA)
			}

			ic := newContext(t, lock, true)
			ic.options = InstallOptions{ForceUpdate: tt.forceUpdate}
			if got := ic.shouldSkipDependency(dep); got != tt.wantSkip {
				t.Errorf("shouldSkipDependency = %v, want %v (ForceUpdate = %v, repository = %q)",
					got, tt.wantSkip, tt.forceUpdate, dep.Repository)
			}
		})
	}
}

// TestCollectDependencies_SelectionLeavesTheRestAlone is the other half of
// `update --select`: the selected dependencies are the only ones the run
// touches. Args carries the selection, so an unselected dependency is neither
// re-resolved nor reconciled away.
func TestCollectDependencies_SelectionLeavesTheRestAlone(t *testing.T) {
	const (
		horse   = "github.com/hashload/horse"
		jhonson = "github.com/hashload/jhonson"
	)

	pkg := &domain.Package{Dependencies: map[string]string{
		horse:   "^1.0.0",
		jhonson: "^2.0.0",
	}}

	selected := []string{horse}
	deps := collectDependenciesToInstall(pkg, selected)

	if len(deps) != 1 {
		t.Fatalf("collected %d dependencies from a selection of 1: %v", len(deps), deps)
	}
	if deps[0].Repository != horse {
		t.Errorf("collected %q, want the selected %q", deps[0].Repository, horse)
	}

	if shouldReconcile(selectionOptions(selected)) {
		t.Error("a targeted selection would reconcile, wiping the unselected dependency's module")
	}
}

// selectionOptions is a shorthand for the options a selection produces.
func selectionOptions(args []string) InstallOptions {
	return InstallOptions{Args: args, ForceUpdate: args}
}
