package main

import (
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// span is a range of lines in one file: File.Lines indexes lo..hi. It may
// cross hunks; the hunk headers in it are not code and are skipped.
type span struct{ file, lo, hi int }

func (s span) covers(fi, li int) bool { return s.file == fi && li >= s.lo && li <= s.hi }

// pos is a line of the diff; positions order by file, then line.
type pos struct{ file, line int }

func (p pos) before(q pos) bool { return p.file < q.file || p.file == q.file && p.line < q.line }

// selection is any number of ranges of code lines. The first v shows a
// cursor that ↑↓ move anywhere; the second v anchors it, and ↑↓ then extend
// the range, across hunks and files. v again keeps that range and shows the
// cursor for the next one; a and y take them all.
type selection struct {
	on, anchored   bool
	anchor, cursor pos
	kept           []span // the ranges before the current one
}

// spanSel selects one range, with the cursor on its last line.
func spanSel(fi, lo, hi int) selection {
	return selection{on: true, anchored: true, anchor: pos{fi, lo}, cursor: pos{fi, hi}}
}

// spansSel selects the ranges: the last is the current one, the others kept.
func spansSel(spans []span) selection {
	if len(spans) == 0 {
		return selection{}
	}
	last := spans[len(spans)-1]
	s := spanSel(last.file, last.lo, last.hi)
	s.kept = append([]span(nil), spans[:len(spans)-1]...)
	return s
}

// active is the current range's first and last line, in diff order.
func (s selection) active() (pos, pos) {
	if s.cursor.before(s.anchor) {
		return s.cursor, s.anchor
	}
	return s.anchor, s.cursor
}

func (s selection) has(r row) bool {
	if !s.on || r.line < 0 {
		return false
	}
	for _, k := range s.kept {
		if k.covers(r.file, r.line) {
			return true
		}
	}
	p := pos{r.file, r.line}
	if !s.anchored {
		return p == s.cursor
	}
	lo, hi := s.active()
	return !p.before(lo) && !hi.before(p)
}

// selSpans are the selected ranges, the current one split by file (leaving
// out collapsed files) and each trimmed to its first and last code line.
func (m *model) selSpans() []span {
	if !m.sel.on {
		return nil
	}
	out := append([]span(nil), m.sel.kept...)
	if !m.sel.anchored {
		return out
	}
	lo, hi := m.sel.active()
	for fi := lo.file; fi <= hi.file; fi++ {
		if m.collapsed[fi] {
			continue
		}
		lines := m.files[fi].Lines
		a, z := 0, len(lines)-1
		if fi == lo.file {
			a = lo.line
		}
		if fi == hi.file {
			z = hi.line
		}
		for a <= z && !selectable(lines[a]) {
			a++
		}
		for z >= a && !selectable(lines[z]) {
			z--
		}
		if a <= z {
			out = append(out, span{fi, a, z})
		}
	}
	return out
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
				p := pos{m.rows[i].file, m.rows[i].line}
				m.sel = selection{on: true, anchor: p, cursor: p}
				return
			}
		}
	}
}

// moveCursor steps the cursor to the next code line in d's direction, over
// hunk headers and into the next file.
func (m *model) moveCursor(d int) {
	c := m.sel.cursor
	i := m.firstRow(c.file, c.line)
	if d > 0 {
		i = m.lastRow(c.file, c.line)
	}
	for i += d; i >= 0 && i < len(m.rows); i += d {
		if m.codeRow(i) {
			m.sel.cursor = pos{m.rows[i].file, m.rows[i].line}
			break
		}
	}
	m.showCursor()
}

// showCursor keeps the cursor's rows on screen, clear of the sticky header.
func (m *model) showCursor() {
	c := m.sel.cursor
	if top := m.firstRow(c.file, c.line); top >= 0 && top < m.off+1 {
		m.off = top - 1
	}
	if bot := m.lastRow(c.file, c.line); bot >= m.off+m.paneHeight() {
		m.off = bot - m.paneHeight() + 1
	}
	m.clamp()
}

// selectKey handles keys while a selection is open; false means not handled.
func (m *model) selectKey(k string) (bool, tea.Cmd) {
	c := m.sel.cursor
	switch k {
	case "esc":
		m.sel = selection{}
	case "v":
		if m.sel.anchored { // keep this range, then pick the next
			m.sel.kept, m.sel.anchored = m.selSpans(), false
		} else {
			m.sel.anchored, m.sel.anchor = true, c
		}
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "a":
		if spans := m.selSpans(); len(spans) > 0 {
			m.newChat(spans)
		}
	case "o":
		if cs := m.convosAt(c.file, c.line); len(cs) > 0 {
			m.openChats(cs)
		}
	case "r":
		if ts := m.threadsAt(c.file, c.line); len(ts) > 0 {
			m.openThreads(ts)
		}
	case "y":
		if spans := m.selSpans(); len(spans) > 0 {
			return true, m.copySelection(spans)
		}
	default:
		return false, nil
	}
	return true, nil
}

// copySelection puts the selected lines on the clipboard: plain code when
// they are all on one side of the change, a diff with +/- when they mix.
// With more than one range, each starts with its place ("model.go:L12–L18");
// a hunk header inside a range becomes "⋮" (or the header itself in a diff).
func (m *model) copySelection(spans []span) tea.Cmd {
	text := m.copyText(spans)
	m.flash = "copied " + m.selSummary()
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if cmd.Run() == nil {
		return nil
	}
	return tea.SetClipboard(text) // OSC 52, where there is no pbcopy
}

func (m *model) copyText(spans []span) string {
	adds, dels := false, false
	for _, sp := range spans {
		for _, l := range m.files[sp.file].Lines[sp.lo : sp.hi+1] {
			adds = adds || l.Kind == Add
			dels = dels || l.Kind == Del
		}
	}
	mixed := adds && dels
	var b strings.Builder
	for i, sp := range spans {
		if len(spans) > 1 {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.rangeLabel(sp.file, sp.lo, sp.hi) + "\n")
		}
		for _, l := range m.files[sp.file].Lines[sp.lo : sp.hi+1] {
			switch {
			case l.Kind == Hunk && mixed:
				b.WriteString(l.Text + "\n")
			case l.Kind == Hunk:
				b.WriteString("⋮\n")
			case !selectable(l):
			case mixed:
				b.WriteString(map[LineKind]string{Add: "+", Del: "-", Ctx: " "}[l.Kind] + l.Raw + "\n")
			default:
				b.WriteString(l.Raw + "\n")
			}
		}
	}
	return b.String()
}

// markSelected replaces the first cell of a selected row with a bar, and of
// a row with a Claude conversation with its mark.
func (m *model) markSelected(i int) string {
	r := m.rows[i]
	text := r.text
	if !m.sel.has(r) {
		if mark, github := m.convoMark(i); mark != "" {
			st := lipgloss.NewStyle().Foreground(m.theme.Accent)
			if github {
				st = st.Foreground(m.theme.ModFg)
			}
			return st.Faint(mark == "│").Render(mark) + ansi.TruncateLeft(text, 1, "")
		}
		return text
	}
	bar := lipgloss.NewStyle().Foreground(m.theme.Accent)
	first := i == 0 || m.rows[i-1].file != r.file || m.rows[i-1].line != r.line
	if (pos{r.file, r.line}) == m.sel.cursor && first { // not on a wrapped line's other rows
		bar = bar.Bold(true)
		return bar.Render("▶") + ansi.TruncateLeft(text, 1, "")
	}
	if !m.sel.anchored {
		return text // just the cursor: nothing selected yet
	}
	return bar.Render("▌") + ansi.TruncateLeft(text, 1, "")
}

// selectedCount is the number of code lines in the selection.
func (m *model) selectedCount() int {
	n := 0
	for _, sp := range m.selSpans() {
		for _, l := range m.files[sp.file].Lines[sp.lo : sp.hi+1] {
			if selectable(l) {
				n++
			}
		}
	}
	return n
}

// selSummary is "12 lines", or "12 lines in 3 ranges", for the footer.
func (m *model) selSummary() string {
	n := m.selectedCount()
	s := fmt.Sprintf("%d %s", n, plural(n, "line", "lines"))
	if r := len(m.selSpans()); r > 1 {
		s += fmt.Sprintf(" in %d ranges", r)
	}
	return s
}

// selHints is the footer while selecting.
func (m *model) selHints() string {
	switch {
	case m.sel.anchored:
		return m.selSummary() + " selected · v add another range · a ask Claude · y copy · esc cancel"
	case len(m.sel.kept) > 0:
		return m.selSummary() + " selected · go to the next range's first line, then v · a ask Claude · y copy · esc cancel"
	}
	return "go to the first line, then press v to start selecting · esc cancel"
}
