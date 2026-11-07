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
		if !m.paneShown() {
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
	if m.leftThread() && !m.away {
		return x < m.treeWidth() && y >= 1 && y <= m.paneHeight()
	}
	if !m.paneShown() {
		return false
	}
	if cw := m.chatWidth(); cw > 0 {
		return x > m.width-cw-1 && y >= 1 && y <= m.paneHeight()
	}
	return y > m.paneHeight() && y < m.height-1
}

// scrollPane scrolls the Claude or review-thread pane by n lines, as ↑↓ do in it.
func (m *model) scrollPane(n int) {
	switch {
	case m.thread != nil:
		m.thread.scroll += n
	case m.chat != nil && m.chat.term != nil:
		m.chat.term.wheel(n)
	case m.chat != nil:
		m.chat.scroll += n
	}
}

// termOrigin is where Claude Code's screen starts: under the pane's title.
func (m *model) termOrigin() (x, y int) {
	if cw := m.chatWidth(); cw > 0 {
		return m.width - cw, 2
	}
	return 0, m.paneHeight() + 2
}

// toTerm passes a mouse event over Claude Code's screen on to it, when it
// draws the whole screen itself; false if the event is not for it. A release
// goes where the press went.
func (m *model) toTerm(ev tea.MouseMsg) bool {
	c := m.chat
	if c == nil || c.term == nil || !c.term.fullScreen() || m.help || m.confirmPost {
		return false
	}
	mo := ev.Mouse()
	x0, y0 := m.termOrigin()
	x, y := mo.X-x0, mo.Y-y0
	in := m.inPane(mo.X, mo.Y) && x >= 0 && y >= 0 && x < c.term.w && y < c.term.h
	switch ev.(type) {
	case tea.MouseClickMsg:
		m.termPress = in
	case tea.MouseReleaseMsg:
		in, m.termPress = m.termPress, false
	}
	if !in {
		return false
	}
	if _, click := ev.(tea.MouseClickMsg); click && m.away {
		m.backToPane()
	}
	c.term.mouse(ev, x, y)
	return true
}

// borderAt is the border at x, y that a drag resizes, if any.
func (m *model) borderAt(x, y int) drag {
	tw, cw := m.treeWidth(), m.chatWidth()
	switch {
	case tw > 0 && x == tw && y >= 1 && y <= m.paneHeight():
		return dragTree
	case cw > 0 && x == m.width-cw-1 && y >= 1 && y <= m.paneHeight():
		return dragPaneW
	case m.paneShown() && cw == 0 && y == m.paneHeight()+1:
		return dragPaneH
	}
	return dragNone
}

func (m *model) mouseClick(msg tea.MouseClickMsg) tea.Cmd {
	mo := msg.Mouse()
	if mo.Button != tea.MouseLeft || m.help {
		m.help = false
		return nil
	}
	if d := m.borderAt(mo.X, mo.Y); d != dragNone {
		m.drag = d
		return nil
	}
	if !m.inPane(mo.X, mo.Y) && m.paneOpen() && !m.away && !m.consent && !m.confirmPost {
		m.switchFocus() // a click anywhere outside the pane is like ctrl+o
	}
	tw := m.treeWidth()
	switch {
	case m.pop.has(mo.X, mo.Y):
		m.openNote(m.pop.noteAt(mo.Y), m.pop.notes)
	case m.inPane(mo.X, mo.Y):
		if m.away {
			m.backToPane()
		}
	case mo.X < tw:
		if mo.Y >= 1 {
			return m.leftClick(mo.X, mo.Y-1)
		}
	default:
		if fi := m.headerAt(mo.Y); fi >= 0 {
			m.clickHeader(fi, mo.X)
			return nil
		}
		r := m.diffRow(mo.Y)
		if r >= 0 && m.rows[r].thread != nil && !m.consent && !m.confirmPost {
			m.openThreads([]*thread{m.rows[r].thread}) // a thread under its lines opens
			return nil
		}
		if fi, li, ok := m.gapRow(r); ok && !m.consent && !m.confirmPost {
			return m.toggleGap(fi, li) // an @@ row shows or hides the lines it skips
		}
		if r < 0 || !m.codeRow(r) || m.consent || m.confirmPost {
			return nil
		}
		if ns, _ := m.markUnder(mo.X, mo.Y); len(ns) > 0 && mo.Mod == 0 {
			m.openNote(ns[len(ns)-1], ns) // a click on ◆ or ● opens it
			return nil
		}
		m.focus = focusDiff
		var kept []span
		if mo.Mod.Contains(tea.ModAlt) || mo.Mod.Contains(tea.ModCtrl) {
			kept = m.selSpans() // Option- or Ctrl-click adds a range
		}
		m.sel = spanSel(m.rows[r].file, m.rows[r].line, m.rows[r].line)
		m.sel.kept = kept
		m.drag = dragSel
	}
	return nil
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
		// Extend to the line under the pointer, across hunks and files.
		r := m.diffRow(mo.Y)
		if mo.Y < 1 {
			m.scroll(-1)
			r = m.diffRow(1)
		} else if mo.Y > m.paneHeight() {
			m.scroll(1)
			r = m.diffRow(m.paneHeight())
		}
		if r >= 0 && m.codeRow(r) {
			m.sel.cursor = pos{m.rows[r].file, m.rows[r].line}
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
  c           comment on the selection on the PR
  y           copy the selection
  o / r       open the Claude conversation / review thread on the line
  T           review threads: under their lines, on hover, or only with r
  n / p       next / previous file
  c / C       collapse the file / all files
  e / E       show the lines an @@ hides (at the cursor) / all in the file
  /           search (n / N next / previous match, esc clears)
  m           markdown files: formatted / source
  t / tab     hide the left pane / move to it (tab again: the next one)
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
  click @@    show / hide the lines it hides
  hover       a commented line shows its review thread
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
