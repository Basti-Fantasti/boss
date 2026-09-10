package packages_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/filesystem"
	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	lockService "github.com/basti-fantasti/bossy/internal/core/services/lock"
	"github.com/basti-fantasti/bossy/internal/core/services/packages"
	"github.com/basti-fantasti/bossy/pkg/consts"
)

const (
	testDepRepo   = "github.com/hashload/horse"
	testDepCommit = "0dd7b276a1b9dfbb1a1fbd1a31b1ee6f9a1a4d21"
)

// newTestService wires a PackageService over the real on-disk repositories,
// rooted at a fresh temp directory. It returns the service and the path of the
// bossy.json inside that directory (the file itself is written by the caller
// when the test needs it to exist).
func newTestService(t *testing.T) (*packages.PackageService, string) {
	t.Helper()

	fs := filesystem.NewOSFileSystem()
	svc := packages.NewPackageService(
		repository.NewFilePackageRepository(fs),
		repository.NewFileLockRepository(fs),
	)
	return svc, filepath.Join(t.TempDir(), consts.FilePackage)
}

// writeManifest writes a minimal bossy.json next to the given package path.
func writeManifest(t *testing.T, packagePath string) {
	t.Helper()
	if err := os.WriteFile(packagePath, []byte(`{"name":"test"}`), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// lockWithDep builds a lock carrying a single dependency pinned to a commit.
func lockWithDep(t *testing.T) (domain.PackageLock, string) {
	t.Helper()

	dep := domain.Dependency{Repository: testDepRepo}
	lock := domain.PackageLock{
		Installed: map[string]domain.LockedDependency{},
	}
	lock.Installed[dep.GetKey()] = domain.LockedDependency{
		Name:    dep.Name(),
		Version: "develop",
		Commit:  testDepCommit,
		Hash:    "deadbeef",
	}
	return lock, dep.GetKey()
}

// TestPackageService_LockRoundTrip is the regression guard for the bug where
// PackageService persisted the lock to the legacy "boss.lock" name while
// everything else in the codebase read and wrote "bossy-lock.json". The save
// went to one file, the load looked at another, and FileLockRepository.Load
// swallowed the miss by returning an empty lock — so the pinned commit
// silently vanished on every round trip.
func TestPackageService_LockRoundTrip(t *testing.T) {
	svc, packagePath := newTestService(t)
	writeManifest(t, packagePath)

	pkg, err := svc.Load(packagePath)
	if err != nil {
		t.Fatalf("load package: %v", err)
	}

	lock, key := lockWithDep(t)
	pkg.Lock = lock

	if saveErr := svc.SaveLock(pkg, packagePath); saveErr != nil {
		t.Fatalf("save lock: %v", saveErr)
	}

	reloaded, err := svc.Load(packagePath)
	if err != nil {
		t.Fatalf("reload package: %v", err)
	}

	locked, ok := reloaded.Lock.Installed[key]
	if !ok {
		t.Fatalf("locked dependency %q did not survive the round trip; lock has %d entries",
			key, len(reloaded.Lock.Installed))
	}
	if locked.Commit != testDepCommit {
		t.Errorf("commit = %q, want %q", locked.Commit, testDepCommit)
	}
	if locked.Version != "develop" {
		t.Errorf("version = %q, want %q", locked.Version, "develop")
	}
}

// TestPackageService_SaveLockWritesCanonicalName asserts the file actually
// landed under the canonical lock name, and that no legacy "boss.lock" was
// produced. Without this, a round trip could pass while still using the wrong
// filename on both ends — which is exactly the state the bug left the code in.
func TestPackageService_SaveLockWritesCanonicalName(t *testing.T) {
	svc, packagePath := newTestService(t)
	writeManifest(t, packagePath)

	pkg, err := svc.Load(packagePath)
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	pkg.Lock, _ = lockWithDep(t)

	if saveErr := svc.SaveLock(pkg, packagePath); saveErr != nil {
		t.Fatalf("save lock: %v", saveErr)
	}

	dir := filepath.Dir(packagePath)
	if _, err := os.Stat(filepath.Join(dir, consts.FilePackageLock)); err != nil {
		t.Errorf("expected %s to exist: %v", consts.FilePackageLock, err)
	}
	if _, err := os.Stat(filepath.Join(dir, consts.FilePackageLockOld)); err == nil {
		t.Errorf("legacy %s must not be written", consts.FilePackageLockOld)
	}
}

// TestPackageService_ReadsLockWrittenByLockService pins the contract between
// the two writers of the lock file: LockService.Save (used by the installer)
// and PackageService (used by every read path). They must agree on the
// filename.
func TestPackageService_ReadsLockWrittenByLockService(t *testing.T) {
	svc, packagePath := newTestService(t)
	writeManifest(t, packagePath)

	fs := filesystem.NewOSFileSystem()
	lockSvc := lockService.NewLockService(repository.NewFileLockRepository(fs), fs)

	lock, key := lockWithDep(t)
	if err := lockSvc.Save(&lock, filepath.Dir(packagePath)); err != nil {
		t.Fatalf("LockService.Save: %v", err)
	}

	pkg, err := svc.Load(packagePath)
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	locked, ok := pkg.Lock.Installed[key]
	if !ok {
		t.Fatalf("PackageService did not read the lock written by LockService (%d entries)",
			len(pkg.Lock.Installed))
	}
	if locked.Commit != testDepCommit {
		t.Errorf("commit = %q, want %q", locked.Commit, testDepCommit)
	}
}

// TestPackageService_MigratesLegacyLockOnLoad covers the legacy upgrade path:
// a repository still carrying a "boss.lock" from an older boss release must be
// renamed to "bossy-lock.json" and its contents must survive.
func TestPackageService_MigratesLegacyLockOnLoad(t *testing.T) {
	svc, packagePath := newTestService(t)
	writeManifest(t, packagePath)
	dir := filepath.Dir(packagePath)

	legacy := filepath.Join(dir, consts.FilePackageLockOld)
	legacyBody := `{"hash":"h","updated":"2020-01-01T00:00:00Z","installedModules":{` +
		`"github.com/hashload/horse":{"name":"horse","version":"develop","commit":"` +
		testDepCommit + `","hash":"deadbeef","artifacts":{}}}}`
	if err := os.WriteFile(legacy, []byte(legacyBody), 0600); err != nil {
		t.Fatalf("write legacy lock: %v", err)
	}

	pkg, err := svc.Load(packagePath)
	if err != nil {
		t.Fatalf("load package: %v", err)
	}

	dep := domain.Dependency{Repository: testDepRepo}
	locked, ok := pkg.Lock.Installed[dep.GetKey()]
	if !ok {
		t.Fatalf("legacy lock contents were lost on migration (%d entries)", len(pkg.Lock.Installed))
	}
	if locked.Commit != testDepCommit {
		t.Errorf("commit = %q, want %q", locked.Commit, testDepCommit)
	}

	if _, err := os.Stat(legacy); err == nil {
		t.Errorf("legacy %s should have been renamed away", consts.FilePackageLockOld)
	}
	if _, err := os.Stat(filepath.Join(dir, consts.FilePackageLock)); err != nil {
		t.Errorf("expected migrated %s to exist: %v", consts.FilePackageLock, err)
	}
}
