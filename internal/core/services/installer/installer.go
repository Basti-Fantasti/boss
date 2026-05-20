// Package installer provides dependency installation and uninstallation functionality.
// It manages both global and local dependency installations, handling version locking and updates.
package installer

import (
	"os"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/filesystem"
	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	lockService "github.com/basti-fantasti/bossy/internal/core/services/lock"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/pkg/pkgmanager"
)

// InstallOptions holds the options for the installation process.
type InstallOptions struct {
	Args          []string
	LockedVersion bool
	NoSave        bool
	Compiler      string
	Platform      string
	Strict        bool
	ForceUpdate   []string
}

// createLockService creates a new lock service instance.
func createLockService() *lockService.LockService {
	fs := filesystem.NewOSFileSystem()
	lockRepo := repository.NewFileLockRepository(fs)
	return lockService.NewLockService(lockRepo, fs)
}

// InstallModules installs the modules based on the provided options.
func InstallModules(options InstallOptions) {
	pkg, err := pkgmanager.LoadPackage()
	if err != nil {
		if os.IsNotExist(err) {
			msg.Die("❌ 'bossy.json' not exists in " + env.GetCurrentDir())
		} else {
			msg.Die("❌ Fail on open dependencies file: %s", err)
		}
	}

	if env.GetGlobal() {
		GlobalInstall(env.GlobalConfiguration(), options.Args, pkg, options.LockedVersion, options.NoSave)
	} else {
		LocalInstall(env.GlobalConfiguration(), options, pkg)
	}
}

// UninstallModules uninstalls the specified modules.
func UninstallModules(args []string, noSave bool) {
	pkg, err := pkgmanager.LoadPackage()
	if err != nil && !os.IsNotExist(err) {
		msg.Die("❌ Fail on open dependencies file: %s", err)
	}

	if pkg == nil {
		return
	}

	for _, arg := range args {
		dependencyRepository := ParseDependency(arg)
		pkg.UninstallDependency(dependencyRepository)
	}

	if err := pkgmanager.SavePackageCurrent(pkg); err != nil {
		msg.Warn("⚠️ Failed to save package: %v", err)
	}
	lockSvc := createLockService()
	_ = lockSvc.Save(&pkg.Lock, env.GetCurrentDir())

	InstallModules(InstallOptions{
		Args:          []string{},
		LockedVersion: false,
		NoSave:        noSave,
	})
}
