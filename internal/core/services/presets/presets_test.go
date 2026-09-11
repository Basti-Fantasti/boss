package presets_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
)

// fakeStore is an in-memory ports.PresetStore so the service is tested without
// touching disk.
type fakeStore struct {
	local    domain.Catalog
	remote   domain.Catalog
	loadErr  error
	saveErr  error
	saves    int
	lastSave domain.Catalog
}

func newFakeStore() *fakeStore {
	return &fakeStore{local: domain.NewCatalog(), remote: domain.NewCatalog()}
}

func (f *fakeStore) LoadLocal() (domain.Catalog, error)  { return f.local, f.loadErr }
func (f *fakeStore) LoadRemote() (domain.Catalog, error) { return f.remote, f.loadErr }

func (f *fakeStore) SaveLocal(c domain.Catalog) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.local = c
	f.saves++
	f.lastSave = c
	return nil
}

func (f *fakeStore) SaveRemote(c domain.Catalog) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.remote = c
	f.saves++
	f.lastSave = c
	return nil
}

func preset(id string, tags ...string) domain.Preset {
	return domain.Preset{
		ID:         id,
		Repo:       "github.com/example/" + id,
		DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		Tags:       tags,
	}
}

func TestService_AllMergesAndSorts(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote.Presets = []domain.Preset{preset("zeoslib"), preset("delphi-neon")}
	store.local.Presets = []domain.Preset{preset("aaa"), {
		ID:         "zeoslib",
		Repo:       "github.com/myfork/zeoslib",
		DefaultRef: domain.PresetRef{Kind: domain.RefKindBranch, Value: "experiment"},
	}}

	got, err := presets.New(store).All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	want := []string{"aaa", "delphi-neon", "zeoslib"}
	if len(got) != len(want) {
		t.Fatalf("got %d presets, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("presets[%d].ID = %q, want %q", i, got[i].ID, id)
		}
	}
	if got[2].Repo != "github.com/myfork/zeoslib" {
		t.Errorf("local override did not win: repo = %q", got[2].Repo)
	}
}

func TestService_ByIDIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote.Presets = []domain.Preset{preset("PYTHONENVIRONMENTDIR")}

	svc := presets.New(store)

	got, ok, err := svc.ByID("pythonenvironmentdir")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !ok {
		t.Fatal("ByID did not find the preset")
	}
	if got.ID != "PYTHONENVIRONMENTDIR" {
		t.Errorf("ByID returned %q", got.ID)
	}

	if _, ok, err = svc.ByID("nope"); err != nil || ok {
		t.Errorf("ByID(nope) = ok %v, err %v; want false, nil", ok, err)
	}
}

func TestService_ByTagFiltersAcrossBothCatalogs(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote.Presets = []domain.Preset{preset("zeoslib", "gtr-standard"), preset("neon", "web")}
	store.local.Presets = []domain.Preset{preset("mine", "GTR-STANDARD")}

	got, err := presets.New(store).ByTag("gtr-standard")
	if err != nil {
		t.Fatalf("ByTag: %v", err)
	}

	want := []string{"mine", "zeoslib"}
	if len(got) != len(want) {
		t.Fatalf("got %d presets, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("presets[%d].ID = %q, want %q (tag match must be case-insensitive and sorted)", i, got[i].ID, id)
		}
	}
}

func TestService_AddRejectsDuplicate(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.local.Presets = []domain.Preset{preset("zeoslib")}

	err := presets.New(store).Add(preset("ZEOSLIB"))
	if err == nil {
		t.Fatal("Add of an existing local id returned no error")
	}
	if !errors.Is(err, presets.ErrExists) {
		t.Errorf("error = %v, want ErrExists", err)
	}
}

func TestService_AddRejectsInvalid(t *testing.T) {
	t.Parallel()

	store := newFakeStore()

	err := presets.New(store).Add(domain.Preset{ID: "no repo", Repo: ""})
	if err == nil {
		t.Fatal("Add of an invalid preset returned no error")
	}
	if store.saves != 0 {
		t.Errorf("an invalid preset was written to the store (%d saves)", store.saves)
	}
}

// Overriding a shared entry is the documented reason the local catalog exists,
// so Update must accept an id that is only present remotely.
func TestService_UpdateCreatesLocalOverrideForRemoteEntry(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote.Presets = []domain.Preset{preset("zeoslib")}

	override := domain.Preset{
		ID:         "zeoslib",
		Repo:       "github.com/myfork/zeoslib",
		DefaultRef: domain.PresetRef{Kind: domain.RefKindBranch, Value: "experiment"},
	}
	if err := presets.New(store).Update(override); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(store.local.Presets) != 1 || store.local.Presets[0].Repo != "github.com/myfork/zeoslib" {
		t.Fatalf("local catalog = %+v, want the override", store.local.Presets)
	}
	if len(store.remote.Presets) != 1 || store.remote.Presets[0].Repo != "github.com/example/zeoslib" {
		t.Errorf("Update must not touch the remote catalog, got %+v", store.remote.Presets)
	}
}

func TestService_UpdateReplacesExistingLocalEntry(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.local.Presets = []domain.Preset{preset("zeoslib"), preset("neon")}

	override := domain.Preset{
		ID:         "zeoslib",
		Repo:       "github.com/myfork/zeoslib",
		DefaultRef: domain.PresetRef{Kind: domain.RefKindTag, Value: "8.0.1"},
	}
	if err := presets.New(store).Update(override); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(store.local.Presets) != 2 {
		t.Fatalf("Update changed the local entry count to %d", len(store.local.Presets))
	}
	got, ok := store.local.Find("zeoslib")
	if !ok || got.DefaultRef.Value != "8.0.1" {
		t.Errorf("local zeoslib = %+v, want the override", got)
	}
}

func TestService_RemoveUnknownErrors(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote.Presets = []domain.Preset{preset("zeoslib")}

	// Present remotely but not locally: there is nothing to remove, and
	// removing the remote entry is not something the client may do.
	err := presets.New(store).Remove("zeoslib")
	if err == nil {
		t.Fatal("Remove of a remote-only id returned no error")
	}
	if !errors.Is(err, presets.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestService_RemoveDeletesLocalEntry(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.local.Presets = []domain.Preset{preset("zeoslib"), preset("neon")}

	if err := presets.New(store).Remove("ZEOSLIB"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := store.local.Find("zeoslib"); ok {
		t.Error("entry survived Remove")
	}
	if _, ok := store.local.Find("neon"); !ok {
		t.Error("Remove deleted the wrong entry")
	}
}

func TestService_ImportLocalCountsAddedAndReplaced(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.local.Presets = []domain.Preset{preset("zeoslib")}

	incoming := domain.Catalog{Schema: domain.CatalogSchema, Presets: []domain.Preset{
		preset("zeoslib"),
		preset("neon"),
		preset("markdown"),
	}}

	added, replaced, err := presets.New(store).ImportLocal(incoming)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if added != 2 {
		t.Errorf("added = %d, want 2", added)
	}
	if replaced != 1 {
		t.Errorf("replaced = %d, want 1", replaced)
	}
	if len(store.local.Presets) != 3 {
		t.Errorf("local catalog holds %d presets, want 3", len(store.local.Presets))
	}
}

func TestService_ImportLocalRejectsInvalidCatalog(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.local.Presets = []domain.Preset{preset("zeoslib")}

	// Wrong schema: nothing may be written, and the existing catalog must survive.
	_, _, err := presets.New(store).ImportLocal(domain.Catalog{Schema: 99})
	if err == nil {
		t.Fatal("ImportLocal of an unsupported schema returned no error")
	}
	if store.saves != 0 {
		t.Errorf("an invalid catalog was written (%d saves)", store.saves)
	}
	if len(store.local.Presets) != 1 {
		t.Errorf("the existing local catalog was damaged: %+v", store.local.Presets)
	}
}

func TestService_ReplaceRemoteValidatesFirst(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.remote.Presets = []domain.Preset{preset("working")}

	err := presets.New(store).ReplaceRemote(domain.Catalog{
		Schema:  domain.CatalogSchema,
		Presets: []domain.Preset{{ID: "broken"}},
	})
	if err == nil {
		t.Fatal("ReplaceRemote of an invalid catalog returned no error")
	}
	if len(store.remote.Presets) != 1 || store.remote.Presets[0].ID != "working" {
		t.Errorf("a bad catalog destroyed the working one: %+v", store.remote.Presets)
	}
}

func TestService_AllSurfacesStoreErrors(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.loadErr = errors.New("disk on fire")

	if _, err := presets.New(store).All(); err == nil {
		t.Fatal("All swallowed a store error")
	} else if !strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("error %q does not wrap the store error", err)
	}
}
