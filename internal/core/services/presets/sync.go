package presets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

// CatalogFileName is the file a catalog repository must carry at its root.
const CatalogFileName = "presets.json"

// Cloner checks a repository out into a directory that already exists.
//
// Injected rather than imported so the service stays free of adapter
// dependencies, and so the sync logic is testable against a local fixture
// without reaching the network.
type Cloner func(repoKey, destDir string) error

// Sync replaces the shared catalog with the one in the given repository.
//
// The clone lands in a temporary directory that is removed afterwards: the
// catalog is one small JSON document read once, not a dependency, so it has no
// business in the dependency cache.
//
// The downloaded catalog is validated in full before anything is written. A
// catalog repository carrying one bad entry must not destroy the working
// remote.json already on disk — after a bad sync the user would otherwise be
// left with no catalog at all and no way to install.
func (s *Service) Sync(repoKey string, clone Cloner) (domain.Catalog, error) {
	if repoKey == "" {
		return domain.Catalog{}, errors.New("no catalog repository configured; pass --source once to set it")
	}

	tempDir, err := os.MkdirTemp("", "bossy-presets-")
	if err != nil {
		return domain.Catalog{}, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// git clone refuses a destination that already exists and is not empty,
	// so the checkout goes into a fresh child of the temporary directory.
	checkout := filepath.Join(tempDir, "catalog")
	if cloneErr := clone(repoKey, checkout); cloneErr != nil {
		return domain.Catalog{}, cloneErr
	}

	path := filepath.Join(checkout, CatalogFileName)
	raw, err := os.ReadFile(path) // #nosec G304 -- path is inside a directory this function created
	if err != nil {
		return domain.Catalog{}, fmt.Errorf("%s carries no %s at its root: %w", repoKey, CatalogFileName, err)
	}

	catalog := domain.Catalog{}
	if decodeErr := json.Unmarshal(raw, &catalog); decodeErr != nil {
		return domain.Catalog{}, fmt.Errorf("parse %s from %s: %w", CatalogFileName, repoKey, decodeErr)
	}

	catalog.Source = repoKey
	catalog.SyncedAt = time.Now().UTC()

	if saveErr := s.ReplaceRemote(catalog); saveErr != nil {
		return domain.Catalog{}, saveErr
	}

	return catalog, nil
}
