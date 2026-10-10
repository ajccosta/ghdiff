package main

import (
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// selection is a range of code lines in one file. The first v shows a
// cursor that ↑↓ move anywhere; the second v anchors it, and ↑↓ then extend
// the range within that file.
type selection struct {
	on, anchored   bool
	file           int
	anchor, cursor int // indexes into File.Lines
}

func (s selection) bounds() (int, int) {
	return min(s.anchor, s.cursor), max(s.anchor, s.cursor)
}

func (s selection) has(r row) bool {
	if !s.on || r.file != s.file || r.line < 0 {
		return false
	}
	if !s.anchored {
		return r.line == s.cursor
	}
	lo, hi := s.bounds()
	return r.line >= lo && r.line <= hi
}

func selectable(l Line) bool { return l.Kind == Ctx || l.Kind == Add || l.Kind == Del }

// firstRow and lastRow are the screen rows a diff line wraps onto.
func (m *model) firstRow(fi, li int) int {
	for i, r := range m.rows {
		if r.file == fi && r.line == li {
			return i
		}
	}
	return -1
}

func (m *model) lastRow(fi, li int) int {
	last := -1
	for i, r := range m.rows {
		if r.file == fi && r.line == li {
			last = i
		} else if last >= 0 {
			break
		}
	}
	return last
}

func (m *model) codeRow(i int) bool {
	r := m.rows[i]
	return r.line >= 0 && selectable(m.files[r.file].Lines[r.line])
}

// startCursor puts the cursor on the code line nearest the middle of the screen.
func (m *model) startCursor() {
	h := m.paneHeight()
	mid := m.off + h/2
	for d := 0; d < h; d++ {
		for _, i := range []int{mid - d, mid + d} {
			if i > m.off && i < min(len(m.rows), m.off+h) && m.codeRow(i) {
				r := m.rows[i]
				m.sel = selection{on: true, file: r.file, anchor: r.line, cursor: r.line}
				return
			}
		}
	}
}

// moveCursor steps to the next code line in d's direction: anywhere before
// the anchor is set, within the anchored file after.
func (m *model) moveCursor(d int) {
	if m.sel.anchored {
		lines := m.files[m.sel.file].Lines
		// A GitHub comment can't span hunks, so stop at the hunk's edge.
		for i := m.sel.cursor + d; i >= 0 && i < len(lines) && lines[i].Kind != Hunk; i += d {
			if selectable(lines[i]) {
				m.sel.cursor = i
				break
			}
		}
	} else {
		i := m.firstRow(m.sel.file, m.sel.cursor)
		if d > 0 {
			i = m.lastRow(m.sel.file, m.sel.cursor)
		}
		for i += d; i >= 0 && i < len(m.rows); i += d {
			if m.codeRow(i) {
				m.sel.file, m.sel.cursor = m.rows[i].file, m.rows[i].line
				break
			}
		}
	}
	m.showCursor()
}

// showCursor keeps the cursor's rows on screen, clear of the sticky header.
func (m *model) showCursor() {
	if top := m.firstRow(m.sel.file, m.sel.cursor); top >= 0 && top < m.off+1 {
		m.off = top - 1
	}
	if bot := m.lastRow(m.sel.file, m.sel.cursor); bot >= m.off+m.paneHeight() {
		m.off = bot - m.paneHeight() + 1
	}
	m.clamp()
}

// selectKey handles keys while a selection is open; false means not handled.
func (m *model) selectKey(k string) (bool, tea.Cmd) {
	switch k {
	case "esc":
		m.sel.on = false
	case "v":
		m.sel.anchored, m.sel.anchor = true, m.sel.cursor
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "a":
		if m.sel.anchored {
			lo, hi := m.sel.bounds()
			m.newChat(m.sel.file, lo, hi)
		}
	case "o":
		if cs := m.convosAt(m.sel.file, m.sel.cursor); len(cs) > 0 {
			m.openChats(cs)
		}
	case "y":
		if m.sel.anchored {
			return true, m.copySelection()
		}
	default:
		return false, nil
	}
	return true, nil
}

// copySelection puts the selected lines on the clipboard: plain code when
// they are all on one side of the change, a diff with +/- when they mix.
func (m *model) copySelection() tea.Cmd {
	lo, hi := m.sel.bounds()
	lines := m.files[m.sel.file].Lines[lo : hi+1]
	adds, dels := false, false
	for _, l := range lines {
		adds = adds || l.Kind == Add
		dels = dels || l.Kind == Del
	}
	var b strings.Builder
	for _, l := range lines {
		if !selectable(l) {
			continue
		}
		if adds && dels {
			b.WriteString(map[LineKind]string{Add: "+", Del: "-", Ctx: " "}[l.Kind])
		}
		b.WriteString(l.Raw + "\n")
	}
	n := m.selectedCount()
	m.flash = fmt.Sprintf("copied %d %s", n, plural(n, "line", "lines"))
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(b.String())
	if cmd.Run() == nil {
		return nil
	}
	return tea.SetClipboard(b.String()) // OSC 52, where there is no pbcopy
}

// markSelected replaces the first cell of a selected row with a bar, and of
// a row with a Claude conversation with its mark.
func (m *model) markSelected(i int) string {
	r := m.rows[i]
	text := r.text
	if !m.sel.has(r) {
		if mark := m.convoMark(i); mark != "" {
			return lipgloss.NewStyle().Foreground(m.theme.Accent).Faint(mark != "●").Render(mark) +
				ansi.TruncateLeft(text, 1, "")
		}
		return text
	}
	bar := lipgloss.NewStyle().Foreground(m.theme.Accent)
	if r.line == m.sel.cursor && r.file == m.sel.file {
		bar = bar.Bold(true)
		return bar.Render("▶") + ansi.TruncateLeft(text, 1, "")
	}
	return bar.Render("▌") + ansi.TruncateLeft(text, 1, "")
}

// selectedCount is the number of lines in the selection, for the footer.
func (m *model) selectedCount() int {
	lo, hi := m.sel.bounds()
	n := 0
	for _, l := range m.files[m.sel.file].Lines[lo : hi+1] {
		if selectable(l) {
			n++
		}
	}
	return n
}
