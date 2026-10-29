package main

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Where review threads show (T switches, remembered in settings.json). An
// open thread (r, or a click on the box) is in the tree's column, where t (or
// ctrl+o) switches between the tree and the thread.
//   - hover: a box over the diff while the mouse is on a thread's ◆ (or the
//     box), or the cursor is on its first line.
//   - left: no box; r opens the thread.
//   - pane: open threads go below or beside the diff, like a Claude
//     conversation. Only set by hand ("threadView": "pane"); T skips it.

type threadMode string

const (
	threadsHover threadMode = "hover"
	threadsLeft  threadMode = "left"
	threadsPane  threadMode = "pane"
)

// threadModes in the order T cycles through them, with what the footer says.
var threadModes = []struct {
	mode threadMode
	note string
}{
	{threadsHover, "Review threads and Claude conversations show when you hover their ◆ or ●"},
	{threadsLeft, "Review threads open in the left column (t switches to the tree)"},
}

func (m *model) threadMode() threadMode {
	if m.ThreadView == threadsPane {
		return threadsPane
	}
	for _, tm := range threadModes {
		if tm.mode == m.ThreadView {
			return tm.mode
		}
	}
	return threadsHover
}

// cycleThreadMode moves to the next place, taking the thread on screen along.
func (m *model) cycleThreadMode() {
	var shown []*thread
	for _, n := range m.pop.notes {
		if t, ok := n.(threadNote); ok {
			shown = append(shown, t.t)
		}
	}
	if m.thread != nil {
		shown = nil
	}
	cur, next := m.threadMode(), threadModes[0]
	for i, tm := range threadModes {
		if tm.mode == cur {
			next = threadModes[(i+1)%len(threadModes)]
		}
	}
	m.ThreadView, m.flash = next.mode, next.note
	m.saveSettings()
	switch {
	case m.thread != nil && m.threadMode() == threadsHover:
		t := m.thread
		m.closeThread()
		m.sel = selection{on: true, anchor: pos{t.file, t.lo}, cursor: pos{t.file, t.lo}}
		m.showCursor()
	case m.thread != nil:
		m.relayout()
		m.showCursor()
	case len(shown) > 0 && m.threadMode() != threadsHover:
		m.focus = focusDiff
		m.openThreads(shown)
	}
}

// startsThread are the threads whose ◆ is on line li of file fi.
func (m *model) startsThread(fi, li int) []*thread {
	var out []*thread
	for _, t := range m.threadsAt(fi, li) {
		if t.lo == li {
			out = append(out, t)
		}
	}
	return out
}

// diffX is the screen column of the diff pane's first cell, where ◆ is.
func (m *model) diffX() int {
	if tw := m.treeWidth(); tw > 0 {
		return tw + 1
	}
	return 0
}

// leftThread: the open thread is drawn in the tree's column.
func (m *model) leftThread() bool {
	return m.thread != nil && m.threadMode() != threadsPane && m.width >= 60
}

// paneShown: a pane below or beside the diff is open (not a left thread).
func (m *model) paneShown() bool { return m.chat != nil || (m.thread != nil && !m.leftThread()) }

// leftView is the tree's column while a thread is open there: the thread, or
// the tree when the keys are in the diff (t or ctrl+o).
func (m *model) leftView(w, h int) []string {
	out := m.threadView(w, h)
	if m.typing && !m.away {
		out[len(out)-1] = m.inputLine(w)
	}
	return out
}

// hover is where the mouse is (motion without a button held).
type hover struct {
	x, y int
	on   bool
}

// popup is the hover box last drawn, in screen cells, and its notes; starts
// are the box rows (from y) where each note begins.
type popup struct {
	x, y, w, h int
	notes      []note
	starts     []int
}

func (p popup) has(x, y int) bool {
	return len(p.notes) > 0 && x >= p.x && x < p.x+p.w && y >= p.y && y < p.y+p.h
}

// noteAt is the note drawn on screen row y of the box.
func (p popup) noteAt(y int) note {
	n := p.notes[0]
	for i, s := range p.starts {
		if y-p.y >= s {
			n = p.notes[i]
		}
	}
	return n
}

// hoverNotes are the notes to show in the box, and the diff row to put it
// under: the line under the mouse, else the cursor's.
func (m *model) hoverNotes() ([]note, int) {
	if m.threadMode() != threadsHover || m.paneOpen() || m.help || m.consent || m.confirmPost {
		return nil, -1
	}
	if m.mouse.on && m.pop.has(m.mouse.x, m.mouse.y) {
		return m.pop.notes, m.pop.anchor(m)
	}
	if m.mouse.on {
		if ns, r := m.markUnder(m.mouse.x, m.mouse.y); len(ns) > 0 {
			return ns, r
		}
	}
	if m.sel.on && m.focus == focusDiff && !m.sel.anchored {
		if c := m.sel.cursor; len(m.notesAt(c.file, c.line)) > 0 {
			return m.notesAt(c.file, c.line), m.lastRow(c.file, c.line)
		}
	}
	return nil, -1
}

// markUnder are the notes of the ◆ or ● at screen cell (x, y), and the last
// diff row of their line.
func (m *model) markUnder(x, y int) ([]note, int) {
	if x != m.diffX() {
		return nil, -1
	}
	r := m.diffRow(y)
	if r < 0 || m.rows[r].line < 0 || m.firstRow(m.rows[r].file, m.rows[r].line) != r {
		return nil, -1
	}
	return m.notesAt(m.rows[r].file, m.rows[r].line), m.lastRow(m.rows[r].file, m.rows[r].line)
}

// anchor is the diff row the box hangs from, so it stays put while the mouse
// is on it.
func (p popup) anchor(m *model) int {
	return m.lastRow(p.notes[0].at())
}

// drawPopup lays the hover box over the diff pane's lines (screen rows 1..h).
func (m *model) drawPopup(lines []string) {
	ns, row := m.hoverNotes() // before forgetting the last box: the mouse may be on it
	m.pop = popup{}
	if len(ns) == 0 || row < 0 {
		return
	}
	x0 := m.diffX()
	dw := m.diffWidth()
	if dw < 34 {
		return
	}
	w := min(dw-4, 90)
	inner := w - 4 // border and padding
	th := m.theme
	dim := lipgloss.NewStyle().Foreground(th.Dim)

	var body []string
	var starts []int
	for i, n := range ns {
		if i > 0 {
			body = append(body, "")
		}
		starts = append(starts, 1+len(body)) // under the top border
		body = append(body, lipgloss.NewStyle().Foreground(th.ModFg).Bold(true).Render(n.title(m)))
		body = append(body, n.body(m, inner)...)
	}
	h := len(lines)
	maxBody := max(3, h/2-2)
	hint := "click to open and reply · T no box"
	if len(body) > maxBody {
		body = body[:maxBody-1]
		hint = "… click to read it all"
	}
	body = append(body, dim.Render(hint))
	for i := range body {
		body[i] = ansi.Truncate(body[i], inner, "")
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(th.ModFg).
		Padding(0, 1).Width(w).Render(strings.Join(body, "\n"))
	bl := strings.Split(box, "\n")

	a := row - m.off // 0-based line in the pane
	top := a + 1
	if top+len(bl) > h {
		top = a - len(bl)
		if top < 0 {
			top = max(0, h-len(bl))
		}
	}
	x := x0 // right under the ◆, so the mouse can move onto it
	for i, l := range bl {
		if j := top + i; j >= 0 && j < h {
			lines[j] = overlay(lines[j], x, l, w)
		}
	}
	m.pop = popup{x: x, y: top + 1, w: w, h: len(bl), notes: ns, starts: starts}
}

// overlay puts s (w cells wide) over line from column x.
func overlay(line string, x int, s string, w int) string {
	left := ansi.Truncate(line, x, "")
	pad := strings.Repeat(" ", max(0, x-ansi.StringWidth(left)))
	return left + "\x1b[0m" + pad + s + "\x1b[0m" + ansi.TruncateLeft(line, x+w, "")
}
