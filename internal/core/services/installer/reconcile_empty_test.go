//nolint:testpackage // Drives the unexported reconciliation seam directly
package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/filesystem"
	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/packages"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/pkgmanager"
)

// emptyTreeFixture isolates BOSS_HOME and the working directory, wires the
// package-service singleton that setup normally installs at startup, and
// returns the modules directory. Nothing outside the two temp directories is
// touched.
func emptyTreeFixture(t *testing.T) string {
	t.Helper()

	t.Setenv("BOSS_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	fs := filesystem.NewOSFileSystem()
	previous := pkgmanager.GetInstance()
	pkgmanager.SetInstance(packages.NewPackageService(
		repository.NewFilePackageRepository(fs), repository.NewFileLockRepository(fs)))
	t.Cleanup(func() { pkgmanager.SetInstance(previous) })

	return env.GetModulesDir()
}

// installModule creates modules/<name> with a file in it, the way a real
// checkout leaves one behind.
func installModule(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(env.GetModulesDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unit.pas"), []byte("unit Unit1;"), 0o600); err != nil {
		t.Fatalf("write into %s: %v", dir, err)
	}
	return dir
}

// TestReconcileEmpty_LastDependencyRemoved is the defect. `bossy uninstall`
// drops the entry from bossy.json and then re-runs the installer to reconcile —
// which worked for every dependency except the last one, because an empty
// dependency set returned before the reconciliation pass. The module directory
// and the lock entry both survived a removal that reported success.
func TestReconcileEmpty_LastDependencyRemoved(t *testing.T) {
	emptyTreeFixture(t)
	moduleDir := installModule(t, "horse")

	// bossy.json no longer declares horse; the lock still records it.
	pkg := &domain.Package{
		Dependencies: map[string]string{},
		Lock: domain.PackageLock{Installed: map[string]domain.LockedDependency{
			"github.com/hashload/horse": {Name: "horse", Version: "3.3.5"},
		}},
	}

	// Preconditions, so neither assertion below can pass vacuously.
	if _, err := os.Stat(moduleDir); err != nil {
		t.Fatalf("precondition: module dir %s is not there to begin with: %v", moduleDir, err)
	}
	if len(pkg.Lock.Installed) != 1 {
		t.Fatalf("precondition: lock holds %d entries, want 1", len(pkg.Lock.Installed))
	}

	opts := InstallOptions{Args: []string{}}
	if !shouldReconcileEmpty(opts, pkg) {
		t.Fatal("an emptied dependency set was treated as nothing to do")
	}

	if err := reconcileEmptyTree(opts, pkg); err != nil {
		t.Fatalf("reconcileEmptyTree: %v", err)
	}

	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Errorf("module dir %s survived the removal of the last dependency", moduleDir)
	}
	if len(pkg.Lock.Installed) != 0 {
		t.Errorf("lock still holds %v after the last dependency was removed", pkg.Lock.Installed)
	}
	if _, err := os.Stat(filepath.Join(env.GetCurrentDir(), consts.FilePackageLock)); err != nil {
		t.Errorf("the emptied lock was not written back: %v", err)
	}
}

// TestReconcileEmpty_NoDependenciesIsANoOp guards the other side. A project that
// simply has no dependencies must stay quiet and fast: nothing to reconcile
// means nothing is read, created or written — in particular no lock file is
// brought into being where none existed.
func TestReconcileEmpty_NoDependenciesIsANoOp(t *testing.T) {
	modulesDir := emptyTreeFixture(t)

	pkg := &domain.Package{
		Dependencies: map[string]string{},
		Lock:         domain.PackageLock{Installed: map[string]domain.LockedDependency{}},
	}

	opts := InstallOptions{Args: []string{}}
	if shouldReconcileEmpty(opts, pkg) {
		t.Fatal("a project with nothing installed wanted a reconciliation pass")
	}

	if err := reconcileEmptyTree(opts, pkg); err != nil {
		t.Fatalf("reconcileEmptyTree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(env.GetCurrentDir(), consts.FilePackageLock)); !os.IsNotExist(err) {
		t.Error("a lock file was created for a project that has no dependencies and no lock")
	}
	if _, err := os.Stat(modulesDir); !os.IsNotExist(err) {
		t.Error("a modules directory was created for a project that has no dependencies")
	}
}

// TestReconcileEmpty_DefaultPathsAreNotOrphans covers a project that has been
// installed before and then emptied out entirely: modules/ holds only the
// directories bossy creates for itself. Those are not leftovers of a removed
// dependency, so there is still nothing to do.
func TestReconcileEmpty_DefaultPathsAreNotOrphans(t *testing.T) {
	modulesDir := emptyTreeFixture(t)

	for _, name := range consts.DefaultPaths() {
		if err := os.MkdirAll(filepath.Join(modulesDir, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	pkg := &domain.Package{
		Dependencies: map[string]string{},
		Lock:         domain.PackageLock{Installed: map[string]domain.LockedDependency{}},
	}

	if shouldReconcileEmpty(InstallOptions{Args: []string{}}, pkg) {
		t.Errorf("bossy's own directories %v were mistaken for orphaned modules", consts.DefaultPaths())
	}
}

// TestReconcileEmpty_TargetedInstallLeavesOtherModulesAlone is the regression
// guard. `bossy install <pkg>` only ever processes the dependency it was given,
// so it must not reconcile against that subset — doing so wipes every other
// installed module. A targeted run that resolves to no dependencies at all is
// the sharpest version of that: reconciling would delete the whole tree.
func TestReconcileEmpty_TargetedInstallLeavesOtherModulesAlone(t *testing.T) {
	emptyTreeFixture(t)
	otherModule := installModule(t, "jhonson")

	pkg := &domain.Package{
		Dependencies: map[string]string{"github.com/hashload/jhonson": "^2.0.0"},
		Lock: domain.PackageLock{Installed: map[string]domain.LockedDependency{
			"github.com/hashload/jhonson": {Name: "jhonson", Version: "2.0.0"},
		}},
	}

	// Args names a dependency, so this is a targeted run whatever it resolved to.
	opts := InstallOptions{Args: []string{"github.com/hashload/horse"}}
	if shouldReconcileEmpty(opts, pkg) {
		t.Fatal("a targeted install wanted to reconcile against its own subset")
	}

	if err := reconcileEmptyTree(opts, pkg); err != nil {
		t.Fatalf("reconcileEmptyTree: %v", err)
	}

	if _, err := os.Stat(otherModule); err != nil {
		t.Errorf("targeted install wiped the unrelated module %s: %v", otherModule, err)
	}
	if len(pkg.Lock.Installed) != 1 {
		t.Errorf("targeted install cleared unrelated lock entries: %v", pkg.Lock.Installed)
	}
}
