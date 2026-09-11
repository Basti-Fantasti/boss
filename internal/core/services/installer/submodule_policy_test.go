//nolint:testpackage // exercises the unexported install context directly
package installer

import (
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/tracker"
)

const (
	taurusRepo   = "github.com/JPeterMugaas/TaurusTLS"
	pinnedCommit = "1111111111111111111111111111111111111111"
)

// newPolicyContext builds an install context around a manifest and a lock.
func newPolicyContext(t *testing.T, pkg *domain.Package, lock *domain.PackageLock, locked bool) *installContext {
	t.Helper()
	return &installContext{
		root:             pkg,
		rootLocked:       lock,
		useLockedVersion: locked,
		progress:         &ProgressTracker{Tracker: tracker.NewNull[DependencyStatus]()},
		warnings:         make([]string, 0),
	}
}

// emptyLock is shared with lock_reproducibility_test.go.

// The default policy is git's own, which the clone already applied. Nothing may
// happen here — in particular no attempt to reach a repository that the test
// has not created, which would fail loudly if the guard were missing.
func TestApplySubmodulePolicy_PinnedIsANoOp(t *testing.T) {
	t.Parallel()

	for _, policy := range []string{"", string(domain.SubmodulePolicyPinned)} {
		pkg := domain.NewPackage()
		if policy != "" {
			pkg.Submodules = map[string]string{taurusRepo: policy}
		}
		dep := domain.ParseDependency(taurusRepo, "main")
		ctx := newPolicyContext(t, pkg, emptyLock(), true)

		ctx.applySubmodulePolicy(dep)

		if len(ctx.warnings) != 0 {
			t.Errorf("policy %q produced warnings %v; it should do nothing at all", policy, ctx.warnings)
		}
		if len(ctx.rootLocked.Installed) != 0 {
			t.Errorf("policy %q touched the lock: %+v", policy, ctx.rootLocked.Installed)
		}
	}
}

// Replaying a lock must use the recorded commits rather than chasing the branch
// tips again. Without a real worktree the checkout fails, but the point stands:
// the failure names the replay, which proves the replay branch was taken.
func TestApplySubmodulePolicy_LockedRunReplaysRatherThanResolves(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	pkg.Submodules = map[string]string{taurusRepo: string(domain.SubmodulePolicyRemote)}

	dep := domain.ParseDependency(taurusRepo, "main")
	lock := emptyLock()
	lock.Installed[dep.GetKey()] = domain.LockedDependency{
		Name:       dep.Name(),
		Version:    "main",
		Submodules: map[string]string{"lib/indy": pinnedCommit},
	}

	ctx := newPolicyContext(t, pkg, lock, true)
	ctx.applySubmodulePolicy(dep)

	if len(ctx.warnings) == 0 {
		t.Fatal("expected the replay to be attempted and to fail without a worktree")
	}
	if !strings.Contains(ctx.warnings[0], "replay locked submodule commits") {
		t.Errorf("warning = %q; want it to name the replay, not the advance", ctx.warnings[0])
	}

	// The recorded commits must survive a failed replay: dropping them would
	// silently turn the next install into a re-resolve.
	if got := lock.Installed[dep.GetKey()].Submodules["lib/indy"]; got != pinnedCommit {
		t.Errorf("locked submodule commit = %q, want it untouched (%q)", got, pinnedCommit)
	}
}

// A lock with no submodule record has nothing to replay, so even a locked run
// has to resolve — that is how the first install after declaring the policy
// gets its commits.
func TestApplySubmodulePolicy_LockedRunWithoutRecordResolves(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	pkg.Submodules = map[string]string{taurusRepo: string(domain.SubmodulePolicyRemote)}

	dep := domain.ParseDependency(taurusRepo, "main")
	ctx := newPolicyContext(t, pkg, emptyLock(), true)

	ctx.applySubmodulePolicy(dep)

	if len(ctx.warnings) == 0 {
		t.Fatal("expected the advance to be attempted and to fail without a worktree")
	}
	if !strings.Contains(ctx.warnings[0], "advance submodules") {
		t.Errorf("warning = %q; want it to name the advance", ctx.warnings[0])
	}
}

// `bossy update` re-resolves the dependency, so it must re-resolve the
// submodules too rather than replaying stale commits.
func TestApplySubmodulePolicy_UnlockedRunResolvesDespiteARecord(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	pkg.Submodules = map[string]string{taurusRepo: string(domain.SubmodulePolicyRemote)}

	dep := domain.ParseDependency(taurusRepo, "main")
	lock := emptyLock()
	lock.Installed[dep.GetKey()] = domain.LockedDependency{
		Name:       dep.Name(),
		Version:    "main",
		Submodules: map[string]string{"lib/indy": pinnedCommit},
	}

	ctx := newPolicyContext(t, pkg, lock, false)
	ctx.applySubmodulePolicy(dep)

	if len(ctx.warnings) == 0 {
		t.Fatal("expected the advance to be attempted")
	}
	if !strings.Contains(ctx.warnings[0], "advance submodules") {
		t.Errorf("warning = %q; want an advance, not a replay", ctx.warnings[0])
	}
}

// A submodule problem must not abort the install. The dependency itself is
// checked out correctly; the submodules just sit where the superproject put
// them, which is git's default and a coherent state.
func TestApplySubmodulePolicy_FailureIsAWarningNotAFault(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	pkg.Submodules = map[string]string{taurusRepo: string(domain.SubmodulePolicyRemote)}

	dep := domain.ParseDependency(taurusRepo, "main")
	ctx := newPolicyContext(t, pkg, emptyLock(), false)

	// applySubmodulePolicy returns nothing; the assertion is that it returns at
	// all rather than calling msg.Die, and that the problem was recorded.
	ctx.applySubmodulePolicy(dep)

	if len(ctx.warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", ctx.warnings)
	}
	if !strings.Contains(ctx.warnings[0], dep.Name()) {
		t.Errorf("warning %q does not name the dependency", ctx.warnings[0])
	}
}

// The policy is matched on the module directory name, so declaring it under the
// bare name works as well as under the full repository key.
func TestApplySubmodulePolicy_MatchesBareModuleName(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	pkg.Submodules = map[string]string{"TaurusTLS": string(domain.SubmodulePolicyRemote)}

	dep := domain.ParseDependency(taurusRepo, "main")
	ctx := newPolicyContext(t, pkg, emptyLock(), false)

	ctx.applySubmodulePolicy(dep)

	if len(ctx.warnings) == 0 {
		t.Error("the bare module name did not match, so the policy was skipped")
	}
}
