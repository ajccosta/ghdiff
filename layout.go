package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Pane sizes set with the mouse (dragging a border) or + - { }, remembered
// in settings.json; 0 means the default size.
type sizes struct {
	TreeW int `json:"treeWidth,omitempty"`
	PaneW int `json:"paneWidth,omitempty"`  // the Claude pane beside the diff
	PaneH int `json:"paneHeight,omitempty"` // the Claude pane below the diff
}

// drag is what a held mouse button is moving.
type drag int

const (
	dragNone drag = iota
	dragTree
	dragPaneW
	dragPaneH
	dragSel
)

// sizeKey resizes with + - (the Claude pane) and { } (the tree).
func (m *model) sizeKey(k string) bool {
	switch k {
	case "+", "=", "-", "_":
		if !m.paneOpen() {
			m.flash = "+ - resize the Claude pane, once one is open"
			return true
		}
		d := 4
		if k == "-" || k == "_" {
			d = -4
		}
		if m.sideChat() {
			m.PaneW = m.chatWidth() + d
		} else {
			m.PaneH = m.chatHeight() + d/2
		}
	case "{", "}":
		if m.treeWidth() == 0 {
			return true
		}
		d := 4
		if k == "{" {
			d = -4
		}
		m.TreeW = m.treeWidth() + d
	default:
		return false
	}
	m.saveSettings()
	m.relayout()
	return true
}

// diffRow is the diff row at screen row y, or -1 (the title bar, the sticky
// file header, or past the end).
func (m *model) diffRow(y int) int {
	if y < 1 || y > m.paneHeight() {
		return -1
	}
	if y == 1 && len(m.starts) > 0 {
		cur := m.currentFile()
		if m.off > m.starts[cur] && !m.collapsed[cur] {
			return -1
		}
	}
	if r := m.off + y - 1; r < len(m.rows) {
		return r
	}
	return -1
}

// inPane: the screen cell is inside the Claude/thread pane.
func (m *model) inPane(x, y int) bool {
	if !m.paneOpen() {
		return false
	}
	if cw := m.chatWidth(); cw > 0 {
		return x > m.width-cw-1 && y >= 1 && y <= m.paneHeight()
	}
	return y > m.paneHeight() && y < m.height-1
}

func (m *model) mouseClick(msg tea.MouseClickMsg) {
	mo := msg.Mouse()
	if mo.Button != tea.MouseLeft || m.help {
		m.help = false
		return
	}
	tw, cw := m.treeWidth(), m.chatWidth()
	switch {
	case tw > 0 && mo.X == tw && mo.Y >= 1 && mo.Y <= m.paneHeight():
		m.drag = dragTree
	case cw > 0 && mo.X == m.width-cw-1 && mo.Y >= 1 && mo.Y <= m.paneHeight():
		m.drag = dragPaneW
	case m.paneOpen() && cw == 0 && mo.Y == m.paneHeight()+1:
		m.drag = dragPaneH
	case m.inPane(mo.X, mo.Y):
		if m.away {
			m.backToPane()
		}
	case mo.X < tw:
		if i := m.treeTop() + mo.Y - 1; mo.Y >= 1 && i < len(m.tree) && m.tree[i].file >= 0 {
			m.treeSel = m.tree[i].file
			m.jumpTo(m.treeSel)
		}
	default:
		if fi := m.headerAt(mo.Y); fi >= 0 {
			m.clickHeader(fi, mo.X)
			return
		}
		r := m.diffRow(mo.Y)
		if r < 0 || !m.codeRow(r) || m.consent || m.confirmPost {
			return
		}
		if m.paneOpen() && !m.away {
			m.switchFocus() // clicking the diff is like ctrl+o
		}
		m.focus = focusDiff
		m.sel = selection{on: true, anchored: true, file: m.rows[r].file, anchor: m.rows[r].line, cursor: m.rows[r].line}
		m.drag = dragSel
	}
}

// headerAt is the file whose header is at screen row y (the sticky one at
// the top counts), or -1.
func (m *model) headerAt(y int) int {
	if y < 1 || y > m.paneHeight() || len(m.starts) == 0 {
		return -1
	}
	cur := m.currentFile()
	if y == 1 && m.off > m.starts[cur] && !m.collapsed[cur] {
		return cur
	}
	if r := m.off + y - 1; r < len(m.rows) && m.rows[r].header {
		return m.rows[r].file
	}
	return -1
}

// clickHeader: the arrow collapses or expands the file; anywhere else on the
// header makes it the current file, in the diff and the tree.
func (m *model) clickHeader(fi, x int) {
	x0 := 0
	if tw := m.treeWidth(); tw > 0 {
		x0 = tw + 1
	}
	if x >= x0 && x <= x0+2 {
		m.collapsed[fi] = !m.collapsed[fi]
		m.relayout()
	}
	m.jumpTo(fi)
	m.treeSel = fi
}

func (m *model) mouseMotion(msg tea.MouseMotionMsg) {
	mo := msg.Mouse()
	switch m.drag {
	case dragTree:
		m.TreeW = mo.X
	case dragPaneW:
		m.PaneW = m.width - mo.X - 1
	case dragPaneH:
		m.PaneH = m.height - 1 - mo.Y
	case dragSel:
		// Extend to the line under the pointer, within the hunk.
		r := m.diffRow(mo.Y)
		if mo.Y < 1 {
			m.scroll(-1)
			r = m.diffRow(1)
		} else if mo.Y > m.paneHeight() {
			m.scroll(1)
			r = m.diffRow(m.paneHeight())
		}
		if r < 0 || m.rows[r].file != m.sel.file || !m.codeRow(r) {
			return
		}
		to, lines := m.rows[r].line, m.files[m.sel.file].Lines
		step := 1
		if to < m.sel.anchor {
			step = -1
		}
		for i := m.sel.anchor; i != to+step; i += step {
			if lines[i].Kind == Hunk {
				return
			}
			if selectable(lines[i]) {
				m.sel.cursor = i
			}
		}
		return
	default:
		return
	}
	m.relayout()
}

func (m *model) mouseRelease() {
	if m.drag != dragNone && m.drag != dragSel {
		m.saveSettings()
	}
	m.drag = dragNone
}

const helpText = `ghdiff keys

Diff
  v           cursor; v again starts selecting
  a           ask Claude about the selection
  y           copy the selection
  o / r       open the Claude conversation / review thread on the line
  n / p       next / previous file
  c / C       collapse the file / all files
  t / tab     hide the tree / move to it
  s           Claude pane beside or below the diff
  q           quit

Claude pane
  a           follow-up question       N   new conversation
  < >         other conversations      m   raw / formatted
  P           post to the PR           esc close
  ctrl+o      keys to the diff and back (the pane stays open)

Sizes
  + -         Claude pane bigger / smaller
  { }         tree narrower / wider
  mouse       drag a border to resize

Mouse
  click, drag select lines
  wheel       scroll

h or ? shows this; any key closes it`

func (m *model) helpView(w, h int) []string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.theme.Dim).Padding(0, 2)
	lines := strings.Split(box.Render(helpText), "\n")
	pad := strings.Repeat(" ", max(0, (w-ansi.StringWidth(lines[0]))/2))
	out := make([]string, h)
	top := max(0, (h-len(lines))/2)
	for i := range out {
		if j := i - top; j >= 0 && j < len(lines) {
			out[i] = ansi.Truncate(pad+lines[j], w, "")
		}
	}
	return out
}
