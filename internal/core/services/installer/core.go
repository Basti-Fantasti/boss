package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/basti-fantasti/bossy/pkg/pkgmanager"

	"github.com/Masterminds/semver/v3"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/filesystem"
	git "github.com/basti-fantasti/bossy/internal/adapters/secondary/git"
	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/compiler"
	lockService "github.com/basti-fantasti/bossy/internal/core/services/lock"
	"github.com/basti-fantasti/bossy/internal/core/services/paths"
	"github.com/basti-fantasti/bossy/internal/core/services/tracker"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/utils"
	"github.com/basti-fantasti/bossy/utils/librarypath"
)

type installContext struct {
	config           env.ConfigProvider
	rootLocked       *domain.PackageLock
	root             *domain.Package
	processed        []string
	visited          map[string]bool
	useLockedVersion bool
	progress         *ProgressTracker
	lockSvc          *lockService.LockService
	modulesDir       string
	options          InstallOptions
	warnings         []string
	depManager       *DependencyManager
	requestedDeps    map[string]bool // Track which dependencies were explicitly requested
}

//nolint:lll // Function signature readability
func newInstallContext(config env.ConfigProvider, pkg *domain.Package, options InstallOptions, progress *ProgressTracker) *installContext {
	fs := filesystem.NewOSFileSystem()
	lockRepo := repository.NewFileLockRepository(fs)
	lockSvc := lockService.NewLockService(lockRepo, fs)

	requestedDeps := make(map[string]bool)
	if len(options.Args) > 0 {
		for _, arg := range options.Args {
			normalized, ok := NormalizeDepKey(arg)
			if !ok {
				normalized = ParseDependency(arg)
			}
			requestedDeps[normalized] = true
		}
	}

	return &installContext{
		config:           config,
		rootLocked:       &pkg.Lock,
		root:             pkg,
		useLockedVersion: options.LockedVersion,
		processed:        consts.DefaultPaths(),
		visited:          make(map[string]bool),
		progress:         progress,
		lockSvc:          lockSvc,
		modulesDir:       env.GetModulesDir(),
		options:          options,
		warnings:         make([]string, 0),
		depManager:       NewDefaultDependencyManager(config),
		requestedDeps:    requestedDeps,
	}
}

// DoInstall performs the installation of dependencies.
func DoInstall(config env.ConfigProvider, options InstallOptions, pkg *domain.Package) error {
	msg.Info("🔍 Analyzing dependencies...\n")

	deps := collectDependenciesToInstall(pkg, options.Args)

	if len(deps) == 0 {
		msg.Info("📄 No dependencies to install")
		return nil
	}

	var progress *ProgressTracker
	if msg.IsDebugMode() {
		progress = &ProgressTracker{
			Tracker: tracker.NewNull[DependencyStatus](),
		}
	} else {
		progress = NewProgressTracker(deps)
	}
	installContext := newInstallContext(config, pkg, options, progress)

	msg.Info("✨ Installing %d dependencies:\n", len(deps))

	if !msg.IsDebugMode() {
		if err := progress.Start(); err != nil {
			msg.Warn("⚠️ Could not start progress tracker: %s", err)
		} else {
			msg.SetQuietMode(true)
			msg.SetProgressTracker(progress)
		}
	}

	dependencies, err := installContext.ensureDependencies(pkg)
	if err != nil {
		msg.SetQuietMode(false)
		msg.SetProgressTracker(nil)
		progress.Stop()
		return fmt.Errorf("❌ Installation failed: %w", err)
	}

	msg.SetQuietMode(false)
	msg.SetProgressTracker(nil)
	progress.Stop()

	if shouldReconcile(options) {
		paths.EnsureCleanModulesDir(dependencies, pkg.Lock)
		pkg.Lock.CleanRemoved(dependencies)
	}
	if err := pkgmanager.SavePackageCurrent(pkg); err != nil {
		msg.Warn("⚠️ Failed to save package: %v", err)
	}
	if err := installContext.lockSvc.Save(&pkg.Lock, env.GetCurrentDir()); err != nil {
		msg.Warn("⚠️ Failed to save lock file: %v", err)
	}

	librarypath.UpdateLibraryPath(pkg)

	compiler.Build(pkg, options.Compiler, options.Platform)
	if err := pkgmanager.SavePackageCurrent(pkg); err != nil {
		msg.Warn("⚠️ Failed to save package: %v", err)
	}
	if err := installContext.lockSvc.Save(&pkg.Lock, env.GetCurrentDir()); err != nil {
		msg.Warn("⚠️ Failed to save lock file: %v", err)
	}

	if len(installContext.warnings) > 0 {
		msg.Warn("⚠️ Installation Warnings:")
		for _, warning := range installContext.warnings {
			msg.Warn("   - %s", warning)
		}
	}

	msg.Success("✅ Installation completed successfully!")
	return nil
}

// shouldReconcile reports whether DoInstall should reconcile modules/ and the
// lock against the dependency set it just processed. A targeted invocation
// (`bossy install <pkg>` or `bossy update <pkg>`) only processes a subset of
// `bossy.json`, so reconciling against that subset would wipe every other
// installed module and lock entry. Only reconcile on a full-tree install.
func shouldReconcile(options InstallOptions) bool {
	return len(options.Args) == 0
}

func (ic *installContext) addWarning(warning string) {
	ic.warnings = append(ic.warnings, warning)
}

// collectDependenciesToInstall collects dependencies to install based on args filter.
// If args is empty, returns all dependencies. Otherwise, returns only specified ones.
func collectDependenciesToInstall(pkg *domain.Package, args []string) []domain.Dependency {
	if pkg.Dependencies == nil {
		return []domain.Dependency{}
	}

	allDeps := pkg.GetParsedDependencies()

	if len(args) == 0 {
		return allDeps
	}

	var filtered []domain.Dependency
	for _, arg := range args {
		normalized, ok := NormalizeDepKey(arg)
		if !ok {
			normalized = ParseDependency(arg)
		}
		for _, dep := range allDeps {
			if dep.Repository == normalized {
				filtered = append(filtered, dep)
				break
			}
		}
	}

	return filtered
}

// collectAllDependencies makes a dry-run to collect all dependencies without installing.
// Deprecated: Use collectDependenciesToInstall instead.
func collectAllDependencies(pkg *domain.Package) []domain.Dependency {
	return collectDependenciesToInstall(pkg, []string{})
}

func (ic *installContext) ensureDependencies(pkg *domain.Package) ([]domain.Dependency, error) {
	if pkg.Dependencies == nil {
		return []domain.Dependency{}, nil
	}

	allDeps := pkg.GetParsedDependencies()

	var deps []domain.Dependency
	if pkg == ic.root && len(ic.requestedDeps) > 0 {
		for _, dep := range allDeps {
			if ic.requestedDeps[dep.Repository] {
				deps = append(deps, dep)
			}
		}
	} else {
		deps = allDeps
	}

	if err := ic.ensureModules(pkg, deps); err != nil {
		return nil, err
	}

	var otherDeps []domain.Dependency
	if len(ic.requestedDeps) == 0 {
		var err error
		otherDeps, err = ic.processOthers()
		if err != nil {
			return nil, err
		}
	}

	deps = append(deps, otherDeps...)

	return deps, nil
}

//nolint:gocognit // Complex dependency processing logic
func (ic *installContext) processOthers() ([]domain.Dependency, error) {
	infos, err := os.ReadDir(env.GetModulesDir())
	var lenProcessedInitial = len(ic.processed)
	var result []domain.Dependency
	if err != nil {
		msg.Err("  ❌ Error on try load dir of modules: %s", err)
		return result, err
	}

	for _, info := range infos {
		if !info.IsDir() {
			continue
		}

		moduleName := info.Name()

		if utils.Contains(ic.processed, moduleName) {
			continue
		}

		ic.processed = append(ic.processed, moduleName)

		if !ic.progress.IsEnabled() {
			msg.Info("  ⚙️ Processing module %s", moduleName)
		}

		fileName := filepath.Join(env.GetModulesDir(), moduleName, consts.FilePackage)

		_, err := os.Stat(fileName)
		if os.IsNotExist(err) {
			continue
		}

		if packageOther, err := pkgmanager.LoadPackageOther(fileName); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			msg.Err("  ❌ Error on try load package %s: %s", fileName, err)
		} else {
			childDeps := packageOther.GetParsedDependencies()
			for _, childDep := range childDeps {
				ic.progress.AddDependency(childDep.Name())
			}
			deps, err := ic.ensureDependencies(packageOther)
			if err != nil {
				return nil, err
			}
			result = append(result, deps...)
		}
	}
	if lenProcessedInitial > len(ic.processed) {
		deps, err := ic.processOthers()
		if err != nil {
			return nil, err
		}
		result = append(result, deps...)
	}

	return result, nil
}

func (ic *installContext) ensureModules(pkg *domain.Package, deps []domain.Dependency) error {
	for _, dep := range deps {
		if err := ic.ensureSingleModule(pkg, dep); err != nil {
			return err
		}
	}
	return nil
}

func (ic *installContext) ensureSingleModule(pkg *domain.Package, dep domain.Dependency) error {
	depName := dep.Name()

	if ic.visited[depName] {
		return nil
	}
	ic.visited[depName] = true
	ic.progress.AddDependency(depName)

	if ic.shouldSkipDependency(dep) {
		ic.reportSkipped(depName, consts.StatusMsgAlreadyInstalled)
		return nil
	}

	if err := ic.cloneDependency(dep, depName); err != nil {
		return err
	}

	repository := git.GetRepository(dep)
	ref := ic.resolveReference(pkg, dep, repository)

	if skip, err := ic.checkIfUpToDate(dep, depName, repository, ref); err != nil {
		return err
	} else if skip {
		return nil
	}

	return ic.installDependency(dep, depName, repository, ref)
}

func (ic *installContext) cloneDependency(dep domain.Dependency, depName string) error {
	if !ic.progress.IsEnabled() {
		msg.Info("🧬 Cloning %s", depName)
	} else {
		ic.reportStatus(depName, "cloning", "🧬 Cloning")
	}

	err := GetDependencyWithProgress(dep, ic.progress)
	if err != nil {
		ic.progress.SetFailed(depName, err)
		return err
	}
	return nil
}

func (ic *installContext) checkIfUpToDate(
	dep domain.Dependency,
	depName string,
	repository *goGit.Repository,
	ref *plumbing.Reference,
) (bool, error) {
	ic.reportStatus(depName, "checking", "🔍 Checking version for")

	wt, err := repository.Worktree()
	if err != nil {
		ic.progress.SetFailed(depName, err)
		return false, err
	}

	status, err := wt.Status()
	if err != nil {
		ic.progress.SetFailed(depName, err)
		return false, err
	}

	head, err := repository.Head()
	if err != nil {
		ic.progress.SetFailed(depName, err)
		return false, err
	}

	currentRef := head.Name()
	needsUpdate := ic.lockSvc.NeedUpdate(ic.rootLocked, dep, ref.Name().Short(), ic.modulesDir)

	// A pinned-commit reference is named after its version label, never after
	// the ref currentRef reports for a detached HEAD, so this fast path only
	// fires for tag and branch checkouts. Re-checking out a pinned commit is
	// idempotent, so missing the skip costs work, not correctness.
	if !needsUpdate && status.IsClean() && ref.Name() == currentRef {
		ic.reportSkipped(depName, consts.StatusMsgUpToDate)
		return true, nil
	}

	return false, nil
}

func (ic *installContext) installDependency(
	dep domain.Dependency,
	depName string,
	repository *goGit.Repository,
	ref *plumbing.Reference,
) error {
	ic.reportStatus(depName, "installing", "🔥 Installing")

	if err := ic.checkoutAndUpdate(dep, repository, ref); err != nil {
		ic.progress.SetFailed(depName, err)
		return err
	}

	warning, err := ic.verifyDependencyCompatibility(dep)
	if err != nil {
		ic.progress.SetFailed(depName, err)
		return err
	}

	ic.reportInstallResult(depName, warning)
	return nil
}

func (ic *installContext) reportStatus(depName, progressStatus, infoPrefix string) {
	if ic.progress.IsEnabled() {
		switch progressStatus {
		case "cloning":
			ic.progress.SetCloning(depName)
		case "checking":
			ic.progress.SetChecking(depName, consts.StatusMsgResolvingVer)
		case "installing":
			ic.progress.SetInstalling(depName)
		}
	} else {
		msg.Info("  %s %s...", infoPrefix, depName)
	}
}

func (ic *installContext) reportSkipped(depName, reason string) {
	if ic.progress.IsEnabled() {
		ic.progress.SetSkipped(depName, reason)
	} else {
		msg.Info("  ✅️ %s already installed", depName)
	}
}

func (ic *installContext) reportInstallResult(depName, warning string) {
	//nolint:nestif // Complex warning handling
	if warning != "" {
		if ic.progress.IsEnabled() {
			ic.progress.SetWarning(depName, warning)
		} else {
			msg.Warn("  ⚠️ %s: %s", depName, warning)
		}
		ic.addWarning(fmt.Sprintf("%s: %s", depName, warning))
	} else {
		if ic.progress.IsEnabled() {
			ic.progress.SetCompleted(depName)
		} else {
			msg.Info("  ✅️ %s installed successfully", depName)
		}
	}
}

// shouldSkipDependency reports whether a dependency can be left alone. The lock
// records the commit each dependency was installed at, and that commit is the
// only claim about the module the lock can actually verify: a version string
// says what was asked for, never what is on disk. So the skip decision is
// "modules/<name> is at the locked commit", not "the locked version looks new
// enough". A worktree someone moved by hand, or one left half-written by an
// interrupted run, is reinstalled instead of silently built.
func (ic *installContext) shouldSkipDependency(dep domain.Dependency) bool {
	if utils.Contains(ic.options.ForceUpdate, dep.Name()) {
		return false
	}

	if !ic.useLockedVersion {
		return false
	}

	installed, exists := ic.rootLocked.Installed[dep.GetKey()]
	if !exists {
		return false
	}

	// The module directory is the one thing both branches below depend on and
	// neither can infer: the object cache under $BOSS_HOME/cache survives a
	// deleted modules/<name>, so a repository still opens and still reports the
	// locked commit. Checking presence once, ahead of the branch, is what stops
	// the commit path and the version path drifting apart again.
	if !ic.moduleIsPresent(dep) {
		return false
	}

	if installed.Commit != "" {
		return ic.worktreeIsAt(dep, installed.Commit)
	}

	return ic.lockedVersionSatisfies(dep, installed)
}

// moduleIsPresent reports whether modules/<name> exists. An absent module is
// the ordinary first-install case on a machine that has the lock but not the
// checkout, not a fault worth reporting.
func (ic *installContext) moduleIsPresent(dep domain.Dependency) bool {
	moduleDir := filepath.Join(ic.modulesDir, dep.Name())
	if _, err := os.Stat(moduleDir); err != nil {
		msg.Debug("  📁 %s is not present at %s, installing", dep.Name(), moduleDir)
		return false
	}
	return true
}

// worktreeIsAt reports whether the dependency's checked-out module sits on
// commit. It is only reached once moduleIsPresent has confirmed the module
// directory exists. Anything that stops us confirming the commit — no git
// metadata, an unreadable HEAD — answers "no": reinstalling is always safe and
// is not a fault worth reporting.
func (ic *installContext) worktreeIsAt(dep domain.Dependency, commit string) bool {
	repository, err := git.TryGetRepository(dep)
	if err != nil {
		msg.Debug("  📁 %s is not a readable repository (%s), installing", dep.Name(), err)
		return false
	}

	head, err := repository.Head()
	if err != nil {
		msg.Debug("  📁 HEAD of %s could not be read (%s), installing", dep.Name(), err)
		return false
	}

	if head.Hash().String() != commit {
		msg.Debug("  🔀 %s is at %s but the lock records %s, installing",
			dep.Name(), shortenSHA(head.Hash().String()), shortenSHA(commit))
		return false
	}

	return true
}

// lockedVersionSatisfies is the fallback for lock entries written before the
// commit was recorded. It compares version strings, which is all such an entry
// offers.
//
// A version that is not a semantic version is a branch name or a raw commit
// SHA. Both are supported ways to pin a dependency, so neither is an error:
// there is simply nothing here to compare, and the honest answer is to install
// the dependency and let the lock be rewritten with a commit. Reporting a
// supported pin as a failure on every install is what this replaced.
func (ic *installContext) lockedVersionSatisfies(dep domain.Dependency, installed domain.LockedDependency) bool {
	depv := strings.NewReplacer("^", "", "~", "").Replace(dep.GetVersion())
	requiredVersion, err := semver.NewVersion(depv)
	if err != nil {
		msg.Debug("  📌 %s is pinned to %q, which is not a version range, installing", dep.Name(), dep.GetVersion())
		return false
	}

	installedVersion, err := semver.NewVersion(installed.Version)
	if err != nil {
		msg.Debug("  📌 %s is locked at %q, which is not a version, installing", dep.Name(), installed.Version)
		return false
	}

	return !installedVersion.LessThan(requiredVersion)
}

// resolveReference resolves a dependency's declared version into the reference
// that should be checked out. The whole reference is returned, not just its
// name: for a pinned commit the name carries the version label written into the
// lock while the hash carries the commit to check out, and the two are not the
// same string.
func (ic *installContext) resolveReference(
	pkg *domain.Package,
	dep domain.Dependency,
	repository *goGit.Repository) *plumbing.Reference {
	bestMatch := ic.getVersion(dep, repository)

	if bestMatch == nil {
		warnMsg := fmt.Sprintf("No matching version found for '%s' with constraint '%s'", dep.Repository, dep.GetVersion())
		if !ic.progress.IsEnabled() {
			msg.Warn("  ⚠️ " + warnMsg)
		}
		ic.addWarning(fmt.Sprintf("%s: %s", dep.Name(), warnMsg))

		if mainBranchReference, err := git.GetMain(repository); err == nil {
			warnMsg := fmt.Sprintf("Falling back to main branch: %s", mainBranchReference.Name)
			if !ic.progress.IsEnabled() {
				msg.Warn("  ⚠️ %s: %s", dep.Name(), warnMsg)
			}
			ic.addWarning(fmt.Sprintf("%s: %s", dep.Name(), warnMsg))
			// A branch name: checkoutAndUpdate dispatches on the name's shape,
			// so this takes the branch-checkout path and never reads the hash.
			// ZeroHash is therefore the honest value here — we have not
			// resolved a commit, and must not pretend we have.
			return plumbing.NewHashReference(
				plumbing.NewBranchReferenceName(mainBranchReference.Name), plumbing.ZeroHash)
		}
		msg.Die("❌ Could not find any suitable version or branch for dependency '%s'", dep.Repository)
	}

	if dep.GetVersion() == consts.MinimalDependencyVersion {
		pkg.Dependencies[dep.Repository] = "^" + bestMatch.Name().Short()
	}

	return bestMatch
}

func (ic *installContext) checkoutAndUpdate(
	dep domain.Dependency,
	repository *goGit.Repository,
	ref *plumbing.Reference,
) error {
	referenceName := ref.Name()
	isHashRef := isHashReference(referenceName)
	var err error
	if isHashRef {
		// Take the commit from the reference, never from its name: the name is
		// the version label destined for the lock (a branch name, a tag, or the
		// SHA itself) and only the hash is guaranteed to be the commit.
		if !ic.progress.IsEnabled() {
			msg.Debug("  📌 %s pinned to %s", dep.Name(), shortenSHA(ref.Hash().String()))
		}
		err = ic.checkoutHashWithDeepen(dep, ref.Hash(), ref.Hash().String())
	} else {
		if !ic.progress.IsEnabled() {
			msg.Debug("  🔍 Checking out %s to %s", dep.Name(), referenceName.Short())
		}
		err = git.Checkout(ic.config, dep, referenceName)
	}

	commit := ""
	if head, headErr := repository.Head(); headErr == nil {
		commit = head.Hash().String()
	}
	ic.lockSvc.AddDependency(ic.rootLocked, dep, referenceName.Short(), commit, ic.modulesDir)

	if err != nil {
		return err
	}

	// Skip pull on detached-HEAD checkouts — they're pinned.
	if isHashRef {
		return nil
	}

	if !ic.progress.IsEnabled() {
		msg.Debug("  📥 Pulling latest changes for %s", dep.Name())
	}
	err = git.Pull(ic.config, dep)
	if err != nil && !errors.Is(err, goGit.NoErrAlreadyUpToDate) {
		warnMsg := fmt.Sprintf("Error on pull from dependency %s\n%s", dep.Repository, err)
		if !ic.progress.IsEnabled() {
			msg.Warn("  " + warnMsg)
		}
		ic.addWarning(fmt.Sprintf("%s: %s", dep.Name(), warnMsg))
	}
	return nil
}

func (ic *installContext) getVersion(
	dep domain.Dependency,
	repository *goGit.Repository,
) *plumbing.Reference {
	// Raw SHA in bossy.json — terminal, skip resolution. The name doubles as
	// the version label recorded in the lock, and for a raw-SHA pin the SHA
	// itself is the right label.
	if domain.IsGitSHA(dep.GetVersion()) {
		return plumbing.NewHashReference(
			plumbing.ReferenceName(dep.GetVersion()), plumbing.NewHash(dep.GetVersion()))
	}

	if ic.useLockedVersion {
		lockedDependency := ic.rootLocked.GetInstalled(dep)
		if lockedDependency.Commit != "" {
			// Name keeps the declared version (e.g. "develop") so replaying the
			// lock does not rewrite the label; the hash carries the pin.
			return plumbing.NewHashReference(
				plumbing.ReferenceName(lockedDependency.Version), plumbing.NewHash(lockedDependency.Commit))
		}
		if tag := git.GetByTag(repository, lockedDependency.Version); tag != nil &&
			lockedDependency.Version != dep.GetVersion() {
			return tag
		}
	}

	versions, errVersions := git.GetVersions(ic.config, repository, dep)
	if errVersions != nil {
		// Never fall through to the main-branch fallback here: silently
		// building the wrong branch is worse than not building.
		msg.Die("❌ Could not list versions for '%s': %s", dep.Repository, errVersions)
	}
	constraints, err := domain.ParseConstraint(dep.GetVersion())
	if err != nil {
		warnMsg := fmt.Sprintf("Version constraint '%s' not supported: %s", dep.GetVersion(), err)
		if !ic.progress.IsEnabled() {
			msg.Warn("  ⚠️ " + warnMsg)
		}
		ic.addWarning(fmt.Sprintf("%s: %s", dep.Name(), warnMsg))

		for _, version := range versions {
			if version.Name().Short() == dep.GetVersion() {
				return version
			}
		}
		//nolint:lll // Error message readability
		warnMsg2 := fmt.Sprintf("No exact match found for version '%s'. Available versions: %d", dep.GetVersion(), len(versions))
		if !ic.progress.IsEnabled() {
			msg.Warn("  ⚠️ " + warnMsg2)
		}
		ic.addWarning(fmt.Sprintf("%s: %s", dep.Name(), warnMsg2))
		return nil
	}

	return ic.getVersionSemantic(
		versions,
		constraints)
}

func (ic *installContext) getVersionSemantic(
	versions []*plumbing.Reference,
	contraint *semver.Constraints) *plumbing.Reference {
	var bestVersion *semver.Version
	var bestReference *plumbing.Reference

	for _, versionRef := range versions {
		short := versionRef.Name().Short()
		withoutPrefix := domain.StripVersionPrefix(short)
		newVersion, err := semver.NewVersion(withoutPrefix)
		if err != nil {
			continue
		}
		//nolint:nestif // Version constraint checking
		if contraint.Check(newVersion) {
			if bestVersion != nil && newVersion.GreaterThan(bestVersion) {
				bestVersion = newVersion
				bestReference = versionRef
			}

			if bestVersion == nil {
				bestVersion = newVersion
				bestReference = versionRef
			} else if bestVersion.Equal(newVersion) {
				if strings.HasPrefix(short, "v") && !strings.HasPrefix(bestReference.Name().Short(), "v") {
					bestReference = versionRef
				}
			}
		}
	}
	return bestReference
}

func (ic *installContext) verifyDependencyCompatibility(dep domain.Dependency) (string, error) {
	depPath := filepath.Join(ic.modulesDir, dep.Name())
	bossFile := filepath.Join(depPath, consts.FilePackage)
	if _, statErr := os.Stat(bossFile); os.IsNotExist(statErr) {
		return "", nil
	}
	depPkg, err := pkgmanager.LoadPackageOther(bossFile)
	if err != nil {
		return "", err
	}

	if depPkg.Engines == nil || len(depPkg.Engines.Platforms) == 0 {
		return "", nil
	}

	targetPlatform := ic.options.Platform
	if targetPlatform == "" && ic.root.Toolchain != nil {
		targetPlatform = ic.root.Toolchain.Platform
	}

	if targetPlatform == "" {
		return "", nil
	}

	for _, p := range depPkg.Engines.Platforms {
		if strings.EqualFold(p, targetPlatform) {
			return "", nil
		}
	}

	//nolint:lll // Error message readability
	errorMessage := fmt.Sprintf("Dependency '%s' does not support platform '%s'. Supported: %v", dep.Name(), targetPlatform, depPkg.Engines.Platforms)

	isStrict := ic.options.Strict
	if !isStrict && ic.root.Toolchain != nil {
		isStrict = ic.root.Toolchain.Strict
	}

	if isStrict {
		return "", errors.New(errorMessage)
	}
	return errorMessage, nil
}

// checkoutHashWithDeepen performs a pinned-SHA checkout, retrying once after a
// deepening fetch when the object is missing locally (typical for shallow
// clones). The retry is single-shot: a second object-not-found result
// propagates as-is.
func (ic *installContext) checkoutHashWithDeepen(dep domain.Dependency, hash plumbing.Hash, short string) error {
	err := git.CheckoutHash(ic.config, dep, hash)
	if err == nil || !isObjectNotFound(err) {
		return err
	}
	if fetchErr := git.UnshallowFetch(ic.config, dep); fetchErr != nil {
		return fmt.Errorf("checkout %s: object missing and deepening fetch failed: %w", short, fetchErr)
	}
	return git.CheckoutHash(ic.config, dep, hash)
}

// isObjectNotFound reports whether err indicates that a referenced git object
// is missing from the local repository. This is the signal we use to trigger a
// single deepening fetch on a shallow clone before giving up on a pinned SHA.
// Matches both the embedded go-git sentinel and the wording emitted by the
// native git binary (case-insensitive).
func isObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown revision") || strings.Contains(msg, "object not found")
}

// isHashReference reports whether a resolved reference must be checked out by
// commit hash rather than by name. Tags, branches and remote-tracking refs are
// checked out by name; anything else is a pinned commit whose name is only a
// version label for the lock file.
func isHashReference(name plumbing.ReferenceName) bool {
	return !name.IsTag() && !name.IsBranch() && !name.IsRemote()
}

// shortenSHA abbreviates a full commit hash for debug output, leaving anything
// shorter (a branch or tag label) untouched.
func shortenSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
