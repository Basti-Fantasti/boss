//nolint:testpackage // exercises the unexported model directly
package tui

import (
	"errors"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-git/go-git/v5/plumbing"
)

func testPresets() []domain.Preset {
	return []domain.Preset{
		{
			ID:         "delphimvcframework",
			Repo:       "github.com/danieleteti/delphimvcframework",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindTag, Value: "3.4.2-magnesium"},
		},
		{
			ID:         "delphi-neon",
			Repo:       "github.com/paolo-rossi/delphi-neon",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault},
		},
		{
			ID:         "spring4d",
			Repo:       "bitbucket.org/sglienke/spring4d",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindBranch, Value: "master"},
		},
		{
			ID:         "zeoslib",
			Repo:       "github.com/marsupilami79/zeoslib",
			DefaultRef: domain.PresetRef{Kind: domain.RefKindBranch, Value: "8.0-patches"},
		},
	}
}

// nopLister stands in for the network. Update must never call it directly; it
// is only ever reached through a returned tea.Cmd.
func nopLister(domain.Preset) ([]*plumbing.Reference, error) { return nil, nil }

func newTestModel() rootModel {
	return newModel(testPresets(), nopLister)
}

// key builds the tea.KeyMsg a keystroke produces.
func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// send applies a sequence of keystrokes and returns the resulting model plus
// the command from the final keystroke.
func send(m rootModel, keys ...string) (rootModel, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(key(k))
		m, _ = next.(rootModel)
	}
	return m, cmd
}

func TestUpdate_SpaceTogglesSelection(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), " ")
	if len(m.selected) != 1 {
		t.Fatalf("after one space, %d entries are selected", len(m.selected))
	}
	if _, ok := m.selected["delphimvcframework"]; !ok {
		t.Errorf("selected = %v, want the entry under the cursor", m.selected)
	}

	m, _ = send(m, " ")
	if len(m.selected) != 0 {
		t.Errorf("space did not deselect: %v", m.selected)
	}
}

// A selection carries the preset's catalog default until the user changes it,
// so ticking an entry and confirming immediately installs what the catalog says.
func TestUpdate_SelectionStartsAtTheCatalogDefault(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), " ")

	sel := m.selected["delphimvcframework"]
	if sel.Ref.Kind != domain.RefKindTag || sel.Ref.Value != "3.4.2-magnesium" {
		t.Errorf("Ref = %+v, want the catalog default", sel.Ref)
	}
}

func TestUpdate_CursorMovesAndClamps(t *testing.T) {
	t.Parallel()

	m := newTestModel()

	m, _ = send(m, "up")
	if m.cursor != 0 {
		t.Errorf("cursor moved above the first entry to %d", m.cursor)
	}

	m, _ = send(m, "down", "down", "down", "down", "down")
	if want := len(testPresets()) - 1; m.cursor != want {
		t.Errorf("cursor = %d, want it clamped to %d", m.cursor, want)
	}
}

func TestUpdate_FilterNarrowsList(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "/", "d", "e", "l")

	if !m.filtering {
		t.Fatal("model is not in filter mode")
	}
	if m.filter != "del" {
		t.Fatalf("filter = %q", m.filter)
	}
	if len(m.visible) != 2 {
		t.Fatalf("filter matched %d entries, want the two delphi* ones: %v", len(m.visible), m.visibleIDs())
	}

	// Filtering matches the repository too, so a library can be found by owner.
	m, _ = send(newTestModel(), "/", "m", "a", "r", "s")
	if len(m.visible) != 1 || m.presets[m.visible[0]].ID != "zeoslib" {
		t.Errorf("repository match failed: %v", m.visibleIDs())
	}
}

// Narrowing the list with the cursor low down must not leave it pointing past
// the end, which would index out of range on the next render.
func TestUpdate_FilterKeepsCursorInRange(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "down", "down", "down")
	if m.cursor != 3 {
		t.Fatalf("setup: cursor = %d, want 3", m.cursor)
	}

	m, _ = send(m, "/", "s", "p", "r")
	if len(m.visible) != 1 {
		t.Fatalf("filter matched %d entries, want 1", len(m.visible))
	}
	if m.cursor >= len(m.visible) {
		t.Fatalf("cursor = %d, out of range for %d visible entries", m.cursor, len(m.visible))
	}
}

func TestUpdate_BackspaceWidensFilter(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "/", "d", "e", "l", "backspace")
	if m.filter != "de" {
		t.Errorf("filter = %q, want %q", m.filter, "de")
	}
}

// Escape leaves filter mode and clears it, so the full catalog is back.
func TestUpdate_EscapeClearsFilter(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "/", "d", "e", "l", "esc")
	if m.filtering {
		t.Error("still in filter mode")
	}
	if m.filter != "" {
		t.Errorf("filter = %q, want it cleared", m.filter)
	}
	if len(m.visible) != len(testPresets()) {
		t.Errorf("%d entries visible, want all %d", len(m.visible), len(testPresets()))
	}
}

// Selecting while filtered must tick the entry the cursor is actually on, not
// the one at that index in the unfiltered catalog.
func TestUpdate_SelectionFollowsTheFilteredCursor(t *testing.T) {
	t.Parallel()

	// Enter leaves the filter box while keeping the filter; escape would clear
	// it. Space is a filter character while the box has focus, because names
	// like "DMVC Framework" contain one.
	m, _ := send(newTestModel(), "/", "z", "e", "o", "enter", " ")

	if _, ok := m.selected["zeoslib"]; !ok {
		t.Errorf("selected = %v, want zeoslib", m.selected)
	}
	if len(m.selected) != 1 {
		t.Errorf("selected %d entries, want only the filtered one: %v", len(m.selected), m.selected)
	}
}

// Space inside the filter box types a space rather than toggling, because the
// filter also matches display names.
func TestUpdate_SpaceIsAFilterCharacter(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "/", "a", " ", "b")
	if m.filter != "a b" {
		t.Errorf("filter = %q, want %q", m.filter, "a b")
	}
	if len(m.selected) != 0 {
		t.Errorf("space toggled a selection while filtering: %v", m.selected)
	}
}

func TestUpdate_EnterRequestsRefsOnce(t *testing.T) {
	t.Parallel()

	m, cmd := send(newTestModel(), "enter")
	if m.focus != focusDetail {
		t.Fatal("enter did not move focus to the detail pane")
	}
	if cmd == nil {
		t.Fatal("enter issued no ref-loading command")
	}
	if m.refs["delphimvcframework"].state != refsLoading {
		t.Errorf("ref state = %v, want loading", m.refs["delphimvcframework"].state)
	}

	// Land the result, go back, and re-enter: the cached refs must be reused.
	next, _ := m.Update(refsLoadedMsg{presetID: "delphimvcframework", refs: []refChoice{
		{kind: domain.RefKindBranch, value: "main"},
	}})
	m, _ = next.(rootModel)

	m, _ = send(m, "esc")
	if m.focus != focusList {
		t.Fatal("escape did not return focus to the list")
	}

	m, cmd = send(m, "enter")
	if cmd != nil {
		t.Error("re-entering an already-loaded preset issued another listing command")
	}
	if m.refs["delphimvcframework"].state != refsReady {
		t.Errorf("ref state = %v, want ready", m.refs["delphimvcframework"].state)
	}
}

// A failed listing must not tear down the picker, and above all must not
// quietly substitute a default branch: that is the silent wrong-branch bug.
func TestUpdate_RefsFailedKeepsDefaultAndFlagsUnverified(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), " ", "enter")

	next, cmd := m.Update(refsFailedMsg{presetID: "delphimvcframework", err: errors.New("host unreachable")})
	m, _ = next.(rootModel)

	if cmd != nil {
		t.Errorf("a listing failure produced command %v; it must not quit", cmd)
	}
	if m.done || m.cancelled {
		t.Fatal("a listing failure ended the picker")
	}

	state := m.refs["delphimvcframework"]
	if state.state != refsFailed {
		t.Errorf("ref state = %v, want failed", state.state)
	}
	if state.err == "" {
		t.Error("the failure was not recorded for display")
	}

	sel := m.selected["delphimvcframework"]
	if sel.Ref.Kind != domain.RefKindTag || sel.Ref.Value != "3.4.2-magnesium" {
		t.Errorf("Ref = %+v, want the catalog default to be left alone", sel.Ref)
	}
	if !sel.Unverified {
		t.Error("the selection was not flagged unverified")
	}
}

func TestUpdate_ChoosingARefUpdatesTheSelection(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), " ", "enter")

	next, _ := m.Update(refsLoadedMsg{presetID: "delphimvcframework", refs: []refChoice{
		{kind: domain.RefKindBranch, value: "main"},
		{kind: domain.RefKindBranch, value: "develop"},
		{kind: domain.RefKindTag, value: "3.4.2-magnesium"},
	}})
	m, _ = next.(rootModel)

	// The pane opens on whatever is currently pinned rather than at the top, so
	// reopening it does not silently point at a different ref.
	if m.refs["delphimvcframework"].cursor != 2 {
		t.Fatalf("ref cursor = %d, want it on the pinned tag at index 2", m.refs["delphimvcframework"].cursor)
	}

	// Step up onto develop and take it.
	m, _ = send(m, "up", "enter")

	sel := m.selected["delphimvcframework"]
	if sel.Ref.Kind != domain.RefKindBranch || sel.Ref.Value != "develop" {
		t.Errorf("Ref = %+v, want branch develop", sel.Ref)
	}
	if sel.Unverified {
		t.Error("a ref chosen from a real listing must not be flagged unverified")
	}
}

// Choosing a ref for an unticked entry ticks it: editing a ref is a statement
// of intent to install.
func TestUpdate_ChoosingARefSelectsTheEntry(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "enter")

	next, _ := m.Update(refsLoadedMsg{presetID: "delphimvcframework", refs: []refChoice{
		{kind: domain.RefKindBranch, value: "main"},
	}})
	m, _ = next.(rootModel)
	m, _ = send(m, "enter")

	if _, ok := m.selected["delphimvcframework"]; !ok {
		t.Error("choosing a ref did not select the entry")
	}
}

func TestUpdate_FreezeTogglesOnTheSelection(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), " ", "enter", "f")

	if !m.selected["delphimvcframework"].Freeze {
		t.Error("f did not turn freeze on")
	}

	m, _ = send(m, "f")
	if m.selected["delphimvcframework"].Freeze {
		t.Error("f did not turn freeze off again")
	}
}

func TestUpdate_CtrlSProducesSelections(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), " ", "down", " ")
	if len(m.selected) != 2 {
		t.Fatalf("setup selected %d entries, want 2", len(m.selected))
	}

	m, cmd := send(m, "ctrl+s")
	if !m.done {
		t.Fatal("ctrl+s did not finish the picker")
	}
	if cmd == nil {
		t.Error("ctrl+s did not quit")
	}

	got := m.Selections()
	if len(got) != 2 {
		t.Fatalf("Selections returned %d entries, want 2", len(got))
	}
	// Sorted by id so the written manifest is stable between runs.
	if got[0].Preset.ID != "delphi-neon" || got[1].Preset.ID != "delphimvcframework" {
		t.Errorf("Selections = %q, %q; want them sorted by id", got[0].Preset.ID, got[1].Preset.ID)
	}
}

// Confirming with nothing ticked is a no-op, not an error, and must not be
// mistaken for a cancellation by the caller.
func TestUpdate_CtrlSWithNoSelectionFinishesEmpty(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "ctrl+s")
	if !m.done {
		t.Fatal("ctrl+s did not finish")
	}
	if m.cancelled {
		t.Error("an empty confirmation was recorded as a cancellation")
	}
	if len(m.Selections()) != 0 {
		t.Error("Selections is not empty")
	}
}

func TestUpdate_CtrlCCancels(t *testing.T) {
	t.Parallel()

	m, cmd := send(newTestModel(), " ", "ctrl+c")
	if !m.cancelled {
		t.Fatal("ctrl+c did not cancel")
	}
	if cmd == nil {
		t.Error("ctrl+c did not quit")
	}
	if len(m.Selections()) != 0 {
		t.Error("a cancelled picker returned selections")
	}
}

// q quits from the list, but must be typable inside the filter box.
func TestUpdate_QQuitsOnlyOutsideTheFilter(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "q")
	if !m.cancelled {
		t.Error("q did not cancel from the list")
	}

	m, _ = send(newTestModel(), "/", "q")
	if m.cancelled {
		t.Error("q cancelled while typing a filter")
	}
	if m.filter != "q" {
		t.Errorf("filter = %q, want %q", m.filter, "q")
	}
}

func TestUpdate_EmptyCatalogIsNavigable(t *testing.T) {
	t.Parallel()

	m := newModel(nil, nopLister)

	m, cmd := send(m, "down", " ", "enter")
	if cmd != nil {
		t.Errorf("an empty catalog issued command %v", cmd)
	}
	if len(m.selected) != 0 {
		t.Error("something was selected from an empty catalog")
	}
}

func TestRefChoice_ToPresetRef(t *testing.T) {
	t.Parallel()

	const sha = "0123456789abcdef0123456789abcdef01234567"

	cases := []struct {
		name   string
		choice refChoice
		want   domain.PresetRef
		freeze bool
	}{
		{
			name:   "branch unfrozen keeps the symbolic name",
			choice: refChoice{kind: domain.RefKindBranch, value: "main", hash: sha},
			want:   domain.PresetRef{Kind: domain.RefKindBranch, Value: "main"},
		},
		{
			name:   "branch frozen becomes the resolved commit",
			choice: refChoice{kind: domain.RefKindBranch, value: "main", hash: sha},
			want:   domain.PresetRef{Kind: domain.RefKindCommit, Value: sha},
			freeze: true,
		},
		{
			name:   "tag frozen becomes the resolved commit",
			choice: refChoice{kind: domain.RefKindTag, value: "v1.0.0", hash: sha},
			want:   domain.PresetRef{Kind: domain.RefKindCommit, Value: sha},
			freeze: true,
		},
		{
			// Nothing to freeze to: the listing gave no hash, so the symbolic
			// name is kept rather than an empty or padded SHA written.
			name:   "frozen without a hash keeps the symbolic name",
			choice: refChoice{kind: domain.RefKindBranch, value: "main"},
			want:   domain.PresetRef{Kind: domain.RefKindBranch, Value: "main"},
			freeze: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.choice.toPresetRef(c.freeze); got != c.want {
				t.Errorf("toPresetRef(%v) = %+v, want %+v", c.freeze, got, c.want)
			}
		})
	}
}

func TestRefChoicesFromRefs(t *testing.T) {
	t.Parallel()

	refs := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/main", "1111111111111111111111111111111111111111"),
		plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "2222222222222222222222222222222222222222"),
		plumbing.NewReferenceFromStrings("refs/heads/develop", "3333333333333333333333333333333333333333"),
		plumbing.NewReferenceFromStrings("refs/tags/v2.0.0", "4444444444444444444444444444444444444444"),
	}

	got := refChoicesFromRefs(refs)
	if len(got) != 4 {
		t.Fatalf("got %d choices, want 4", len(got))
	}

	// Branches first, then tags; alphabetical inside each group. Branches lead
	// because a project pinning to a branch is the common case here.
	want := []struct {
		kind  domain.RefKind
		value string
	}{
		{domain.RefKindBranch, "develop"},
		{domain.RefKindBranch, "main"},
		{domain.RefKindTag, "v1.0.0"},
		{domain.RefKindTag, "v2.0.0"},
	}
	for i, w := range want {
		if got[i].kind != w.kind || got[i].value != w.value {
			t.Errorf("choices[%d] = %s %s, want %s %s", i, got[i].kind, got[i].value, w.kind, w.value)
		}
	}
}
