package main

import (
	"reflect"
	"strings"

	"charm.land/lipgloss/v2"
)

// A note is something kept on a line that the hover box shows: a review
// thread from GitHub (◆) or a Claude conversation kept in ghdiff (●, never on
// GitHub unless posted with P). A new kind is one more type with these methods.
type note interface {
	at() (file, lo int)            // the line its mark is on
	title(m *model) string         // the box's heading for it
	body(m *model, w int) []string // what it says, w columns wide
	open(m *model, same []note)    // open it; same are the box's notes of its kind
}

// notesAt are the notes whose mark is on line li of file fi.
func (m *model) notesAt(fi, li int) []note {
	var out []note
	for _, t := range m.startsThread(fi, li) {
		out = append(out, threadNote{t})
	}
	for _, c := range m.convos {
		if s := c.spans[0]; s.file == fi && s.lo == li && len(c.turns) > 0 {
			out = append(out, convoNote{c})
		}
	}
	return out
}

type threadNote struct{ t *thread }

func (n threadNote) at() (int, int) { return n.t.file, n.t.lo }

func (n threadNote) title(m *model) string {
	s := "GitHub · " + m.shortRange(n.t.file, n.t.lo, n.t.hi)
	if n.t.resolved {
		s += " · resolved"
	}
	return s
}

func (n threadNote) body(m *model, w int) []string { return m.threadBody(n.t, w) }

func (n threadNote) open(m *model, same []note) {
	var ts []*thread
	for _, o := range same {
		ts = append(ts, o.(threadNote).t)
	}
	m.openThreads(ts)
	m.showThread(n.t)
}

type convoNote struct{ c *convo }

func (n convoNote) at() (int, int) { return n.c.spans[0].file, n.c.spans[0].lo }

func (n convoNote) title(m *model) string {
	s := "Claude · " + m.spansLabel(n.c.spans) + " · " + n.c.created.Local().Format("Jan 2 15:04")
	if n.c.commentID != 0 {
		s += " · posted"
	}
	return s
}

func (n convoNote) body(m *model, w int) []string {
	bold := lipgloss.NewStyle().Bold(true)
	wrap := lipgloss.NewStyle().Width(w)
	var out []string
	for i := range n.c.turns {
		tn := &n.c.turns[i]
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, strings.Split(wrap.Render(bold.Render("You: ")+tn.q), "\n")...)
		if m.raw || tn.err {
			out = append(out, strings.Split(wrap.Render(tn.a), "\n")...)
		} else {
			out = append(out, strings.Split(m.markdown(tn, w), "\n")...)
		}
	}
	return out
}

func (n convoNote) open(m *model, same []note) {
	var cs []*convo
	for _, o := range same {
		cs = append(cs, o.(convoNote).c)
	}
	m.openChats(cs)
	m.showChat(n.c)
}

// openNote opens n, with the other notes of its kind in ns to switch to.
func (m *model) openNote(n note, ns []note) {
	var same []note
	for _, o := range ns {
		if sameKind(o, n) {
			same = append(same, o)
		}
	}
	m.focus = focusDiff
	n.open(m, same)
}

func sameKind(a, b note) bool { return reflect.TypeOf(a) == reflect.TypeOf(b) }
