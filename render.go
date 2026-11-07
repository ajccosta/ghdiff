package main

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Theme colours follow GitHub's diff view.
type Theme struct {
	AddBg, AddWord, AddGutter color.Color
	DelBg, DelWord, DelGutter color.Color
	HunkBg, HunkFg            color.Color
	HeadBg, Dim, Accent       color.Color
	AddFg, DelFg, ModFg       color.Color
	Syntax                    *chroma.Style // code colours, nil for none
}

// themeFor is the diff's colours (diffTheme) and the code's (syntax.go).
func themeFor(dark, truecolor bool) Theme {
	t := diffTheme(dark, truecolor)
	t.Syntax = syntaxStyle(dark)
	return t
}

// diffTheme picks colours for the background and the terminal's colour depth.
// The truecolor values are GitHub's; on 256-colour terminals (Terminal.app,
// tmux) those subtle tints would all round to grey, so stronger ones are used.
func diffTheme(dark, truecolor bool) Theme {
	c := lipgloss.Color
	if !truecolor && dark {
		return Theme{
			AddBg: c("22"), AddWord: c("28"), AddGutter: c("22"),
			DelBg: c("52"), DelWord: c("88"), DelGutter: c("52"),
			HunkBg: c("17"), HunkFg: c("110"),
			HeadBg: c("236"), Dim: c("244"), Accent: c("75"),
			AddFg: c("71"), DelFg: c("203"), ModFg: c("178"),
		}
	}
	if !truecolor {
		return Theme{
			AddBg: c("194"), AddWord: c("157"), AddGutter: c("194"),
			DelBg: c("224"), DelWord: c("217"), DelGutter: c("224"),
			HunkBg: c("195"), HunkFg: c("66"),
			HeadBg: c("255"), Dim: c("245"), Accent: c("25"),
			AddFg: c("28"), DelFg: c("160"), ModFg: c("136"),
		}
	}
	if dark {
		return Theme{
			AddBg: c("#12261e"), AddWord: c("#1f5a33"), AddGutter: c("#1a3a27"),
			DelBg: c("#2d1517"), DelWord: c("#7a2a30"), DelGutter: c("#4a1f23"),
			HunkBg: c("#121d2f"), HunkFg: c("#8b9eb8"),
			HeadBg: c("#161b22"), Dim: c("#6e7681"), Accent: c("#58a6ff"),
			AddFg: c("#3fb950"), DelFg: c("#f85149"), ModFg: c("#d29922"),
		}
	}
	return Theme{
		AddBg: c("#e6ffec"), AddWord: c("#abf2bc"), AddGutter: c("#ccffd8"),
		DelBg: c("#ffebe9"), DelWord: c("#ffc1bd"), DelGutter: c("#ffd7d5"),
		HunkBg: c("#ddf4ff"), HunkFg: c("#57606a"),
		HeadBg: c("#f6f8fa"), Dim: c("#8c959f"), Accent: c("#0969da"),
		AddFg: c("#1a7f37"), DelFg: c("#cf222e"), ModFg: c("#9a6700"),
	}
}

// row is one screen line of the diff pane.
type row struct {
	file   int
	line   int // index into File.Lines, -1 for headers and spacers
	header bool
	tail   bool    // the row after a file's last hunk that shows the rest (expand.go)
	col    int     // where the text starts after the line numbers; search looks from there
	thread *thread // a review thread drawn under its lines; a click opens it
	text   string
}

// expander says which hidden lines show (expand.go).
type expander interface {
	gap(fi, li int) []Line
	expandLabel(fi, li int) string
	mdSource() bool                     // markdown files show their source, not formatted (md.go)
	under(fi, li, col, width int) []row // rows drawn under line li (threadinline.go)
}

func digits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}

// statusMark and counts draw on top of base so a header keeps its background.
func statusMark(t Theme, status string, base lipgloss.Style) string {
	switch status {
	case "added":
		return base.Foreground(t.AddFg).Render("+")
	case "deleted":
		return base.Foreground(t.DelFg).Render("−")
	case "renamed":
		return base.Foreground(t.Accent).Render("→")
	case "binary":
		return base.Foreground(t.Dim).Render("◆")
	}
	return base.Foreground(t.ModFg).Render("•")
}

func counts(t Theme, adds, dels int, base lipgloss.Style) string {
	return base.Foreground(t.AddFg).Render(fmt.Sprintf("+%d", adds)) + base.Render(" ") +
		base.Foreground(t.DelFg).Render(fmt.Sprintf("−%d", dels))
}

// fileHeader is a file's header row; the current file's (the one the tree
// highlights too) has its arrow and name in the accent colour.
func fileHeader(t Theme, f *File, collapsed bool, width int, current bool, note string) string {
	arrow := "▾"
	if collapsed {
		arrow = "▸"
	}
	name := f.Path()
	if f.Status == "renamed" && f.Old != f.New {
		name = f.Old + " → " + f.New
	}
	bg := lipgloss.NewStyle().Background(t.HeadBg)
	hl := bg
	if current {
		hl = bg.Foreground(t.Accent)
	}
	left := bg.Render(" ") + hl.Bold(current).Render(arrow) + bg.Render(" ") + statusMark(t, f.Status, bg) + hl.Bold(true).Render(" "+name+" ")
	right := counts(t, f.Adds, f.Dels, bg) + bg.Render(" ")
	if note != "" {
		right = bg.Foreground(t.Dim).Render(note+"   ") + right
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		left = ansi.Truncate(left, max(0, width-ansi.StringWidth(right)-2), "…") + bg.Render(" ")
		gap = max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right))
	}
	return left + bg.Render(strings.Repeat(" ", gap)) + right
}

// fileNote is the dim note in a file's header.
func fileNote(f *File, collapsed, mdSource bool) string {
	if collapsed && !f.prepared {
		return "not loaded · c loads it"
	}
	return mdNote(f, mdSource)
}

// buildRows lays the whole diff out for a pane of the given width.
func buildRows(t Theme, files []*File, collapsed map[int]bool, width int, ex expander) ([]row, []int) {
	maxNo := 1
	for _, f := range files {
		for _, l := range f.Lines {
			maxNo = max(maxNo, l.Old, l.New)
		}
	}
	nw := max(3, digits(maxNo))
	gutterW := 2*nw + 5 // "old new  s "
	cw := max(10, width-gutterW)

	var rows []row
	starts := make([]int, len(files))
	for fi, f := range files {
		starts[fi] = len(rows)
		rows = append(rows, row{file: fi, line: -1, header: true, text: fileHeader(t, f, collapsed[fi], width, false, fileNote(f, collapsed[fi], ex.mdSource()))})
		if collapsed[fi] {
			rows = append(rows, row{file: fi, line: -1})
			continue
		}
		f.prepare()
		format := func(l Line) Line { return l }
		if isMarkdown(f) && !ex.mdSource() {
			format = mdFormatter()
		}
		// Lines the diff leaves out show under their @@ row, not selectable.
		gap := func(li int) {
			for _, g := range ex.gap(fi, li) {
				g = format(g)
				for _, text := range renderLine(t, g, nw, cw-1, width-1) {
					rows = append(rows, row{file: fi, line: -1, col: 1 + gutterW, text: " " + text})
				}
			}
		}
		for li, l := range f.Lines {
			if lab := ex.expandLabel(fi, li); lab != "" && l.Kind == Hunk {
				l.Text = lab + "  " + l.Text
			}
			l = format(l)
			// One spare column on the left for the selection bar.
			for _, text := range renderLine(t, l, nw, cw-1, width-1) {
				rows = append(rows, row{file: fi, line: li, col: 1 + gutterW, text: " " + text})
			}
			rows = append(rows, ex.under(fi, li, 1+gutterW, width)...)
			if l.Kind == Hunk {
				gap(li)
			}
		}
		if lab := ex.expandLabel(fi, len(f.Lines)); lab != "" {
			text := renderLine(t, Line{Kind: Hunk, Text: lab}, nw, cw-1, width-1)[0]
			rows = append(rows, row{file: fi, line: -1, tail: true, col: 1 + gutterW, text: " " + text})
			gap(len(f.Lines))
		}
		rows = append(rows, row{file: fi, line: -1})
	}
	return rows, starts
}

func renderLine(t Theme, l Line, nw, cw, width int) []string {
	num := func(n int) string {
		if n == 0 {
			return strings.Repeat(" ", nw)
		}
		return fmt.Sprintf("%*d", nw, n)
	}

	switch l.Kind {
	case Hunk:
		s := lipgloss.NewStyle().Background(t.HunkBg).Foreground(t.HunkFg)
		text := ansi.Truncate(strings.Repeat(" ", 2*nw+5)+l.Text, width, "…")
		return []string{s.Render(text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text))))}
	case Note:
		s := lipgloss.NewStyle().Foreground(t.Dim).Italic(true)
		return []string{s.Render(strings.Repeat(" ", 2*nw+5) + l.Text)}
	}

	var lineBg, wordBg, gutterBg color.Color
	sign := " "
	signFg := t.Dim
	switch l.Kind {
	case Add:
		lineBg, wordBg, gutterBg, sign, signFg = t.AddBg, t.AddWord, t.AddGutter, "+", t.AddFg
	case Del:
		lineBg, wordBg, gutterBg, sign, signFg = t.DelBg, t.DelWord, t.DelGutter, "-", t.DelFg
	}
	base := lipgloss.NewStyle()
	gutter := lipgloss.NewStyle().Foreground(t.Dim)
	if lineBg != nil {
		base = base.Background(lineBg)
		gutter = gutter.Background(gutterBg)
	}
	word := base
	if wordBg != nil {
		word = lipgloss.NewStyle().Background(wordBg)
	}
	signStyle := base.Foreground(signFg)

	// Per-rune word-change flags and code colours from the byte ranges.
	type cell struct {
		r  rune
		hi bool
		ts tokStyle
	}
	var cells []cell
	styles := map[chroma.TokenType]tokStyle{}
	for i, r := range l.Text {
		c := cell{r: r}
		for _, s := range l.Spans {
			if i >= s.From && i < s.To {
				c.hi = true
				break
			}
		}
		for _, tk := range l.Syntax {
			if i >= tk.From && i < tk.To {
				ts, ok := styles[tk.Type]
				if !ok {
					ts = t.tokStyle(tk.Type)
					styles[tk.Type] = ts
				}
				c.ts = ts
				break
			}
		}
		cells = append(cells, c)
	}

	// Wrap to the content width.
	var chunks [][]cell
	var cur []cell
	w := 0
	for _, c := range cells {
		rw := runewidth.RuneWidth(c.r)
		if w+rw > cw && len(cur) > 0 {
			chunks = append(chunks, cur)
			cur, w = nil, 0
		}
		cur = append(cur, c)
		w += rw
	}
	chunks = append(chunks, cur)

	out := make([]string, 0, len(chunks))
	for ci, ch := range chunks {
		var b strings.Builder
		if ci == 0 {
			b.WriteString(gutter.Render(num(l.Old) + " " + num(l.New) + " "))
			b.WriteString(signStyle.Render(" " + sign + " "))
		} else {
			b.WriteString(gutter.Render(strings.Repeat(" ", 2*nw+2)))
			b.WriteString(base.Render("   "))
		}
		used := 2*nw + 5
		for i := 0; i < len(ch); {
			j := i
			var seg strings.Builder
			for j < len(ch) && ch[j].hi == ch[i].hi && ch[j].ts == ch[i].ts {
				seg.WriteRune(ch[j].r)
				used += runewidth.RuneWidth(ch[j].r)
				j++
			}
			st := base
			if ch[i].hi {
				st = word
			}
			if ts := ch[i].ts; ts.fg != nil {
				st = st.Foreground(ts.fg)
			}
			if ts := ch[i].ts; ts.bold || ts.italic || ts.underline || ts.strike {
				st = st.Bold(ts.bold).Italic(ts.italic).Underline(ts.underline).Strikethrough(ts.strike)
			}
			b.WriteString(st.Render(seg.String()))
			i = j
		}
		if pad := width - used; pad > 0 {
			b.WriteString(base.Render(strings.Repeat(" ", pad)))
		}
		out = append(out, b.String())
	}
	return out
}

// treeRow is one line of the file tree; file is -1 for directories.
type treeRow struct {
	file  int
	level int
	name  string
}

// buildTree lists files in diff order under their directories, joining
// directory chains that hold nothing else into one row ("internal/tui/").
func buildTree(files []*File) []treeRow {
	var out []treeRow
	type dir struct{ comps []string }
	var stack []dir
	isPrefix := func(p, s []string) bool {
		if len(p) > len(s) {
			return false
		}
		for i := range p {
			if p[i] != s[i] {
				return false
			}
		}
		return true
	}
	for fi, f := range files {
		comps := strings.Split(f.Path(), "/")
		dirs, name := comps[:len(comps)-1], comps[len(comps)-1]
		for len(stack) > 0 && !isPrefix(stack[len(stack)-1].comps, dirs) {
			stack = stack[:len(stack)-1]
		}
		have := 0
		if len(stack) > 0 {
			have = len(stack[len(stack)-1].comps)
		}
		if have < len(dirs) {
			out = append(out, treeRow{file: -1, level: len(stack), name: strings.Join(dirs[have:], "/") + "/"})
			stack = append(stack, dir{comps: dirs})
		}
		out = append(out, treeRow{file: fi, level: len(stack), name: name})
	}
	return out
}
