//nolint:testpackage // exercises the unexported action structs directly
package preset

import (
	"errors"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
)

// memStore is an in-memory ports.PresetStore so the commands are tested
// without touching the user's bossy home.
type memStore struct {
	local  domain.Catalog
	remote domain.Catalog
}

func newMemStore() *memStore {
	return &memStore{local: domain.NewCatalog(), remote: domain.NewCatalog()}
}

func (m *memStore) LoadLocal() (domain.Catalog, error)  { return m.local, nil }
func (m *memStore) LoadRemote() (domain.Catalog, error) { return m.remote, nil }
func (m *memStore) SaveLocal(c domain.Catalog) error    { m.local = c; return nil }
func (m *memStore) SaveRemote(c domain.Catalog) error   { m.remote = c; return nil }

func TestParseRefFlag(t *testing.T) {
	t.Parallel()

	const sha = "0123456789abcdef0123456789abcdef01234567"

	cases := []struct {
		name    string
		input   string
		want    domain.PresetRef
		wantErr bool
	}{
		{"empty means default", "", domain.PresetRef{Kind: domain.RefKindDefault}, false},
		{"bare default", "default", domain.PresetRef{Kind: domain.RefKindDefault}, false},
		{"branch", "branch:main", domain.PresetRef{Kind: domain.RefKindBranch, Value: "main"}, false},
		{"tag", "tag:3.4.2-magnesium", domain.PresetRef{Kind: domain.RefKindTag, Value: "3.4.2-magnesium"}, false},
		{"commit", "commit:" + sha, domain.PresetRef{Kind: domain.RefKindCommit, Value: sha}, false},
		// A branch name may contain a colon-free slash but the split must take
		// only the first colon, so refs like "branch:feature/a:b" stay intact.
		{
			"value containing a colon", "branch:feature:x",
			domain.PresetRef{Kind: domain.RefKindBranch, Value: "feature:x"}, false,
		},
		{"unknown kind", "latest:1", domain.PresetRef{}, true},
		{"missing value", "tag:", domain.PresetRef{}, true},
		{"missing separator", "tag", domain.PresetRef{}, true},
		{"default with a value", "default:main", domain.PresetRef{}, true},
		{"short commit", "commit:deadbeef", domain.PresetRef{}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseRefFlag(c.input)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseRefFlag(%q) error = %v, wantErr %v", c.input, err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Errorf("parseRefFlag(%q) = %+v, want %+v", c.input, got, c.want)
			}
		})
	}
}

func TestEditAction_AddThenList(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := presets.New(store)

	add := editAction{
		mode: modeAdd,
		id:   "dmvcframework",
		fields: presetFields{
			repo:        "github.com/danieleteti/delphimvcframework",
			ref:         "tag:3.4.2-magnesium",
			name:        "DMVC Framework",
			searchPaths: []string{"sources", "lib/loggerpro"},
			platforms:   []string{"Win32", "Win64"},
			tags:        []string{"web"},
		},
	}
	if _, err := add.run(svc); err != nil {
		t.Fatalf("add: %v", err)
	}

	stored, ok := store.local.Find("dmvcframework")
	if !ok {
		t.Fatal("preset was not written to the local catalog")
	}
	if stored.DefaultRef.Kind != domain.RefKindTag || stored.DefaultRef.Value != "3.4.2-magnesium" {
		t.Errorf("DefaultRef = %+v", stored.DefaultRef)
	}
	if len(stored.SearchPaths) != 2 {
		t.Errorf("SearchPaths = %v", stored.SearchPaths)
	}

	lines, err := listAction{}.run(svc)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "dmvcframework") {
		t.Errorf("list output does not mention the preset:\n%s", joined)
	}
}

func TestEditAction_AddRejectsDuplicate(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := presets.New(store)

	add := editAction{mode: modeAdd, id: "neon", fields: presetFields{repo: "github.com/paolo-rossi/delphi-neon"}}
	if _, err := add.run(svc); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := add.run(svc)
	if err == nil {
		t.Fatal("second add returned no error")
	}
	if !errors.Is(err, presets.ErrExists) {
		t.Errorf("error = %v, want ErrExists", err)
	}
}

// update patches only the flags that were passed, so an unrelated field set
// earlier must survive.
func TestEditAction_UpdatePatchesOnlyProvidedFields(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := presets.New(store)

	add := editAction{
		mode: modeAdd,
		id:   "zeoslib",
		fields: presetFields{
			repo: "github.com/marsupilami79/zeoslib", ref: "branch:8.0-patches", searchPaths: []string{"src"},
		},
	}
	if _, err := add.run(svc); err != nil {
		t.Fatalf("add: %v", err)
	}

	update := editAction{mode: modeUpdate, id: "zeoslib", fields: presetFields{ref: "tag:8.0.1", refSet: true}}
	if _, err := update.run(svc); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, ok := store.local.Find("zeoslib")
	if !ok {
		t.Fatal("preset disappeared")
	}
	if got.DefaultRef.Kind != domain.RefKindTag || got.DefaultRef.Value != "8.0.1" {
		t.Errorf("DefaultRef = %+v, want tag 8.0.1", got.DefaultRef)
	}
	if got.Repo != "github.com/marsupilami79/zeoslib" {
		t.Errorf("Repo was reset to %q", got.Repo)
	}
	if len(got.SearchPaths) != 1 || got.SearchPaths[0] != "src" {
		t.Errorf("SearchPaths were reset to %v", got.SearchPaths)
	}
}

// Overriding a shared entry is why the local catalog exists, so update must
// accept an id that currently lives only in the synced catalog.
func TestEditAction_UpdateCreatesOverrideForRemoteOnlyEntry(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.remote.Presets = []domain.Preset{{
		ID:         "spring4d",
		Repo:       "bitbucket.org/sglienke/spring4d",
		DefaultRef: domain.PresetRef{Kind: domain.RefKindBranch, Value: "master"},
	}}
	svc := presets.New(store)

	update := editAction{
		mode: modeUpdate, id: "spring4d",
		fields: presetFields{repo: "github.com/myfork/spring4d", repoSet: true},
	}
	if _, err := update.run(svc); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, ok := store.local.Find("spring4d")
	if !ok {
		t.Fatal("no local override was created")
	}
	if got.Repo != "github.com/myfork/spring4d" {
		t.Errorf("Repo = %q", got.Repo)
	}
	// The base for the override is the remote entry, so its ref carries over.
	if got.DefaultRef.Value != "master" {
		t.Errorf("DefaultRef = %+v, want the remote entry's branch master", got.DefaultRef)
	}
	if len(store.remote.Presets) != 1 || store.remote.Presets[0].Repo != "bitbucket.org/sglienke/spring4d" {
		t.Errorf("the synced catalog was modified: %+v", store.remote.Presets)
	}
}

func TestEditAction_RemoveUnknown(t *testing.T) {
	t.Parallel()

	svc := presets.New(newMemStore())

	_, err := editAction{mode: modeRemove, id: "nope"}.run(svc)
	if err == nil {
		t.Fatal("removing an unknown preset returned no error")
	}
	if !errors.Is(err, presets.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestEditAction_AddRequiresRepo(t *testing.T) {
	t.Parallel()

	svc := presets.New(newMemStore())

	_, err := editAction{mode: modeAdd, id: "x"}.run(svc)
	if err == nil {
		t.Fatal("adding a preset with no repository returned no error")
	}
}

func TestListAction_EmptyCatalog(t *testing.T) {
	t.Parallel()

	lines, err := listAction{}.run(presets.New(newMemStore()))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "No presets") {
		t.Errorf("lines = %v, want a single empty-catalog notice", lines)
	}
}

func TestListAction_FiltersByTag(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.remote.Presets = []domain.Preset{
		{ID: "a", Repo: "h/a", DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault}, Tags: []string{"gtr-standard"}},
		{ID: "b", Repo: "h/b", DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault}, Tags: []string{"web"}},
	}

	lines, err := listAction{tag: "gtr-standard"}.run(presets.New(store))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "a") || strings.Contains(joined, "h/b") {
		t.Errorf("tag filter output:\n%s", joined)
	}
}

func TestShowAction_UnknownID(t *testing.T) {
	t.Parallel()

	_, err := showAction{id: "nope"}.run(presets.New(newMemStore()))
	if err == nil {
		t.Fatal("show of an unknown id returned no error")
	}
}
