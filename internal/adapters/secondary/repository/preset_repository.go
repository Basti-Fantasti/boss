// Package repository provides implementations for domain repositories.
package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/ports"
)

// Compile-time check that FilePresetStore implements ports.PresetStore.
var _ ports.PresetStore = (*FilePresetStore)(nil)

// Catalog file names inside the presets directory.
const (
	localCatalogFile  = "local.json"
	remoteCatalogFile = "remote.json"
)

// FilePresetStore keeps the preset catalogs as two JSON files in a directory,
// in production `<bossy home>/presets`.
type FilePresetStore struct {
	dir string
}

// NewFilePresetStore creates a store rooted at the given directory. The
// directory is created lazily on the first save.
func NewFilePresetStore(dir string) *FilePresetStore {
	return &FilePresetStore{dir: dir}
}

// LoadLocal reads the user's own catalog.
func (s *FilePresetStore) LoadLocal() (domain.Catalog, error) {
	return s.load(localCatalogFile)
}

// SaveLocal replaces the user's own catalog.
func (s *FilePresetStore) SaveLocal(catalog domain.Catalog) error {
	return s.save(localCatalogFile, catalog)
}

// LoadRemote reads the catalog written by the last sync.
func (s *FilePresetStore) LoadRemote() (domain.Catalog, error) {
	return s.load(remoteCatalogFile)
}

// SaveRemote replaces the catalog written by the last sync.
func (s *FilePresetStore) SaveRemote(catalog domain.Catalog) error {
	return s.save(remoteCatalogFile, catalog)
}

// load reads one catalog file.
//
// A missing file yields an empty catalog. A malformed one is an error naming
// the path: starting from empty would silently discard a catalog that
// somebody built, and the next save would overwrite it for good.
func (s *FilePresetStore) load(name string) (domain.Catalog, error) {
	path := filepath.Join(s.dir, name)

	buffer, err := os.ReadFile(path) // #nosec G304 -- reading a catalog from the bossy home
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.NewCatalog(), nil
		}
		return domain.Catalog{}, fmt.Errorf("read %s: %w", path, err)
	}

	catalog := domain.Catalog{}
	if err := json.Unmarshal(buffer, &catalog); err != nil {
		return domain.Catalog{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if catalog.Presets == nil {
		catalog.Presets = []domain.Preset{}
	}

	return catalog, nil
}

// save writes one catalog file, creating the directory if needed.
func (s *FilePresetStore) save(name string, catalog domain.Catalog) error {
	if catalog.Schema == 0 {
		catalog.Schema = domain.CatalogSchema
	}
	if catalog.Presets == nil {
		catalog.Presets = []domain.Preset{}
	}

	encoded, err := json.MarshalIndent(catalog, "", "\t")
	if err != nil {
		return fmt.Errorf("encode catalog: %w", err)
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil { // #nosec G301 -- matches the rest of the bossy home
		return fmt.Errorf("create %s: %w", s.dir, err)
	}

	path := filepath.Join(s.dir, name)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
