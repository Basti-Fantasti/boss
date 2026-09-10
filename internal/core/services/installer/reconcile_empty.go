package installer

import (
	"os"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/paths"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/pkg/pkgmanager"
	"github.com/basti-fantasti/bossy/utils"
	"github.com/basti-fantasti/bossy/utils/librarypath"
)

// reconcileEmptyTree finishes a run that found no dependencies to install.
//
// An empty dependency set is a legitimate state, not a reason to stop. Removing
// the last entry from bossy.json leaves modules/<name> and its lock entry
// behind, and only the reconciliation pass takes them away — so returning early
// made `bossy uninstall` incomplete for the last dependency while working for
// every other one.
//
// Nothing is read, created or written when there is nothing to clean up: a
// project that simply has no dependencies stays quiet, and a lock that does not
// exist is not brought into being.
func reconcileEmptyTree(options InstallOptions, pkg *domain.Package) error {
	if !shouldReconcileEmpty(options, pkg) {
		return nil
	}

	// Same order as the full install path: EnsureCleanModulesDir reads the lock
	// to decide which build artifacts to keep, so it runs before CleanRemoved
	// empties it.
	paths.EnsureCleanModulesDir(nil, pkg.Lock)
	pkg.Lock.CleanRemoved(nil)

	if err := pkgmanager.SavePackageCurrent(pkg); err != nil {
		msg.Warn("⚠️ Failed to save package: %v", err)
	}
	if err := createLockService().Save(&pkg.Lock, env.GetCurrentDir()); err != nil {
		msg.Warn("⚠️ Failed to save lock file: %v", err)
	}

	// The IDE still points at the modules that were just deleted.
	librarypath.UpdateLibraryPath(pkg)

	return nil
}

// shouldReconcileEmpty reports whether a run with no dependencies to install
// still has state to reconcile.
//
// A targeted invocation is excluded outright: `bossy install <pkg>` processes
// only the dependency it was given, so reconciling against that subset would
// wipe every other installed module and lock entry. That holds all the more
// when the subset resolved to nothing.
//
// Otherwise the question is whether anything is left over from a dependency
// that is no longer declared — a lock entry, or a directory under modules/.
func shouldReconcileEmpty(options InstallOptions, pkg *domain.Package) bool {
	if !shouldReconcile(options) {
		return false
	}

	if len(pkg.Lock.Installed) > 0 {
		return true
	}

	entries, err := os.ReadDir(env.GetModulesDir())
	if err != nil {
		// No modules directory: nothing was ever installed here.
		return false
	}

	for _, entry := range entries {
		// The directories bossy creates for its own build output are not
		// leftovers of a removed dependency.
		if !utils.Contains(consts.DefaultPaths(), entry.Name()) {
			return true
		}
	}

	return false
}
