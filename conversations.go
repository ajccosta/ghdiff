package main

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The Conversations pane: every note on this diff (review threads from
// GitHub, whoever wrote them, and Claude conversations kept in ghdiff), in the
// diff's order, two rows each. Opening one goes to its lines and opens it as
// a click on its mark does.

// allNotes are the notes on this diff, by file and line.
func (m *model) allNotes() []note {
	var out []note
	for _, t := range m.threads {
		out = append(out, threadNote{t})
	}
	for _, c := range m.convos {
		if len(c.turns) > 0 {
			out = append(out, convoNote{c})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		fi, li := out[i].at()
		fj, lj := out[j].at()
		return fi < fj || fi == fj && li < lj
	})
	return out
}

func (m *model) noteLines(w, h int) []string {
	t := m.theme
	dim := lipgloss.NewStyle().Foreground(t.Dim)
	ns := m.allNotes()
	if len(ns) == 0 {
		s := "No conversations on this diff. Select lines (v) and press a to ask Claude about them."
		if m.target.pr > 0 {
			s = "No review comments or Claude conversations on this diff. Select lines (v) and press a to ask Claude, or c to comment."
		}
		return wrapRows(dim, s, w, h)
	}
	m.noteSel = min(m.noteSel, len(ns)-1)
	out := make([]string, h)
	top := m.noteTop()
	for i := 0; i+1 < h; i += 2 {
		e := top + i/2
		if e >= len(ns) {
			break
		}
		n := ns[e]
		fi, lo := n.at()
		text, when := n.summary()
		place, right := m.shortRange(fi, lo, lo), ago(when)
		if tn, ok := n.(threadNote); ok {
			place = m.shortRange(fi, tn.t.lo, tn.t.hi)
			if tn.t.resolved {
				right = "resolved · " + right
			}
		} else if cn, ok := n.(convoNote); ok {
			place = m.spansLabel(cn.c.spans)
		}
		st := lipgloss.NewStyle()
		if m.focus == focusTree && e == m.noteSel {
			st = st.Reverse(true)
		}
		head := ansi.Truncate(" "+n.mark(m)+" "+place, max(1, w-ansi.StringWidth(right)-1), "…")
		pad := max(1, w-ansi.StringWidth(head)-ansi.StringWidth(right))
		out[i] = st.Render(head + strings.Repeat(" ", pad) + dim.Render(right))
		out[i+1] = st.Render(ansi.Truncate("   "+dim.Render(text), w, "…"))
	}
	if n := len(m.unplaced); n > 0 && len(ns)-top <= (h-1)/2 {
		out[h-1] = dim.Render(ansi.Truncate(fmt.Sprintf(" %d more Claude %s not on this diff", n,
			plural(n, "conversation", "conversations")), w, "…"))
	}
	return out
}

// noteTop is the first entry on screen, chosen so the cursor is visible.
func (m *model) noteTop() int {
	per := max(1, (m.paneHeight()-m.leftHead())/2)
	return min(max(0, m.noteSel-per/2), max(0, len(m.allNotes())-per))
}

func (m *model) moveNote(d int) {
	m.noteSel = min(max(0, m.noteSel+d), max(0, len(m.allNotes())-1))
}

func (m *model) noteKey(k string) tea.Cmd {
	switch k {
	case "up", "k":
		m.moveNote(-1)
	case "down", "j":
		m.moveNote(1)
	case "home", "g":
		m.moveNote(-len(m.allNotes()))
	case "end", "G":
		m.moveNote(len(m.allNotes()))
	case "right", "l", "enter":
		m.openNoteAt(m.noteSel)
	}
	return nil
}

func (m *model) noteClick(row int) tea.Cmd {
	e := m.noteTop() + row/2
	if e >= len(m.allNotes()) {
		return nil
	}
	m.noteSel, m.focus = e, focusTree
	m.openNoteAt(e)
	return nil
}

// openNoteAt opens entry e on its lines, opening its file if collapsed.
func (m *model) openNoteAt(e int) {
	ns := m.allNotes()
	if e < 0 || e >= len(ns) {
		return
	}
	fi, lo := ns[e].at()
	if m.collapsed[fi] {
		delete(m.collapsed, fi)
		m.relayout()
	}
	m.openNote(ns[e], m.notesAt(fi, lo))
}
