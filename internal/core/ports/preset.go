package ports

import "github.com/basti-fantasti/bossy/internal/core/domain"

// PresetStore persists the two dependency catalogs.
//
// They are kept apart on purpose. The remote catalog is replaced wholesale on
// every sync; the local one holds the user's own entries and overrides and is
// never written by a sync. Merging them into a single file would mean a sync
// had to rewrite the user's work to update the shared entries.
//
// Loading a catalog that has never been written returns an empty catalog and
// no error: a machine that has not synced yet is in a normal state.
type PresetStore interface {
	// LoadLocal reads the user's own catalog.
	LoadLocal() (domain.Catalog, error)
	// SaveLocal replaces the user's own catalog.
	SaveLocal(catalog domain.Catalog) error
	// LoadRemote reads the catalog written by the last sync.
	LoadRemote() (domain.Catalog, error)
	// SaveRemote replaces the catalog written by the last sync.
	SaveRemote(catalog domain.Catalog) error
}
