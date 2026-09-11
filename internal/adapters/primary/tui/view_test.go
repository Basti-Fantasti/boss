//nolint:testpackage // exercises the unexported model directly
package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

// View is not covered by the state-machine tests, so it gets its own pass over
// every pane state. A render panic would only ever show up in a real terminal.
func TestView_RendersEveryState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		build func() rootModel
		want  string
	}{
		{
			name:  "empty catalog",
			build: func() rootModel { return newModel(nil, nopLister) },
			want:  "preset sync",
		},
		{
			name:  "list with a selection",
			build: func() rootModel { m, _ := send(newTestModel(), " "); return m },
			want:  "[x]",
		},
		{
			name:  "filter matching nothing",
			build: func() rootModel { m, _ := send(newTestModel(), "/", "z", "z", "z"); return m },
			want:  "Nothing matches",
		},
		{
			name:  "refs loading",
			build: func() rootModel { m, _ := send(newTestModel(), "enter"); return m },
			want:  "loading refs",
		},
		{
			name: "refs failed",
			build: func() rootModel {
				m, _ := send(newTestModel(), " ", "enter")
				next, _ := m.Update(refsFailedMsg{presetID: "delphimvcframework", err: errors.New("boom")})
				out, _ := next.(rootModel)
				return out
			},
			want: "could not list refs",
		},
		{
			name: "refs ready",
			build: func() rootModel {
				m, _ := send(newTestModel(), " ", "enter")
				next, _ := m.Update(refsLoadedMsg{presetID: "delphimvcframework", refs: []refChoice{
					{kind: domain.RefKindBranch, value: "main", hash: strings.Repeat("a", 40)},
					{kind: domain.RefKindTag, value: "v1.0.0", hash: strings.Repeat("b", 40)},
				}})
				out, _ := next.(rootModel)
				return out
			},
			want: "branches and tags",
		},
		{
			name: "remote advertises nothing",
			build: func() rootModel {
				m, _ := send(newTestModel(), "enter")
				next, _ := m.Update(refsLoadedMsg{presetID: "delphimvcframework"})
				out, _ := next.(rootModel)
				return out
			},
			want: "no branches or tags",
		},
		{
			name: "frozen commit is abbreviated",
			build: func() rootModel {
				m, _ := send(newTestModel(), " ", "enter")
				next, _ := m.Update(refsLoadedMsg{presetID: "delphimvcframework", refs: []refChoice{
					{kind: domain.RefKindBranch, value: "main", hash: strings.Repeat("a", 40)},
				}})
				out, _ := next.(rootModel)
				out, _ = send(out, "f", "enter")
				return out
			},
			want: "commit aaaaaaaaaaaa",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := c.build().View()
			if got == "" {
				t.Fatal("View rendered nothing")
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("View does not contain %q:\n%s", c.want, got)
			}
		})
	}
}

// A finished picker renders nothing, so the terminal is not left holding a
// stale frame under whatever the caller prints next.
func TestView_EmptyOnceDone(t *testing.T) {
	t.Parallel()

	m, _ := send(newTestModel(), "ctrl+s")
	if got := m.View(); got != "" {
		t.Errorf("View after ctrl+s = %q, want empty", got)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
		width    int
	}{
		{"short", "short", 10},
		{"exactly-10", "exactly-10", 10},
		{"far too long to fit", "far too l…", 10},
		{"x", "x", 0},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.width); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}
