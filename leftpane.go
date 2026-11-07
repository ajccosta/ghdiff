package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The column left of the diff shows one of these panes; Tab moves the keys
// from the diff through each available one and back, and when more than one
// is available a row of their names on top switches by click. A new pane is
// one more entry.
type leftPane struct {
	name  string
	avail func(m *model) bool
	view  func(m *model, w, h int) []string // h rows, w wide
	key   func(m *model, k string) tea.Cmd  // a key while it has the focus
	click func(m *model, row int) tea.Cmd   // a click on one of its rows
	wheel func(m *model, n int)
}

// Set in init: the panes' functions read the table themselves.
var leftPanes []leftPane

func init() {
	leftPanes = []leftPane{
		{
			name:  "Files",
			avail: func(*model) bool { return true },
			view:  (*model).treeLines,
			key:   (*model).treeKey,
			click: (*model).treeClick,
			wheel: (*model).moveTree,
		},
		{
			name:  "Commits", // the PR's, when gh-dash opened it (commits.go)
			avail: func(m *model) bool { return m.target.dash },
			view:  (*model).commitLines,
			key:   (*model).commitKey,
			click: (*model).commitClick,
			wheel: (*model).moveCommit,
		},
	}
}

func (m *model) leftPane() leftPane { return leftPanes[m.left] }

// availPanes are the indexes of the panes that can show now.
func (m *model) availPanes() []int {
	var out []int
	for i, p := range leftPanes {
		if p.avail(m) {
			out = append(out, i)
		}
	}
	return out
}

// leftHead is 1 when the column starts with the row of pane names.
func (m *model) leftHead() int {
	if len(m.availPanes()) > 1 {
		return 1
	}
	return 0
}

// nextFocus is Tab: from the diff to the first pane, through the others,
// back to the diff.
func (m *model) nextFocus() {
	ps := m.availPanes()
	if m.focus != focusTree {
		m.focus, m.left, m.treeSel = focusTree, ps[0], m.currentFile()
		return
	}
	for i, p := range ps {
		if p == m.left && i+1 < len(ps) {
			m.left = ps[i+1]
			return
		}
	}
	m.focus = focusDiff
}

// leftLines are the column's h rows: the pane names, then the pane.
func (m *model) leftLines(w, h int) []string {
	head := m.leftHead()
	out := m.leftPane().view(m, w, h-head)
	if head == 0 {
		return out
	}
	t := m.theme
	var names []string
	for _, i := range m.availPanes() {
		st := lipgloss.NewStyle().Foreground(t.Dim)
		if i == m.left {
			st = lipgloss.NewStyle().Bold(true).Foreground(t.Accent).Underline(true)
		}
		names = append(names, st.Render(leftPanes[i].name))
	}
	return append([]string{" " + strings.Join(names, "  ")}, out...)
}

// leftClick is a click on row y (0 at the top) of the column, at x.
func (m *model) leftClick(x, y int) tea.Cmd {
	if m.leftHead() == 0 {
		return m.leftPane().click(m, y)
	}
	if y > 0 {
		return m.leftPane().click(m, y-1)
	}
	at := 1 // the names start after a space, two apart
	for _, i := range m.availPanes() {
		n := ansi.StringWidth(leftPanes[i].name)
		if x >= at && x < at+n {
			m.left, m.focus = i, focusTree
			return nil
		}
		at += n + 2
	}
	return nil
}

// The file tree.

func (m *model) treeLines(w, h int) []string {
	out := make([]string, h)
	cur, tt := m.currentFile(), m.treeTop()
	for i := range out {
		if ti := tt + i; ti < len(m.tree) {
			out[i] = m.treeCell(m.tree[ti], w, cur)
		}
	}
	return out
}

func (m *model) treeKey(k string) tea.Cmd {
	switch k {
	case "up", "k":
		m.moveTree(-1)
	case "down", "j":
		m.moveTree(1)
	case "home", "g":
		m.moveTree(-len(m.files))
	case "end", "G":
		m.moveTree(len(m.files))
	case "right", "l", "enter":
		m.load(m.treeSel)
		m.jumpTo(m.treeSel)
		m.focus = focusDiff
	}
	return nil
}

func (m *model) treeClick(row int) tea.Cmd {
	if i := m.treeTop() + row; i < len(m.tree) && m.tree[i].file >= 0 {
		m.treeSel = m.tree[i].file
		m.load(m.treeSel)
		m.jumpTo(m.treeSel)
	}
	return nil
}

func (m *model) moveTree(d int) {
	m.treeSel = min(max(0, m.treeSel+d), len(m.files)-1)
	m.jumpTo(m.treeSel)
}

// treeTop is the first tree row on screen, chosen so the selection is visible.
func (m *model) treeTop() int {
	sel := 0
	for i, r := range m.tree {
		if r.file == m.treeSel {
			sel = i
		}
	}
	h := m.paneHeight() - m.leftHead()
	return min(max(0, sel-h/2), max(0, len(m.tree)-h))
}
