package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// c on a selection writes a review comment on its lines yourself (a asks
// Claude instead): the footer takes the text, Enter posts it to the PR as a
// new thread, Esc drops it. The threads are fetched again, so it shows.

type commentedMsg struct {
	where, url, err string
}

func (m *model) startComment() {
	spans := m.selSpans()
	switch {
	case m.target.repo == "" || m.target.pr == 0:
		m.flash = "Comments go on a PR: open one from gh-dash or with --pr"
		return
	case m.spansBlocked(spans) != "":
		m.flash = m.spansBlocked(spans)
		return
	}
	sp := spans[0]
	f := m.files[sp.file]
	first, last := m.spanEnds(sp)
	where := fmt.Sprintf("%s:%d", f.Path(), sideNo(first))
	if sideNo(last) != sideNo(first) {
		where += fmt.Sprintf("–%d", sideNo(last))
	}
	t := m.target
	m.asking = &prompt{label: "Comment on " + where + ": ",
		note: func() string { return "  enter post · esc cancel" },
		done: func(body string) tea.Cmd {
			body = strings.TrimSpace(body)
			if body == "" {
				return nil
			}
			m.flash = "Posting the comment on " + where + "…"
			m.sel = selection{}
			return func() tea.Msg {
				_, url, err := newComment(t, f, first, last, body)
				return commentedMsg{where, url, err}
			}
		},
	}
}

func sideNo(l Line) int {
	_, n := ghSide(l)
	return n
}

func (m *model) gotComment(msg commentedMsg) tea.Cmd {
	if msg.err != "" {
		m.flash = "✗ Comment on " + msg.where + " failed: " + msg.err
		return nil
	}
	m.flash = "✓ Commented on " + msg.where
	return m.fetchThreads()
}
