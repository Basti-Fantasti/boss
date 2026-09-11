// Package tui provides the interactive dependency picker.
//
// It is a primary adapter and holds no business logic: it is handed a catalog
// and a ref lister, and returns the user's selections for the caller to install
// through the existing installer service.
package tui

import (
	"sort"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-git/go-git/v5/plumbing"
)

// RefLister enumerates a preset's remote branches and tags.
//
// Injected so the model never reaches the network itself and can be driven
// entirely from tests.
type RefLister func(preset domain.Preset) ([]*plumbing.Reference, error)

// Selection is one dependency the user chose, with the reference to install.
type Selection struct {
	Preset domain.Preset
	Ref    domain.PresetRef
	// Freeze writes the resolved commit into bossy.json rather than the
	// symbolic name, so `bossy update` cannot move the dependency.
	Freeze bool
	// Unverified marks a selection whose refs could not be listed. The catalog
	// default is kept as-is and the caller warns about it. Substituting a
	// default branch here is exactly the silent wrong-branch behaviour removed
	// in dda87ee.
	Unverified bool
}

// focusArea is which pane has the keyboard.
type focusArea int

const (
	focusList focusArea = iota
	focusDetail
)

// refLoadState tracks a preset's ref listing.
type refLoadState int

const (
	refsIdle refLoadState = iota
	refsLoading
	refsReady
	refsFailed
)

// refState is the cached listing for one preset.
type refState struct {
	state   refLoadState
	choices []refChoice
	err     string
	// cursor is the highlighted choice while the detail pane has focus.
	cursor int
}

// refChoice is one selectable reference.
type refChoice struct {
	kind  domain.RefKind
	value string
	hash  string
}

// toPresetRef turns a choice into the reference recorded for installation.
//
// Freezing replaces the symbolic name with the commit it resolved to, which is
// what makes a later `bossy update` unable to move the dependency. Without a
// hash there is nothing to freeze to, so the symbolic name is kept rather than
// an empty value written — go-git would zero-pad that into a plausible but
// wrong commit.
func (c refChoice) toPresetRef(freeze bool) domain.PresetRef {
	if freeze && c.hash != "" {
		return domain.PresetRef{Kind: domain.RefKindCommit, Value: c.hash}
	}
	return domain.PresetRef{Kind: c.kind, Value: c.value}
}

// label renders a choice for the detail pane.
func (c refChoice) label() string {
	return string(c.kind) + " " + c.value
}

// Messages the ref-loading command produces.
type (
	refsLoadedMsg struct {
		presetID string
		refs     []refChoice
	}
	refsFailedMsg struct {
		presetID string
		err      error
	}
)

// rootModel is the whole picker state.
type rootModel struct {
	presets []domain.Preset
	// visible holds indices into presets, in display order.
	visible   []int
	cursor    int
	selected  map[string]Selection
	filter    string
	filtering bool
	focus     focusArea
	refs      map[string]refState
	lister    RefLister
	width     int
	height    int
	done      bool
	cancelled bool
}

// newModel builds the initial state with every preset visible.
func newModel(presets []domain.Preset, lister RefLister) rootModel {
	m := rootModel{
		presets:  presets,
		selected: map[string]Selection{},
		refs:     map[string]refState{},
		lister:   lister,
		width:    defaultWidth,
		height:   defaultHeight,
	}
	return m.refilter()
}

// Init satisfies tea.Model. Nothing is loaded up front: listing refs for a
// whole catalog would mean one network round trip per entry before the first
// frame, so listings are made lazily when a preset is opened.
func (m rootModel) Init() tea.Cmd { return nil }

// Update is a pure function of model and message. It performs no I/O, which is
// what lets the whole state machine be tested without a terminal.
func (m rootModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case refsLoadedMsg:
		return m.onRefsLoaded(msg), nil
	case refsFailedMsg:
		return m.onRefsFailed(msg), nil
	case tea.KeyMsg:
		return m.onKey(msg)
	default:
		return m, nil
	}
}

// onRefsLoaded stores a completed listing.
func (m rootModel) onRefsLoaded(msg refsLoadedMsg) rootModel {
	state := m.refs[msg.presetID]
	state.state = refsReady
	state.choices = msg.refs
	state.err = ""
	state.cursor = indexOfCurrentRef(msg.refs, m.selected[msg.presetID].Ref)
	m.refs[msg.presetID] = state
	return m
}

// onRefsFailed records a listing failure.
//
// The picker stays open and the selection keeps the catalog's default ref,
// flagged unverified. Falling back to a default branch here would reintroduce
// the silent wrong-branch bug; the user is told instead.
func (m rootModel) onRefsFailed(msg refsFailedMsg) rootModel {
	state := m.refs[msg.presetID]
	state.state = refsFailed
	state.choices = nil
	state.err = msg.err.Error()
	m.refs[msg.presetID] = state

	if sel, ok := m.selected[msg.presetID]; ok {
		sel.Unverified = true
		m.selected[msg.presetID] = sel
	}
	return m
}

// onKey routes a keystroke to the focused pane.
func (m rootModel) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		m.cancelled = true
		m.done = true
		return m, tea.Quit
	}
	if msg.Type == tea.KeyCtrlS {
		m.done = true
		return m, tea.Quit
	}

	if m.filtering {
		return m.onFilterKey(msg)
	}
	if m.focus == focusDetail {
		return m.onDetailKey(msg)
	}
	return m.onListKey(msg)
}

// onFilterKey handles typing in the filter box.
func (m rootModel) onFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	//nolint:exhaustive // only the editing keys matter here; the default ignores the rest
	switch msg.Type {
	case tea.KeyEsc:
		m.filtering = false
		m.filter = ""
		m = m.refilter()
	case tea.KeyEnter:
		// Keep the filter, leave the box.
		m.filtering = false
	case tea.KeyBackspace:
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m = m.refilter()
		}
	case tea.KeySpace:
		m.filter += " "
		m = m.refilter()
	case tea.KeyRunes:
		m.filter += string(msg.Runes)
		m = m.refilter()
	default:
		return m, nil
	}
	return m, nil
}

// onListKey handles the dependency list.
func (m rootModel) onListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyUp || msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.Type == tea.KeyDown || msg.String() == "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeySpace:
		m = m.toggleCurrent()
	case msg.Type == tea.KeyEnter:
		return m.openDetail()
	case msg.String() == "/":
		m.filtering = true
	case msg.String() == "a":
		m = m.selectAllVisible()
	case msg.String() == "n":
		m.selected = map[string]Selection{}
	case msg.String() == "q":
		m.cancelled = true
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// onDetailKey handles the reference pane.
func (m rootModel) onDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	preset, ok := m.current()
	if !ok {
		m.focus = focusList
		return m, nil
	}
	state := m.refs[preset.ID]

	switch {
	case msg.Type == tea.KeyEsc:
		m.focus = focusList
	case msg.Type == tea.KeyUp || msg.String() == "k":
		if state.cursor > 0 {
			state.cursor--
			m.refs[preset.ID] = state
		}
	case msg.Type == tea.KeyDown || msg.String() == "j":
		if state.cursor < len(state.choices)-1 {
			state.cursor++
			m.refs[preset.ID] = state
		}
	case msg.String() == "f":
		m = m.toggleFreeze(preset)
	case msg.Type == tea.KeyEnter:
		m = m.chooseCurrentRef(preset, state)
		m.focus = focusList
	}
	return m, nil
}

// openDetail focuses the reference pane, starting a listing if one is needed.
func (m rootModel) openDetail() (tea.Model, tea.Cmd) {
	preset, ok := m.current()
	if !ok {
		return m, nil
	}
	m.focus = focusDetail

	state := m.refs[preset.ID]
	// A previous failure is retried, but a completed listing is reused: the
	// cache is what keeps the picker from re-hitting the network on every
	// keystroke.
	if state.state == refsReady || state.state == refsLoading {
		return m, nil
	}

	state.state = refsLoading
	state.err = ""
	m.refs[preset.ID] = state

	return m, listRefsCmd(m.lister, preset)
}

// listRefsCmd runs a ref listing off the update loop.
func listRefsCmd(lister RefLister, preset domain.Preset) tea.Cmd {
	return func() tea.Msg {
		refs, err := lister(preset)
		if err != nil {
			return refsFailedMsg{presetID: preset.ID, err: err}
		}
		return refsLoadedMsg{presetID: preset.ID, refs: refChoicesFromRefs(refs)}
	}
}

// refChoicesFromRefs turns plumbing references into selectable choices,
// branches first and then tags, alphabetical within each group.
//
// Branches lead because pinning to a branch is the common case for the Delphi
// libraries this picker was built for; most of them publish no tags at all.
func refChoicesFromRefs(refs []*plumbing.Reference) []refChoice {
	choices := make([]refChoice, 0, len(refs))
	for _, ref := range refs {
		name := ref.Name()
		switch {
		case name.IsBranch():
			choices = append(choices, refChoice{
				kind: domain.RefKindBranch, value: name.Short(), hash: ref.Hash().String(),
			})
		case name.IsTag():
			choices = append(choices, refChoice{
				kind: domain.RefKindTag, value: name.Short(), hash: ref.Hash().String(),
			})
		}
	}

	sort.SliceStable(choices, func(i, j int) bool {
		if choices[i].kind != choices[j].kind {
			return choices[i].kind == domain.RefKindBranch
		}
		return choices[i].value < choices[j].value
	})
	return choices
}

// indexOfCurrentRef finds the choice matching an already-selected reference, so
// reopening the pane lands on what is currently pinned.
func indexOfCurrentRef(choices []refChoice, ref domain.PresetRef) int {
	for i, c := range choices {
		if c.kind == ref.Kind && c.value == ref.Value {
			return i
		}
	}
	return 0
}

// current returns the preset under the cursor.
func (m rootModel) current() (domain.Preset, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return domain.Preset{}, false
	}
	return m.presets[m.visible[m.cursor]], true
}

// toggleCurrent ticks or unticks the entry under the cursor.
func (m rootModel) toggleCurrent() rootModel {
	preset, ok := m.current()
	if !ok {
		return m
	}
	if _, selected := m.selected[preset.ID]; selected {
		delete(m.selected, preset.ID)
		return m
	}
	m.selected[preset.ID] = Selection{Preset: preset, Ref: preset.DefaultRef}
	return m
}

// selectAllVisible ticks every entry currently shown, which is how a tag filter
// plus "a" adds a whole baseline at once.
func (m rootModel) selectAllVisible() rootModel {
	for _, idx := range m.visible {
		preset := m.presets[idx]
		if _, selected := m.selected[preset.ID]; selected {
			continue
		}
		m.selected[preset.ID] = Selection{Preset: preset, Ref: preset.DefaultRef}
	}
	return m
}

// toggleFreeze flips the freeze flag, selecting the entry if needed.
func (m rootModel) toggleFreeze(preset domain.Preset) rootModel {
	sel, ok := m.selected[preset.ID]
	if !ok {
		sel = Selection{Preset: preset, Ref: preset.DefaultRef}
	}
	sel.Freeze = !sel.Freeze

	// Freezing a ref chosen from a real listing resolves it to that commit.
	if state := m.refs[preset.ID]; state.state == refsReady && state.cursor < len(state.choices) {
		sel.Ref = state.choices[state.cursor].toPresetRef(sel.Freeze)
	}
	m.selected[preset.ID] = sel
	return m
}

// chooseCurrentRef applies the highlighted reference to the selection.
//
// Editing a reference is a statement of intent to install, so it ticks the
// entry if it was not already ticked.
func (m rootModel) chooseCurrentRef(preset domain.Preset, state refState) rootModel {
	if state.state != refsReady || state.cursor >= len(state.choices) {
		return m
	}
	choice := state.choices[state.cursor]

	sel, ok := m.selected[preset.ID]
	if !ok {
		sel = Selection{Preset: preset}
	}
	sel.Ref = choice.toPresetRef(sel.Freeze)
	sel.Unverified = false
	m.selected[preset.ID] = sel
	return m
}

// refilter rebuilds the visible set and keeps the cursor inside it.
func (m rootModel) refilter() rootModel {
	needle := strings.ToLower(strings.TrimSpace(m.filter))
	m.visible = make([]int, 0, len(m.presets))

	for i, p := range m.presets {
		if needle == "" || presetMatches(p, needle) {
			m.visible = append(m.visible, i)
		}
	}

	// Narrowing the list can strand the cursor past the end, which would index
	// out of range on the next render.
	if m.cursor >= len(m.visible) {
		m.cursor = max(len(m.visible)-1, 0)
	}
	return m
}

// presetMatches reports whether a preset matches the filter. The repository and
// tags are searched as well as the id, so a library can be found by owner or by
// the tag it was grouped under.
func presetMatches(p domain.Preset, needle string) bool {
	if strings.Contains(strings.ToLower(p.ID), needle) ||
		strings.Contains(strings.ToLower(p.Name), needle) ||
		strings.Contains(strings.ToLower(p.Repo), needle) {
		return true
	}
	for _, tag := range p.Tags {
		if strings.Contains(strings.ToLower(tag), needle) {
			return true
		}
	}
	return false
}

// Selections returns what the user chose, sorted by preset id.
//
// A cancelled picker returns nothing, so a caller cannot mistake an abort for
// an empty confirmation.
func (m rootModel) Selections() []Selection {
	if m.cancelled {
		return nil
	}
	out := make([]Selection, 0, len(m.selected))
	for _, sel := range m.selected {
		out = append(out, sel)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Preset.ID) < strings.ToLower(out[j].Preset.ID)
	})
	return out
}

// visibleIDs is a test helper rendering the filtered set.
func (m rootModel) visibleIDs() []string {
	ids := make([]string, 0, len(m.visible))
	for _, idx := range m.visible {
		ids = append(ids, m.presets[idx].ID)
	}
	return ids
}
