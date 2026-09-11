package repository_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	"github.com/basti-fantasti/bossy/internal/core/domain"
)

func samplePreset(id string) domain.Preset {
	return domain.Preset{
		ID:          id,
		Name:        strings.ToUpper(id),
		Repo:        "github.com/example/" + id,
		DefaultRef:  domain.PresetRef{Kind: domain.RefKindBranch, Value: "main"},
		SearchPaths: []string{"src"},
		Platforms:   []string{"Win32", "Win64"},
		Tags:        []string{"gtr-standard"},
	}
}

// A machine that has never synced has no catalog on disk. That is the normal
// first-run state, not a failure, so the store must report an empty catalog.
func TestPresetStore_LoadMissingReturnsEmptyCatalog(t *testing.T) {
	t.Parallel()

	store := repository.NewFilePresetStore(filepath.Join(t.TempDir(), "presets"))

	for _, tc := range []struct {
		name string
		load func() (domain.Catalog, error)
	}{
		{"local", store.LoadLocal},
		{"remote", store.LoadRemote},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.load()
			if err != nil {
				t.Fatalf("load on a missing file returned %v, want no error", err)
			}
			if got.Schema != domain.CatalogSchema {
				t.Errorf("Schema = %d, want %d", got.Schema, domain.CatalogSchema)
			}
			if len(got.Presets) != 0 {
				t.Errorf("got %d presets from a missing file", len(got.Presets))
			}
		})
	}
}

func TestPresetStore_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "presets")
	store := repository.NewFilePresetStore(dir)

	want := domain.Catalog{
		Schema:   domain.CatalogSchema,
		Source:   "gtr:delphi/libraries/bossy-presets",
		SyncedAt: time.Date(2026, 9, 11, 9, 12, 44, 0, time.UTC),
		Presets:  []domain.Preset{samplePreset("zeoslib"), samplePreset("delphi-neon")},
	}

	if err := store.SaveRemote(want); err != nil {
		t.Fatalf("SaveRemote: %v", err)
	}

	got, err := store.LoadRemote()
	if err != nil {
		t.Fatalf("LoadRemote: %v", err)
	}
	if got.Source != want.Source {
		t.Errorf("Source = %q, want %q", got.Source, want.Source)
	}
	if !got.SyncedAt.Equal(want.SyncedAt) {
		t.Errorf("SyncedAt = %v, want %v", got.SyncedAt, want.SyncedAt)
	}
	if len(got.Presets) != len(want.Presets) {
		t.Fatalf("got %d presets, want %d", len(got.Presets), len(want.Presets))
	}
	for i := range want.Presets {
		if got.Presets[i].ID != want.Presets[i].ID {
			t.Errorf("Presets[%d].ID = %q, want %q", i, got.Presets[i].ID, want.Presets[i].ID)
		}
		if got.Presets[i].DefaultRef != want.Presets[i].DefaultRef {
			t.Errorf("Presets[%d].DefaultRef = %+v, want %+v", i, got.Presets[i].DefaultRef, want.Presets[i].DefaultRef)
		}
	}
}

// Starting from empty on a malformed file would silently discard a catalog
// somebody spent effort on, so it has to be an error that names the path.
func TestPresetStore_MalformedFileErrors(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "presets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "local.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := repository.NewFilePresetStore(dir)
	if _, err := store.LoadLocal(); err == nil {
		t.Fatal("LoadLocal on a malformed file returned no error")
	} else if !strings.Contains(err.Error(), "local.json") {
		t.Errorf("error %q does not name the offending file", err)
	}
}

// sync overwrites remote.json wholesale. If that could touch local.json the
// user would lose their own entries on every sync.
func TestPresetStore_LocalAndRemoteAreIndependent(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "presets")
	store := repository.NewFilePresetStore(dir)

	local := domain.Catalog{Schema: domain.CatalogSchema, Presets: []domain.Preset{samplePreset("mine")}}
	if err := store.SaveLocal(local); err != nil {
		t.Fatalf("SaveLocal: %v", err)
	}

	remote := domain.Catalog{Schema: domain.CatalogSchema, Presets: []domain.Preset{samplePreset("theirs")}}
	if err := store.SaveRemote(remote); err != nil {
		t.Fatalf("SaveRemote: %v", err)
	}

	gotLocal, err := store.LoadLocal()
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if len(gotLocal.Presets) != 1 || gotLocal.Presets[0].ID != "mine" {
		t.Fatalf("saving the remote catalog changed the local one: %+v", gotLocal.Presets)
	}
}

func TestPresetStore_SaveCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested", "presets")
	store := repository.NewFilePresetStore(dir)

	if err := store.SaveLocal(domain.NewCatalog()); err != nil {
		t.Fatalf("SaveLocal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "local.json")); err != nil {
		t.Fatalf("local.json was not created: %v", err)
	}
}
