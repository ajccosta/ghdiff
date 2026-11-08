package main

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The split view (|): the old side of each file on the left and the new one
// on the right, as VS Code shows a diff. Unchanged lines show on both sides,
// a removed line beside the line added in its place, and a side with no line
// there is hatched. A conflict (git's <<<<<<< ======= >>>>>>> markers in
// the new side) shows its two versions next to each other instead: the
// current one on the left, the incoming one on the right.

// split is whether the diff shows side by side (the model's Split).
func (m *model) split() bool { return m.Split }
func (noExpand) split() bool { return false }
func (m *model) toggleSplit() {
	m.Split = !m.Split
	m.saveSettings()
	m.relayout()
}

// splitRows appends file fi's rows, side by side, to rows; gap draws the
// hidden lines under an @@ row.
func splitRows(rows []row, t Theme, f *File, fi, nw, width int, format func(Line) Line,
	ex expander, gap func(int) []row) []row {
	col := 1 + nw + 4 // the left side's text starts after its number and sign
	add := func(li int, texts []string) {
		for _, text := range texts {
			rows = append(rows, row{file: fi, line: li, col: col, text: " " + text})
		}
		if li >= 0 {
			rows = append(rows, ex.under(fi, li, col, width)...)
		}
	}
	lines := f.Lines
	for i := 0; i < len(lines); {
		l := format(lines[i])
		switch {
		case l.Kind == Hunk || l.Kind == Note:
			if lab := ex.expandLabel(fi, i); lab != "" && l.Kind == Hunk {
				l.Text = lab + "  " + l.Text
			}
			add(i, renderLine(t, l, nw, width-1, width-1))
			if l.Kind == Hunk {
				rows = append(rows, gap(i)...)
			}
			i++
		case conflictEnd(lines, i) > 0:
			end := conflictEnd(lines, i)
			rows = conflictRows(rows, t, lines, i, end, fi, nw, width, format, ex)
			i = end + 1
		case l.Kind == Ctx:
			add(i, splitLine(t, &l, &l, nw, width-1, nil, nil))
			i++
		default: // a run of removed lines beside the added ones after it
			dels, adds := i, i
			for adds < len(lines) && lines[adds].Kind == Del {
				adds++
			}
			end := adds
			for end < len(lines) && lines[end].Kind == Add && conflictEnd(lines, end) < 0 {
				end++
			}
			for k := 0; k < max(adds-dels, end-adds); k++ {
				var left, right *Line
				li := -1
				if dels+k < adds {
					d := format(lines[dels+k])
					left, li = &d, dels+k
				}
				if adds+k < end {
					a := format(lines[adds+k])
					right = &a
					if li < 0 {
						li = adds + k
					}
				}
				add(li, splitLine(t, left, right, nw, width-1, nil, nil))
			}
			i = max(end, i+1)
		}
	}
	return rows
}

// conflictEnd is the index of the >>>>>>> line closing the conflict that
// lines[i] opens, or -1 when it opens none.
func conflictEnd(lines []Line, i int) int {
	if lines[i].Kind == Del || !strings.HasPrefix(lines[i].Text, "<<<<<<< ") {
		return -1
	}
	for j := i + 1; j < len(lines) && lines[j].Kind != Hunk; j++ {
		if lines[j].Kind != Del && strings.HasPrefix(lines[j].Text, ">>>>>>> ") {
			return j
		}
	}
	return -1
}

// conflictRows appends the conflict from lines[start] (<<<<<<<) to
// lines[end] (>>>>>>>): a head naming each side, then the current version
// beside the incoming one. A diff3 base (|||||||) is left out, as are the
// lines the diff removed, which are in neither version.
func conflictRows(rows []row, t Theme, lines []Line, start, end, fi, nw, width int,
	format func(Line) Line, ex expander) []row {
	var cur, inc []int
	part := &cur
	for j := start + 1; j < end; j++ {
		switch l := lines[j]; {
		case l.Kind == Del:
		case strings.HasPrefix(l.Text, "|||||||"):
			part = nil
		case l.Text == "=======":
			part = &inc
		case part != nil:
			*part = append(*part, j)
		}
	}
	lw, rw := splitWidths(width - 1)
	head := func(w int, bg color.Color, name, label string) string {
		s := lipgloss.NewStyle().Background(bg).Bold(true)
		text := ansi.Truncate(" "+name+" · "+label, w, "…")
		return s.Render(text + strings.Repeat(" ", max(0, w-ansi.StringWidth(text))))
	}
	col := 1 + nw + 4
	rows = append(rows, row{file: fi, line: start, col: col, text: " " +
		head(lw, t.CurHead, "Current", strings.TrimPrefix(lines[start].Text, "<<<<<<< ")) +
		divider(t) + head(rw, t.IncHead, "Incoming", strings.TrimPrefix(lines[end].Text, ">>>>>>> "))})
	for k := 0; k < max(len(cur), len(inc)); k++ {
		var left, right *Line
		li := -1
		if k < len(cur) {
			c := format(lines[cur[k]])
			c.Kind, c.Spans = Ctx, nil // numbered as the rest of its side
			left, li = &c, cur[k]
		}
		if k < len(inc) {
			c := format(lines[inc[k]])
			c.Kind, c.Old, c.Spans = Ctx, 0, nil
			right = &c
			if li < 0 {
				li = inc[k]
			}
		}
		for _, text := range splitLine(t, left, right, nw, width-1, t.CurBg, t.IncBg) {
			rows = append(rows, row{file: fi, line: li, col: col, text: " " + text})
		}
		rows = append(rows, ex.under(fi, li, col, width)...)
	}
	return rows
}

// splitWidths shares width between the sides and the divider.
func splitWidths(width int) (left, right int) {
	left = (width - 1) / 2
	return left, width - 1 - left
}

func divider(t Theme) string { return lipgloss.NewStyle().Foreground(t.Dim).Render("│") }

// splitLine draws left (its old number) beside right (its new number), each
// wrapped to its half of width, with backgrounds lbg and rbg in place of
// their own when set. A missing side is hatched.
func splitLine(t Theme, left, right *Line, nw, width int, lbg, rbg color.Color) []string {
	lw, rw := splitWidths(width)
	side := func(l *Line, n func(Line) int, w int, bg color.Color) []string {
		if l == nil {
			return []string{lipgloss.NewStyle().Foreground(t.HeadBg).Render(strings.Repeat("╱", w))}
		}
		return renderNums(t, *l, []int{n(*l)}, nw, max(1, w-nw-4), w, bg)
	}
	ls := side(left, func(l Line) int { return l.Old }, lw, lbg)
	rs := side(right, func(l Line) int { return l.New }, rw, rbg)
	out := make([]string, max(len(ls), len(rs)))
	for i := range out {
		a, b := strings.Repeat(" ", lw), strings.Repeat(" ", rw)
		if i < len(ls) {
			a = ls[i]
		} else if left == nil {
			a = ls[0]
		}
		if i < len(rs) {
			b = rs[i]
		} else if right == nil {
			b = rs[0]
		}
		out[i] = a + divider(t) + b
	}
	return out
}
