package presets_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
)

// fixtureCloner returns a Cloner that writes the given presets.json content
// into the destination instead of cloning, so sync is exercised without a
// network or a git binary.
func fixtureCloner(t *testing.T, content string) presets.Cloner {
	t.Helper()
	return func(_, destDir string) error {
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destDir, presets.CatalogFileName), []byte(content), 0o600)
	}
}

func catalogJSON(t *testing.T, ids ...string) string {
	t.Helper()
	catalog := domain.Catalog{Schema: domain.CatalogSchema}
	for _, id := range ids {
		catalog.Presets = append(catalog.Presets, domain.Preset{
			ID:         id,
			Repo:       "github.com/example/" + id,
			DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		})
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func TestSync_WritesRemoteCatalog(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := presets.New(store)

	const source = "gtr:delphi/libraries/bossy-presets"
	got, err := svc.Sync(source, fixtureCloner(t, catalogJSON(t, "zeoslib", "neon")))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(got.Presets) != 2 {
		t.Fatalf("returned %d presets, want 2", len(got.Presets))
	}
	if got.Source != source {
		t.Errorf("Source = %q, want %q", got.Source, source)
	}
	if got.SyncedAt.IsZero() {
		t.Error("SyncedAt was not stamped")
	}
	if len(store.remote.Presets) != 2 {
		t.Errorf("the remote catalog holds %d presets, want 2", len(store.remote.Presets))
	}
	if len(store.local.Presets) != 0 {
		t.Errorf("sync wrote to the local catalog: %+v", store.local.Presets)
	}
}

// A catalog repository with one bad entry must not leave the machine with no
// catalog at all; the working one has to survive.
func TestSync_InvalidCatalogLeavesExistingRemoteIntact(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote = domain.Catalog{Schema: domain.CatalogSchema, Presets: []domain.Preset{preset("working")}}
	svc := presets.New(store)

	broken := `{"schema":1,"presets":[{"id":"","repo":""}]}`
	if _, err := svc.Sync("gtr:x/y", fixtureCloner(t, broken)); err == nil {
		t.Fatal("Sync of an invalid catalog returned no error")
	}

	if len(store.remote.Presets) != 1 || store.remote.Presets[0].ID != "working" {
		t.Errorf("the working catalog was destroyed: %+v", store.remote.Presets)
	}
}

func TestSync_MalformedJSONLeavesExistingRemoteIntact(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote = domain.Catalog{Schema: domain.CatalogSchema, Presets: []domain.Preset{preset("working")}}
	svc := presets.New(store)

	if _, err := svc.Sync("gtr:x/y", fixtureCloner(t, "{ not json")); err == nil {
		t.Fatal("Sync of malformed JSON returned no error")
	}
	if len(store.remote.Presets) != 1 {
		t.Errorf("the working catalog was destroyed: %+v", store.remote.Presets)
	}
}

func TestSync_MissingCatalogFileErrors(t *testing.T) {
	t.Parallel()

	svc := presets.New(newFakeStore())

	emptyClone := func(_, destDir string) error { return os.MkdirAll(destDir, 0o755) }

	_, err := svc.Sync("gtr:x/y", emptyClone)
	if err == nil {
		t.Fatal("Sync of a repository without presets.json returned no error")
	}
	if !strings.Contains(err.Error(), presets.CatalogFileName) {
		t.Errorf("error %q does not name the missing file", err)
	}
}

func TestSync_CloneFailureIsSurfaced(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote = domain.Catalog{Schema: domain.CatalogSchema, Presets: []domain.Preset{preset("working")}}
	svc := presets.New(store)

	failing := func(_, _ string) error { return os.ErrPermission }

	if _, err := svc.Sync("gtr:x/y", failing); err == nil {
		t.Fatal("Sync swallowed a clone failure")
	}
	if len(store.remote.Presets) != 1 {
		t.Errorf("a failed clone damaged the catalog: %+v", store.remote.Presets)
	}
}

func TestSync_RequiresASource(t *testing.T) {
	t.Parallel()

	svc := presets.New(newFakeStore())

	_, err := svc.Sync("", fixtureCloner(t, catalogJSON(t, "a")))
	if err == nil {
		t.Fatal("Sync with no source returned no error")
	}
	if !strings.Contains(err.Error(), "--source") {
		t.Errorf("error %q does not tell the user how to set a source", err)
	}
}
