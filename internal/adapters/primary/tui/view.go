package tui

import (
	"fmt"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/charmbracelet/lipgloss"
)

// Fallback geometry, used until the terminal reports its size.
const (
	defaultWidth  = 96
	defaultHeight = 30
	// listRows is how many dependencies are shown at once; the rest scroll.
	listRows = 12
	// idColumn keeps the ref column aligned across rows.
	idColumn = 26
)

// Styles. Colours are set by name so they adapt to the terminal's palette
// rather than fighting a user's theme.
//
//nolint:gochecknoglobals // immutable style table
var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	cursorStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	errorStyle    = lipgloss.NewStyle().Bold(true)
	helpStyle     = lipgloss.NewStyle().Faint(true)
)

// View renders the picker.
func (m rootModel) View() string {
	if m.done {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")
	b.WriteString(m.renderList())
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(m.width, defaultWidth)))
	b.WriteString("\n")
	b.WriteString(m.renderDetail())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(m.renderHelp()))
	return b.String()
}

func (m rootModel) renderHeader() string {
	title := titleStyle.Render("bossy add — select dependencies")

	filter := "/ " + m.filter
	if m.filtering {
		filter += "▊"
	} else if m.filter == "" {
		filter = dimStyle.Render("/ filter")
	}

	count := fmt.Sprintf("%d of %d shown · %d selected", len(m.visible), len(m.presets), len(m.selected))
	return title + "\n" + filter + "   " + dimStyle.Render(count)
}

func (m rootModel) renderList() string {
	if len(m.visible) == 0 {
		if len(m.presets) == 0 {
			return dimStyle.Render("  No presets available. Run 'bossy preset sync' or 'bossy preset add'.")
		}
		return dimStyle.Render("  Nothing matches the filter.")
	}

	start := m.scrollStart()
	end := min(start+listRows, len(m.visible))

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(m.renderRow(i))
		b.WriteString("\n")
	}
	if end < len(m.visible) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  … %d more", len(m.visible)-end)))
		b.WriteString("\n")
	}
	return b.String()
}

// scrollStart keeps the cursor inside the visible window.
func (m rootModel) scrollStart() int {
	if m.cursor < listRows {
		return 0
	}
	return min(m.cursor-listRows+1, max(len(m.visible)-listRows, 0))
}

func (m rootModel) renderRow(i int) string {
	preset := m.presets[m.visible[i]]
	sel, selected := m.selected[preset.ID]

	pointer := "  "
	if i == m.cursor && m.focus == focusList {
		pointer = cursorStyle.Render("▸ ")
	}

	box := "[ ]"
	if selected {
		box = selectedStyle.Render("[x]")
	}

	ref := preset.DefaultRef
	if selected {
		ref = sel.Ref
	}

	row := fmt.Sprintf("%s%s %-*s %s", pointer, box, idColumn, truncate(preset.ID, idColumn), refLabel(ref))

	switch {
	case selected && sel.Unverified:
		row += errorStyle.Render("  ⚠ unverified")
	case selected && sel.Freeze:
		row += dimStyle.Render("  frozen")
	case m.refs[preset.ID].state == refsLoading:
		row += dimStyle.Render("  …")
	}
	return row
}

// refLabel renders a reference compactly, abbreviating a commit so a 40-digit
// SHA does not push the row past the terminal width.
func refLabel(ref domain.PresetRef) string {
	if ref.Kind == domain.RefKindCommit && len(ref.Value) > 12 {
		return "commit " + ref.Value[:12]
	}
	return ref.String()
}

func (m rootModel) renderDetail() string {
	preset, ok := m.current()
	if !ok {
		return dimStyle.Render("  —")
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(preset.DisplayName()))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(preset.Repo))
	b.WriteString("\n")
	if preset.Description != "" {
		b.WriteString(dimStyle.Render(truncate(preset.Description, m.width-2)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.renderRefPane(preset))

	if len(preset.SearchPaths) > 0 {
		b.WriteString("\npaths  ")
		b.WriteString(dimStyle.Render(truncate(strings.Join(preset.SearchPaths, ", "), m.width-9)))
	}
	return b.String()
}

func (m rootModel) renderRefPane(preset domain.Preset) string {
	state := m.refs[preset.ID]
	sel := m.selected[preset.ID]

	freeze := "[ ]"
	if sel.Freeze {
		freeze = "[x]"
	}
	footer := fmt.Sprintf("pin    %s freeze resolved commit into bossy.json  (f)", freeze)

	switch state.state {
	case refsIdle:
		return dimStyle.Render("ref    press enter to list branches and tags") + "\n" + footer
	case refsLoading:
		return dimStyle.Render("ref    loading refs…") + "\n" + footer
	case refsFailed:
		// The catalog default is kept deliberately. Substituting a default
		// branch is the silent wrong-branch failure this project already fixed
		// once, so the user is told instead.
		return errorStyle.Render("ref    could not list refs: "+truncate(state.err, m.width-24)) + "\n" +
			dimStyle.Render("       keeping the catalog default "+preset.DefaultRef.String()+", unverified") + "\n" +
			footer
	case refsReady:
		return m.renderRefChoices(state) + "\n" + footer
	default:
		return footer
	}
}

func (m rootModel) renderRefChoices(state refState) string {
	if len(state.choices) == 0 {
		return dimStyle.Render("ref    the remote advertises no branches or tags")
	}

	const window = 5
	start := 0
	if state.cursor >= window {
		start = min(state.cursor-window+1, max(len(state.choices)-window, 0))
	}
	end := min(start+window, len(state.choices))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("ref    %d branches and tags\n", len(state.choices)))
	for i := start; i < end; i++ {
		marker := "( )"
		if i == state.cursor {
			marker = cursorStyle.Render("(•)")
		}
		b.WriteString("       " + marker + " " + state.choices[i].label() + "\n")
	}
	if end < len(state.choices) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("       … %d more\n", len(state.choices)-end)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m rootModel) renderHelp() string {
	if m.filtering {
		return "type to filter · enter keep · esc clear"
	}
	if m.focus == focusDetail {
		return "↑↓ choose ref · enter apply · f freeze · esc back · ^s write · ^c cancel"
	}
	return "space toggle · enter edit ref · / filter · a all · n none · ^s write · ^c cancel"
}

// truncate shortens a string to width, marking the cut.
func truncate(s string, width int) string {
	if width <= 1 || len(s) <= width {
		return s
	}
	return s[:width-1] + "…"
}
