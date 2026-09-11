package domain_test

import (
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

func TestPresetRef_Validate(t *testing.T) {
	t.Parallel()

	const validSHA = "0123456789abcdef0123456789abcdef01234567"

	cases := []struct {
		name    string
		ref     domain.PresetRef
		wantErr bool
	}{
		{"default with no value", domain.PresetRef{Kind: domain.RefKindDefault}, false},
		{"default with a value", domain.PresetRef{Kind: domain.RefKindDefault, Value: "main"}, true},
		{"branch", domain.PresetRef{Kind: domain.RefKindBranch, Value: "8.0-patches"}, false},
		{"branch without value", domain.PresetRef{Kind: domain.RefKindBranch}, true},
		{"tag", domain.PresetRef{Kind: domain.RefKindTag, Value: "3.4.2-magnesium"}, false},
		{"tag without value", domain.PresetRef{Kind: domain.RefKindTag}, true},
		{"commit full sha", domain.PresetRef{Kind: domain.RefKindCommit, Value: validSHA}, false},
		{"commit uppercase sha", domain.PresetRef{Kind: domain.RefKindCommit, Value: strings.ToUpper(validSHA)}, false},
		// plumbing.NewHash zero-pads anything shorter than 40 characters, turning
		// an abbreviated SHA into a plausible-looking but wrong hash. Refuse it here.
		{"commit short sha", domain.PresetRef{Kind: domain.RefKindCommit, Value: "deadbeef"}, true},
		{"commit non-hex", domain.PresetRef{Kind: domain.RefKindCommit, Value: strings.Repeat("z", 40)}, true},
		{"commit without value", domain.PresetRef{Kind: domain.RefKindCommit}, true},
		{"unknown kind", domain.PresetRef{Kind: "latest", Value: "x"}, true},
		{"empty kind", domain.PresetRef{}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := c.ref.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestPreset_Validate(t *testing.T) {
	t.Parallel()

	valid := func(mutate func(*domain.Preset)) domain.Preset {
		p := domain.Preset{
			ID:         "dmvcframework",
			Repo:       "github.com/danieleteti/delphimvcframework",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindTag, Value: "3.4.2-magnesium"},
		}
		if mutate != nil {
			mutate(&p)
		}
		return p
	}

	cases := []struct {
		name    string
		preset  domain.Preset
		wantErr bool
	}{
		{"complete", valid(nil), false},
		// config.toml carries PYTHONENVIRONMENTDIR, so uppercase ids must pass.
		{"uppercase id", valid(func(p *domain.Preset) { p.ID = "PYTHONENVIRONMENTDIR" }), false},
		{"dotted id", valid(func(p *domain.Preset) { p.ID = "Neslib.Yaml" }), false},
		{"hyphenated id", valid(func(p *domain.Preset) { p.ID = "delphi-neon" }), false},
		{"empty id", valid(func(p *domain.Preset) { p.ID = "" }), true},
		{"id starting with a digit", valid(func(p *domain.Preset) { p.ID = "4delphi" }), true},
		{"id with a slash", valid(func(p *domain.Preset) { p.ID = "gtr/opengl" }), true},
		{"id with a space", valid(func(p *domain.Preset) { p.ID = "delphi neon" }), true},
		{"empty repo", valid(func(p *domain.Preset) { p.Repo = "" }), true},
		{"blank repo", valid(func(p *domain.Preset) { p.Repo = "   " }), true},
		{"invalid ref", valid(func(p *domain.Preset) { p.DefaultRef = domain.PresetRef{Kind: domain.RefKindTag} }), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := c.preset.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestPreset_Version(t *testing.T) {
	t.Parallel()

	const sha = "0123456789abcdef0123456789abcdef01234567"

	cases := []struct {
		name string
		ref  domain.PresetRef
		want string
	}{
		{"default", domain.PresetRef{Kind: domain.RefKindDefault}, ">0.0.0"},
		{"branch", domain.PresetRef{Kind: domain.RefKindBranch, Value: "8.0-patches"}, "8.0-patches"},
		{"tag", domain.PresetRef{Kind: domain.RefKindTag, Value: "3.4.2-magnesium"}, "3.4.2-magnesium"},
		{"commit", domain.PresetRef{Kind: domain.RefKindCommit, Value: sha}, sha},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := domain.Preset{ID: "x", Repo: "host/x", DefaultRef: c.ref}
			if got := p.Version(); got != c.want {
				t.Fatalf("Version() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCatalog_Validate(t *testing.T) {
	t.Parallel()

	preset := func(id string) domain.Preset {
		return domain.Preset{
			ID:         id,
			Repo:       "github.com/example/" + id,
			DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		}
	}

	cases := []struct {
		name    string
		catalog domain.Catalog
		wantErr bool
	}{
		{
			name:    "well formed",
			catalog: domain.Catalog{Schema: 1, Presets: []domain.Preset{preset("a"), preset("b")}},
		},
		{
			name:    "empty catalog",
			catalog: domain.Catalog{Schema: 1},
		},
		{
			name:    "unsupported schema",
			catalog: domain.Catalog{Schema: 2, Presets: []domain.Preset{preset("a")}},
			wantErr: true,
		},
		{
			name:    "missing schema",
			catalog: domain.Catalog{Presets: []domain.Preset{preset("a")}},
			wantErr: true,
		},
		{
			name:    "duplicate ids",
			catalog: domain.Catalog{Schema: 1, Presets: []domain.Preset{preset("a"), preset("a")}},
			wantErr: true,
		},
		{
			name:    "duplicate ids differing only in case",
			catalog: domain.Catalog{Schema: 1, Presets: []domain.Preset{preset("a"), preset("A")}},
			wantErr: true,
		},
		{
			name:    "invalid member",
			catalog: domain.Catalog{Schema: 1, Presets: []domain.Preset{{ID: "a"}}},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := c.catalog.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestMergeCatalogs(t *testing.T) {
	t.Parallel()

	remote := domain.Catalog{Schema: 1, Presets: []domain.Preset{
		{
			ID:          "zeoslib",
			Repo:        "github.com/marsupilami79/zeoslib",
			DefaultRef:  domain.PresetRef{Kind: domain.RefKindBranch, Value: "8.0-patches"},
			Description: "shared",
			SearchPaths: []string{"src", "src/core"},
			Tags:        []string{"gtr-standard"},
		},
		{
			ID:         "delphi-neon",
			Repo:       "github.com/paolo-rossi/delphi-neon",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		},
	}}

	local := domain.Catalog{Schema: 1, Presets: []domain.Preset{
		{
			ID:         "zeoslib",
			Repo:       "github.com/myfork/zeoslib",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindBranch, Value: "experiment"},
		},
		{
			ID:         "aaa-local-only",
			Repo:       "github.com/example/aaa",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		},
	}}

	got := domain.MergeCatalogs(remote, local)

	wantIDs := []string{"aaa-local-only", "delphi-neon", "zeoslib"}
	if len(got) != len(wantIDs) {
		t.Fatalf("merged %d presets, want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Fatalf("merged[%d].ID = %q, want %q (result must be sorted by id)", i, got[i].ID, id)
		}
	}

	zeos := got[2]
	if zeos.Repo != "github.com/myfork/zeoslib" {
		t.Errorf("local entry must shadow the remote one, got repo %q", zeos.Repo)
	}
	if zeos.DefaultRef.Value != "experiment" {
		t.Errorf("local ref must win, got %q", zeos.DefaultRef.Value)
	}
	// Shadowing replaces the entry outright. Inheriting the remote description or
	// search paths would produce a third entry that nobody wrote.
	if zeos.Description != "" {
		t.Errorf("shadowed entry inherited Description %q from the remote catalog", zeos.Description)
	}
	if len(zeos.SearchPaths) != 0 {
		t.Errorf("shadowed entry inherited SearchPaths %v from the remote catalog", zeos.SearchPaths)
	}
	if len(zeos.Tags) != 0 {
		t.Errorf("shadowed entry inherited Tags %v from the remote catalog", zeos.Tags)
	}
}

func TestMergeCatalogs_EmptyInputs(t *testing.T) {
	t.Parallel()

	if got := domain.MergeCatalogs(domain.Catalog{}, domain.Catalog{}); len(got) != 0 {
		t.Fatalf("merging two empty catalogs returned %d presets", len(got))
	}
}

func TestSubmodulePolicy_Validate(t *testing.T) {
	t.Parallel()

	cases := map[domain.SubmodulePolicy]bool{
		"":                           false,
		domain.SubmodulePolicyPinned: false,
		domain.SubmodulePolicyRemote: false,
		"Remote":                     true,
		"tips":                       true,
		"--remote":                   true,
	}
	for policy, wantErr := range cases {
		if err := policy.Validate(); (err != nil) != wantErr {
			t.Errorf("SubmodulePolicy(%q).Validate() error = %v, wantErr %v", policy, err, wantErr)
		}
	}
}

func TestSubmodulePolicy_IsRemote(t *testing.T) {
	t.Parallel()

	if !domain.SubmodulePolicyRemote.IsRemote() {
		t.Error("remote policy does not report itself as remote")
	}
	for _, policy := range []domain.SubmodulePolicy{"", domain.SubmodulePolicyPinned} {
		if policy.IsRemote() {
			t.Errorf("policy %q reports itself as remote", policy)
		}
	}
}

func TestPreset_ValidateRejectsUnknownSubmodulePolicy(t *testing.T) {
	t.Parallel()

	p := domain.Preset{
		ID:         "taurustls",
		Repo:       "github.com/JPeterMugaas/TaurusTLS",
		DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		Submodules: "tips",
	}
	if err := p.Validate(); err == nil {
		t.Fatal("an unknown submodule policy was accepted")
	}
}

func TestPackage_ModuleSubmodulePolicy(t *testing.T) {
	t.Parallel()

	pkg := domain.NewPackage()
	pkg.Submodules = map[string]string{
		"github.com/JPeterMugaas/TaurusTLS": string(domain.SubmodulePolicyRemote),
		"delphi-neon":                       string(domain.SubmodulePolicyPinned),
		"github.com/x/blank":                "",
	}

	cases := map[string]domain.SubmodulePolicy{
		// Matched on the module directory name, so the full key and the bare
		// name both resolve.
		"github.com/JPeterMugaas/TaurusTLS": domain.SubmodulePolicyRemote,
		"TaurusTLS":                         domain.SubmodulePolicyRemote,
		"taurustls":                         domain.SubmodulePolicyRemote,
		"delphi-neon":                       domain.SubmodulePolicyPinned,
		"blank":                             domain.SubmodulePolicyPinned,
		"never-declared":                    domain.SubmodulePolicyPinned,
	}
	for name, want := range cases {
		if got := pkg.ModuleSubmodulePolicy(name); got != want {
			t.Errorf("ModuleSubmodulePolicy(%q) = %q, want %q", name, got, want)
		}
	}

	var nilPkg *domain.Package
	if got := nilPkg.ModuleSubmodulePolicy("anything"); got != domain.SubmodulePolicyPinned {
		t.Errorf("nil package policy = %q, want pinned", got)
	}
}
