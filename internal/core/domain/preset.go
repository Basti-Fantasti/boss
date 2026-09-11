package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/basti-fantasti/bossy/pkg/consts"
)

// CatalogSchema is the only catalog schema version this build understands.
// A catalog carrying anything else is refused rather than read on a
// best-effort basis: a silently misread catalog installs the wrong things.
const CatalogSchema = 1

// DefaultVersionConstraint is what a preset with no pinned ref writes into
// bossy.json, matching what `bossy install <pkg>` records for an argument
// carrying no explicit version.
const DefaultVersionConstraint = ">0.0.0"

// RefKind names the kind of git reference a preset pins to.
type RefKind string

// The reference kinds a preset may declare.
const (
	// RefKindDefault follows whatever the remote advertises as HEAD.
	RefKindDefault RefKind = "default"
	// RefKindBranch tracks a named branch.
	RefKindBranch RefKind = "branch"
	// RefKindTag tracks a named tag.
	RefKindTag RefKind = "tag"
	// RefKindCommit pins to a full commit SHA.
	RefKindCommit RefKind = "commit"
)

// PresetRef is the reference a preset pins to by default.
type PresetRef struct {
	Kind  RefKind `json:"kind"`
	Value string  `json:"value,omitempty"`
}

// Validate reports whether the reference is well formed for its kind.
func (r PresetRef) Validate() error {
	switch r.Kind {
	case RefKindDefault:
		if r.Value != "" {
			return fmt.Errorf("ref kind %q must not carry a value, got %q", r.Kind, r.Value)
		}
		return nil
	case RefKindBranch, RefKindTag:
		if strings.TrimSpace(r.Value) == "" {
			return fmt.Errorf("ref kind %q requires a value", r.Kind)
		}
		return nil
	case RefKindCommit:
		if !isFullHexSHA(r.Value) {
			return fmt.Errorf("ref kind %q requires a full 40-character hex SHA, got %q", r.Kind, r.Value)
		}
		return nil
	default:
		return fmt.Errorf("unknown ref kind %q", r.Kind)
	}
}

// String renders the reference the way the TUI and `preset show` display it.
func (r PresetRef) String() string {
	if r.Kind == RefKindDefault {
		return string(RefKindDefault)
	}
	return string(r.Kind) + " " + r.Value
}

// isFullHexSHA reports whether s is exactly 40 hex digits.
//
// go-git's plumbing.NewHash discards the hex decoding error and zero-pads,
// so an abbreviated SHA such as "deadbeef" becomes a well-formed but wrong
// 40-character hash that nothing downstream can distinguish from a real one.
// The length has to be checked before the value reaches go-git.
func isFullHexSHA(s string) bool {
	const shaLen = 40
	if len(s) != shaLen {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// Preset is one entry in the dependency catalog: a library a project may pick
// from, together with the metadata needed to wire it up correctly.
//
// SearchPaths and Platforms exist because most Delphi libraries ship no
// bossy.json of their own, so bossy cannot learn from the dependency which of
// its directories actually hold the library. Carrying that knowledge in the
// catalog is what keeps a repository with samples and tests beside its source
// from putting hundreds of directories on the IDE search path.
type Preset struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	Repo        string    `json:"repo"`
	DefaultRef  PresetRef `json:"default_ref"`
	SearchPaths []string  `json:"searchpaths,omitempty"`
	Platforms   []string  `json:"platforms,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
}

// Validate reports whether the preset is complete enough to install from.
func (p Preset) Validate() error {
	if !consts.PresetIDPattern.MatchString(p.ID) {
		return fmt.Errorf("preset id %q invalid (must match %s)", p.ID, consts.PresetIDPattern.String())
	}
	if strings.TrimSpace(p.Repo) == "" {
		return fmt.Errorf("preset %q has no repository", p.ID)
	}
	if err := p.DefaultRef.Validate(); err != nil {
		return fmt.Errorf("preset %q: %w", p.ID, err)
	}
	return nil
}

// DisplayName is the human-facing label, falling back to the id.
func (p Preset) DisplayName() string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

// Version returns the value this preset writes into bossy.json's dependencies
// map for its default reference.
func (p Preset) Version() string {
	return VersionForRef(p.DefaultRef)
}

// VersionForRef maps a reference onto the bossy.json dependency value.
// Branch, tag and commit are all written literally; only an unpinned preset
// falls back to the open constraint.
func VersionForRef(ref PresetRef) string {
	if ref.Kind == RefKindDefault {
		return DefaultVersionConstraint
	}
	return ref.Value
}

// HasTag reports whether the preset carries the given tag, case-insensitively.
func (p Preset) HasTag(tag string) bool {
	for _, t := range p.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// Catalog is a collection of presets as stored on disk.
type Catalog struct {
	Schema   int       `json:"schema"`
	Source   string    `json:"source,omitempty"`
	SyncedAt time.Time `json:"synced_at,omitempty"`
	Presets  []Preset  `json:"presets"`
}

// NewCatalog returns an empty catalog stamped with the current schema.
func NewCatalog() Catalog {
	return Catalog{Schema: CatalogSchema, Presets: []Preset{}}
}

// Validate reports whether the catalog can be trusted as a whole.
//
// Ids are compared case-insensitively because they address entries across the
// remote and local catalogs; two entries differing only in case would shadow
// each other unpredictably depending on map iteration order.
func (c Catalog) Validate() error {
	if c.Schema != CatalogSchema {
		return fmt.Errorf("unsupported catalog schema %d, expected %d", c.Schema, CatalogSchema)
	}
	seen := make(map[string]struct{}, len(c.Presets))
	for _, p := range c.Presets {
		if err := p.Validate(); err != nil {
			return err
		}
		key := strings.ToLower(p.ID)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate preset id %q", p.ID)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// Find returns the preset with the given id, matched case-insensitively.
func (c Catalog) Find(id string) (Preset, bool) {
	for _, p := range c.Presets {
		if strings.EqualFold(p.ID, id) {
			return p, true
		}
	}
	return Preset{}, false
}

// MergeCatalogs combines the synced remote catalog with the user's local one,
// sorted by id.
//
// A local entry shadows a remote entry with the same id in full. Merging
// field by field is deliberately not done: when the remote entry later
// changes, a partial override would produce a third entry that neither the
// catalog maintainer nor the user ever wrote.
func MergeCatalogs(remote, local Catalog) []Preset {
	merged := make(map[string]Preset, len(remote.Presets)+len(local.Presets))
	for _, p := range remote.Presets {
		merged[strings.ToLower(p.ID)] = p
	}
	for _, p := range local.Presets {
		merged[strings.ToLower(p.ID)] = p
	}

	out := make([]Preset, 0, len(merged))
	for _, p := range merged {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}
