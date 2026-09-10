// Package packages provides services for package operations.
package packages

import (
	"fmt"
	"path/filepath"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/ports"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// PackageService handles package operations using repositories.
type PackageService struct {
	packageRepo ports.PackageRepository
	lockRepo    ports.LockRepository
}

// NewPackageService creates a new package service.
func NewPackageService(packageRepo ports.PackageRepository, lockRepo ports.LockRepository) *PackageService {
	return &PackageService{
		packageRepo: packageRepo,
		lockRepo:    lockRepo,
	}
}

// LoadCurrent loads the current project's package file (bossy.json, or the
// legacy boss.json if bossy.json is absent).
func (s *PackageService) LoadCurrent() (*domain.Package, error) {
	bossFile := s.resolvePackagePath(env.GetBossFile())

	if !s.packageRepo.Exists(bossFile) {
		// Return empty package if file doesn't exist
		pkg := domain.NewPackage()
		pkg.Lock = s.loadOrCreateLock(bossFile)
		return pkg, nil
	}

	pkg, err := s.packageRepo.Load(bossFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load package from %s: %w", bossFile, err)
	}

	pkg.Lock = s.loadOrCreateLock(bossFile)
	return pkg, nil
}

// Load loads a package from a specific path. If the path points at the
// canonical bossy.json but only the legacy boss.json exists in the same
// directory, the legacy file is loaded transparently.
func (s *PackageService) Load(packagePath string) (*domain.Package, error) {
	resolved := s.resolvePackagePath(packagePath)
	pkg, err := s.packageRepo.Load(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to load package from %s: %w", resolved, err)
	}

	pkg.Lock = s.loadOrCreateLock(resolved)
	return pkg, nil
}

// resolvePackagePath returns packagePath when it exists, otherwise falls back
// to a legacy boss.json sibling. Returns the original path when neither
// exists, so callers can produce a meaningful not-found error.
func (s *PackageService) resolvePackagePath(packagePath string) string {
	if filepath.Base(packagePath) != consts.FilePackage {
		return packagePath
	}
	if s.packageRepo.Exists(packagePath) {
		return packagePath
	}
	legacy := filepath.Join(filepath.Dir(packagePath), consts.FilePackageLegacy)
	if s.packageRepo.Exists(legacy) {
		return legacy
	}
	return packagePath
}

// Save saves a package to a specific path.
func (s *PackageService) Save(pkg *domain.Package, packagePath string) error {
	if err := s.packageRepo.Save(pkg, packagePath); err != nil {
		return fmt.Errorf("failed to save package to %s: %w", packagePath, err)
	}
	return nil
}

// SaveCurrent saves the current project's package file.
func (s *PackageService) SaveCurrent(pkg *domain.Package) error {
	return s.Save(pkg, env.GetBossFile())
}

// SaveLock saves the lock file for a package.
func (s *PackageService) SaveLock(pkg *domain.Package, packagePath string) error {
	lockPath := s.getLockPath(packagePath)
	if err := s.lockRepo.Save(&pkg.Lock, lockPath); err != nil {
		return fmt.Errorf("failed to save lock file to %s: %w", lockPath, err)
	}
	return nil
}

// loadOrCreateLock loads the lock file or creates a new empty one.
func (s *PackageService) loadOrCreateLock(packagePath string) domain.PackageLock {
	lockPath := s.getLockPath(packagePath)
	lock, err := s.lockRepo.Load(lockPath)
	if err != nil || lock == nil {
		return domain.PackageLock{
			Updated:   "",
			Hash:      "",
			Installed: make(map[string]domain.LockedDependency),
		}
	}
	return *lock
}

// getLockPath returns the lock file path for a given package path. The lock
// always lives next to the manifest under the canonical name, which is what
// LockService.Save writes and what FileLockRepository migrates legacy locks to.
func (s *PackageService) getLockPath(packagePath string) string {
	dir := filepath.Dir(packagePath)
	return filepath.Join(dir, consts.FilePackageLock)
}
