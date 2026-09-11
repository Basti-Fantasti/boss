//nolint:testpackage // exercises the unexported add action directly
package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/adapters/primary/tui"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
)

type addTestStore struct {
	local  domain.Catalog
	remote domain.Catalog
}

func (s *addTestStore) LoadLocal() (domain.Catalog, error)  { return s.local, nil }
func (s *addTestStore) LoadRemote() (domain.Catalog, error) { return s.remote, nil }
func (s *addTestStore) SaveLocal(c domain.Catalog) error    { s.local = c; return nil }
func (s *addTestStore) SaveRemote(c domain.Catalog) error   { s.remote = c; return nil }

func addTestService() *presets.Service {
	store := &addTestStore{local: domain.NewCatalog(), remote: domain.NewCatalog()}
	store.remote.Presets = []domain.Preset{
		{
			ID:          "dmvcframework",
			Repo:        "github.com/danieleteti/delphimvcframework",
			DefaultRef:  domain.PresetRef{Kind: domain.RefKindTag, Value: "3.4.2-magnesium"},
			SearchPaths: []string{"sources", "lib/loggerpro"},
			Tags:        []string{"gtr-standard", "web"},
		},
		{
			ID:         "zeoslib",
			Repo:       "github.com/marsupilami79/zeoslib",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindBranch, Value: "8.0-patches"},
			Tags:       []string{"gtr-standard"},
		},
		{
			ID:         "neon",
			Repo:       "github.com/paolo-rossi/delphi-neon",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		},
	}
	return presets.New(store)
}

func TestResolveByIDs(t *testing.T) {
	t.Parallel()

	got, err := resolveByIDs(addTestService(), []string{"zeoslib", "DMVCFRAMEWORK"})
	if err != nil {
		t.Fatalf("resolveByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d selections, want 2", len(got))
	}
	// Sorted by id so the manifest is written the same way every run.
	if got[0].Preset.ID != "dmvcframework" || got[1].Preset.ID != "zeoslib" {
		t.Errorf("selections = %q, %q; want them sorted by id", got[0].Preset.ID, got[1].Preset.ID)
	}
	if got[0].Ref.Value != "3.4.2-magnesium" {
		t.Errorf("Ref = %+v, want the catalog default", got[0].Ref)
	}
}

func TestResolveByIDs_UnknownIDErrors(t *testing.T) {
	t.Parallel()

	_, err := resolveByIDs(addTestService(), []string{"zeoslib", "nosuchthing"})
	if err == nil {
		t.Fatal("an unknown id returned no error")
	}
	if !strings.Contains(err.Error(), "nosuchthing") {
		t.Errorf("error %q does not name the unknown id", err)
	}
	// Naming the fix beats making the user guess at the catalog's contents.
	if !strings.Contains(err.Error(), "bossy preset list") {
		t.Errorf("error %q does not point at 'bossy preset list'", err)
	}
}

func TestResolveByTag(t *testing.T) {
	t.Parallel()

	got, err := resolveByTag(addTestService(), "gtr-standard")
	if err != nil {
		t.Fatalf("resolveByTag: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d selections, want 2: %+v", len(got), got)
	}
	if got[0].Preset.ID != "dmvcframework" || got[1].Preset.ID != "zeoslib" {
		t.Errorf("selections = %q, %q", got[0].Preset.ID, got[1].Preset.ID)
	}
}

// A tag nobody uses is a typo, not an instruction to install nothing.
func TestResolveByTag_UnknownTagErrors(t *testing.T) {
	t.Parallel()

	_, err := resolveByTag(addTestService(), "nosuchtag")
	if err == nil {
		t.Fatal("an unmatched tag returned no error")
	}
	if !strings.Contains(err.Error(), "nosuchtag") {
		t.Errorf("error %q does not name the tag", err)
	}
}

func TestApplyToManifest_WritesSearchPaths(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	sels := []tui.Selection{{
		Preset: domain.Preset{
			ID:          "dmvcframework",
			Repo:        "github.com/danieleteti/delphimvcframework",
			SearchPaths: []string{"sources", "lib/loggerpro"},
		},
		Ref: domain.PresetRef{Kind: domain.RefKindTag, Value: "3.4.2-magnesium"},
	}}

	applyToManifest(pkg, sels)

	paths, ok := pkg.ModuleSearchPaths("delphimvcframework")
	if !ok {
		t.Fatalf("no search paths recorded; manifest holds %v", pkg.SearchPaths)
	}
	if len(paths) != 2 || paths[0] != "sources" {
		t.Errorf("search paths = %v", paths)
	}
}

// A preset with no search paths must not write an empty restriction: an empty
// list would restrict the dependency to nothing rather than leaving it
// unrestricted.
func TestApplyToManifest_NoSearchPathsWritesNothing(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	applyToManifest(pkg, []tui.Selection{{
		Preset: domain.Preset{ID: "neon", Repo: "github.com/paolo-rossi/delphi-neon"},
		Ref:    domain.PresetRef{Kind: domain.RefKindDefault},
	}})

	if len(pkg.SearchPaths) != 0 {
		t.Errorf("SearchPaths = %v, want none", pkg.SearchPaths)
	}
}

// Adding a preset must not discard a restriction the user already tuned by hand.
func TestApplyToManifest_KeepsExistingSearchPathsForOtherModules(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	pkg.SearchPaths = map[string][]string{"spring4d": {"Source"}}

	applyToManifest(pkg, []tui.Selection{{
		Preset: domain.Preset{ID: "zeoslib", Repo: "github.com/marsupilami79/zeoslib", SearchPaths: []string{"src"}},
		Ref:    domain.PresetRef{Kind: domain.RefKindBranch, Value: "8.0-patches"},
	}})

	if _, ok := pkg.ModuleSearchPaths("spring4d"); !ok {
		t.Errorf("an unrelated restriction was dropped: %v", pkg.SearchPaths)
	}
	if _, ok := pkg.ModuleSearchPaths("zeoslib"); !ok {
		t.Errorf("the new restriction was not written: %v", pkg.SearchPaths)
	}
}

func TestInstallArgs(t *testing.T) {
	t.Parallel()

	const sha = "0123456789abcdef0123456789abcdef01234567"

	got := installArgs([]tui.Selection{
		{
			Preset: domain.Preset{ID: "zeoslib", Repo: "github.com/marsupilami79/zeoslib"},
			Ref:    domain.PresetRef{Kind: domain.RefKindBranch, Value: "8.0-patches"},
		},
		{
			Preset: domain.Preset{ID: "neon", Repo: "github.com/paolo-rossi/delphi-neon"},
			Ref:    domain.PresetRef{Kind: domain.RefKindDefault},
		},
		{
			Preset: domain.Preset{ID: "pinned", Repo: "gtr:delphi/libraries/opengl"},
			Ref:    domain.PresetRef{Kind: domain.RefKindCommit, Value: sha},
		},
	})

	want := []string{
		"github.com/marsupilami79/zeoslib@8.0-patches",
		"github.com/paolo-rossi/delphi-neon@>0.0.0",
		"gtr:delphi/libraries/opengl@" + sha,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Without a terminal the picker would block on a keystroke that never arrives,
// which in CI means a hung job rather than a failed one.
func TestAddAction_NonTTYWithoutArgsErrors(t *testing.T) {
	t.Parallel()

	action := addAction{interactive: func() bool { return false }}

	_, err := action.resolve(addTestService())
	if err == nil {
		t.Fatal("a non-interactive run with no arguments returned no error")
	}
	if !errors.Is(err, errNoTTY) {
		t.Errorf("error = %v, want errNoTTY", err)
	}
	if !strings.Contains(err.Error(), "--tag") {
		t.Errorf("error %q does not name the non-interactive alternatives", err)
	}
}

// Explicit ids work with no terminal, which is the whole point of the flag form.
func TestAddAction_IDsWorkWithoutATTY(t *testing.T) {
	t.Parallel()

	action := addAction{ids: []string{"zeoslib"}, interactive: func() bool { return false }}

	got, err := action.resolve(addTestService())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].Preset.ID != "zeoslib" {
		t.Errorf("selections = %+v", got)
	}
}

func TestAddAction_EmptyCatalogErrors(t *testing.T) {
	t.Parallel()

	empty := presets.New(&addTestStore{local: domain.NewCatalog(), remote: domain.NewCatalog()})
	action := addAction{interactive: func() bool { return true }}

	_, err := action.resolve(empty)
	if err == nil {
		t.Fatal("an empty catalog returned no error")
	}
	if !strings.Contains(err.Error(), "preset sync") {
		t.Errorf("error %q does not say how to populate the catalog", err)
	}
}

// `add` used to alias `install`, so a repository key passed to the new command
// is an old habit rather than a typo and should be named as such.
func TestResolveByIDs_RepositoryKeyPointsAtInstall(t *testing.T) {
	t.Parallel()

	_, err := resolveByIDs(addTestService(), []string{"github.com/danieleteti/delphimvcframework"})
	if err == nil {
		t.Fatal("a repository key returned no error")
	}
	if !strings.Contains(err.Error(), "bossy install") {
		t.Errorf("error %q does not point at bossy install", err)
	}
}

func TestLooksLikeRepositoryKey(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"github.com/danieleteti/delphimvcframework": true,
		"gtr:delphi/libraries/opengl":               true,
		"git@gitlab.gtr.de:delphi/x.git":            true,
		"dmvcframework":                             false,
		"PYTHONENVIRONMENTDIR":                      false,
		"Neslib.Yaml":                               false,
		"delphi-neon":                               false,
	}
	for in, want := range cases {
		if got := looksLikeRepositoryKey(in); got != want {
			t.Errorf("looksLikeRepositoryKey(%q) = %v, want %v", in, got, want)
		}
	}
}

// A preset asking for floating submodules has to say so in bossy.json: CI never
// reads the catalog, it installs from the committed manifest.
func TestApplyToManifest_WritesRemoteSubmodulePolicy(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	applyToManifest(pkg, []tui.Selection{{
		Preset: domain.Preset{
			ID:         "taurustls",
			Repo:       "github.com/JPeterMugaas/TaurusTLS",
			Submodules: domain.SubmodulePolicyRemote,
		},
		Ref: domain.PresetRef{Kind: domain.RefKindBranch, Value: "main"},
	}})

	if got := pkg.ModuleSubmodulePolicy("TaurusTLS"); got != domain.SubmodulePolicyRemote {
		t.Errorf("policy = %q, want %q; manifest holds %v", got, domain.SubmodulePolicyRemote, pkg.Submodules)
	}
}

// Recording "pinned" on every entry would fill the manifest with restatements
// of git's own default.
func TestApplyToManifest_DefaultSubmodulePolicyWritesNothing(t *testing.T) {
	t.Parallel()

	for _, policy := range []domain.SubmodulePolicy{"", domain.SubmodulePolicyPinned} {
		pkg := domain.NewPackage()
		applyToManifest(pkg, []tui.Selection{{
			Preset: domain.Preset{ID: "neon", Repo: "github.com/paolo-rossi/delphi-neon", Submodules: policy},
			Ref:    domain.PresetRef{Kind: domain.RefKindDefault},
		}})

		if len(pkg.Submodules) != 0 {
			t.Errorf("policy %q wrote %v, want nothing", policy, pkg.Submodules)
		}
		if got := pkg.ModuleSubmodulePolicy("delphi-neon"); got != domain.SubmodulePolicyPinned {
			t.Errorf("policy %q reads back as %q, want pinned", policy, got)
		}
	}
}
