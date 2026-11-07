package main

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSearch_FindsAndSteps(t *testing.T) {
	dir, raw := gitDiff(t)
	m := newModel(Parse(bytes.NewReader(raw)), "t", raw, target{repoPath: dir})
	m.width, m.height = 100, 12
	m.relayout()
	m.key("/")
	for _, r := range "line 2" {
		m.promptKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if len(m.search.hits) == 0 {
		t.Fatal("no matches")
	}
	m.promptKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	first := m.search.cur
	m.key("n")
	if m.search.cur == first && len(m.search.hits) > 1 {
		t.Error("n did not move to the next match")
	}
	for _, h := range m.search.hits {
		if h.from < m.rows[h.row].col {
			t.Errorf("matched in the gutter: %+v", h)
		}
	}
	m.key("esc")
	if m.search.query != "" {
		t.Error("esc did not clear the search")
	}
}
