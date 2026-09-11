// Package presets manages the catalog of predefined dependencies a project can
// pick from.
//
// Two catalogs are kept: a shared one synced from a git repository, and a local
// one holding the user's own entries. The local catalog always wins on an id
// collision, which is how a user overrides a shared entry without editing the
// shared catalog.
package presets

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/ports"
)

// Errors callers distinguish.
var (
	// ErrExists is returned when adding a preset whose id is already used locally.
	ErrExists = errors.New("preset already exists")
	// ErrNotFound is returned when a local preset was expected and is absent.
	ErrNotFound = errors.New("preset not found")
)

// Service reads and edits the preset catalogs.
type Service struct {
	store ports.PresetStore
}

// New creates a Service over the given store.
func New(store ports.PresetStore) *Service {
	return &Service{store: store}
}

// All returns every preset, local entries shadowing remote ones, sorted by id.
func (s *Service) All() ([]domain.Preset, error) {
	remote, local, err := s.both()
	if err != nil {
		return nil, err
	}
	return domain.MergeCatalogs(remote, local), nil
}

// ByID returns the merged preset with the given id, matched case-insensitively.
func (s *Service) ByID(id string) (domain.Preset, bool, error) {
	all, err := s.All()
	if err != nil {
		return domain.Preset{}, false, err
	}
	for _, p := range all {
		if strings.EqualFold(p.ID, id) {
			return p, true, nil
		}
	}
	return domain.Preset{}, false, nil
}

// ByTag returns every merged preset carrying the tag, sorted by id.
func (s *Service) ByTag(tag string) ([]domain.Preset, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	out := make([]domain.Preset, 0, len(all))
	for _, p := range all {
		if p.HasTag(tag) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Tags returns every tag used across the merged catalog, sorted and deduplicated.
func (s *Service) Tags() ([]string, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, p := range all {
		for _, t := range p.Tags {
			seen[strings.ToLower(t)] = t
		}
	}
	out := make([]string, 0, len(seen))
	for _, t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

// Add stores a new preset in the local catalog.
func (s *Service) Add(preset domain.Preset) error {
	if err := preset.Validate(); err != nil {
		return err
	}
	local, err := s.store.LoadLocal()
	if err != nil {
		return fmt.Errorf("load local catalog: %w", err)
	}
	if _, exists := local.Find(preset.ID); exists {
		return fmt.Errorf("%w: %s", ErrExists, preset.ID)
	}
	local.Presets = append(local.Presets, preset)
	return s.saveLocal(local)
}

// Update writes a preset into the local catalog, replacing any local entry with
// the same id.
//
// An id present only in the remote catalog is accepted and creates a local
// override, which is the documented way to shadow a shared entry.
func (s *Service) Update(preset domain.Preset) error {
	if err := preset.Validate(); err != nil {
		return err
	}
	local, err := s.store.LoadLocal()
	if err != nil {
		return fmt.Errorf("load local catalog: %w", err)
	}
	replaced := false
	for i := range local.Presets {
		if strings.EqualFold(local.Presets[i].ID, preset.ID) {
			local.Presets[i] = preset
			replaced = true
			break
		}
	}
	if !replaced {
		local.Presets = append(local.Presets, preset)
	}
	return s.saveLocal(local)
}

// Remove deletes a preset from the local catalog.
//
// Entries in the remote catalog cannot be removed here: the client never writes
// the shared catalog, and a sync would restore the entry anyway.
func (s *Service) Remove(id string) error {
	local, err := s.store.LoadLocal()
	if err != nil {
		return fmt.Errorf("load local catalog: %w", err)
	}
	kept := make([]domain.Preset, 0, len(local.Presets))
	found := false
	for _, p := range local.Presets {
		if strings.EqualFold(p.ID, id) {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return fmt.Errorf("%w in the local catalog: %s", ErrNotFound, id)
	}
	local.Presets = kept
	return s.saveLocal(local)
}

// ImportLocal merges a catalog into the local one. It returns, in order, the
// number of entries that were new and the number that replaced an existing
// local entry.
//
// The incoming catalog is validated in full before anything is written, so a
// single bad entry cannot leave the local catalog half-updated.
func (s *Service) ImportLocal(incoming domain.Catalog) (int, int, error) {
	if err := incoming.Validate(); err != nil {
		return 0, 0, err
	}
	local, err := s.store.LoadLocal()
	if err != nil {
		return 0, 0, fmt.Errorf("load local catalog: %w", err)
	}

	var added, replaced int
	for _, p := range incoming.Presets {
		if _, exists := local.Find(p.ID); exists {
			replaced++
		} else {
			added++
		}
		local = upsert(local, p)
	}

	if err := s.saveLocal(local); err != nil {
		return 0, 0, err
	}
	return added, replaced, nil
}

// ReplaceRemote overwrites the synced catalog.
//
// Validation happens before the write so that a catalog repository with one bad
// entry cannot destroy the working remote.json already on disk.
func (s *Service) ReplaceRemote(catalog domain.Catalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	if err := s.store.SaveRemote(catalog); err != nil {
		return fmt.Errorf("save remote catalog: %w", err)
	}
	return nil
}

// Remote returns the synced catalog as-is, for reporting sync state.
func (s *Service) Remote() (domain.Catalog, error) {
	catalog, err := s.store.LoadRemote()
	if err != nil {
		return domain.Catalog{}, fmt.Errorf("load remote catalog: %w", err)
	}
	return catalog, nil
}

// Local returns the user's catalog as-is.
func (s *Service) Local() (domain.Catalog, error) {
	catalog, err := s.store.LoadLocal()
	if err != nil {
		return domain.Catalog{}, fmt.Errorf("load local catalog: %w", err)
	}
	return catalog, nil
}

// both loads the remote and local catalogs, in that order.
func (s *Service) both() (domain.Catalog, domain.Catalog, error) {
	remote, err := s.store.LoadRemote()
	if err != nil {
		return domain.Catalog{}, domain.Catalog{}, fmt.Errorf("load remote catalog: %w", err)
	}
	local, err := s.store.LoadLocal()
	if err != nil {
		return domain.Catalog{}, domain.Catalog{}, fmt.Errorf("load local catalog: %w", err)
	}
	return remote, local, nil
}

// saveLocal stamps the schema and persists the local catalog.
func (s *Service) saveLocal(catalog domain.Catalog) error {
	catalog.Schema = domain.CatalogSchema
	if err := s.store.SaveLocal(catalog); err != nil {
		return fmt.Errorf("save local catalog: %w", err)
	}
	return nil
}

// upsert replaces a preset with the same id, or appends it.
func upsert(catalog domain.Catalog, preset domain.Preset) domain.Catalog {
	for i := range catalog.Presets {
		if strings.EqualFold(catalog.Presets[i].ID, preset.ID) {
			catalog.Presets[i] = preset
			return catalog
		}
	}
	catalog.Presets = append(catalog.Presets, preset)
	return catalog
}
