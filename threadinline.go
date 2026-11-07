package main

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// In the inline thread mode (the default) each review thread is a box under
// its last line, as on GitHub: its comments in full, or one line when it is
// resolved. A click on it opens the thread to reply (as r does on its lines).

// under is the rows drawn under line li of file fi, from column col.
func (m *model) under(fi, li, col, width int) []row {
	if m.threadMode() != threadsInline {
		return nil
	}
	var rows []row
	for _, t := range m.threads {
		if t.file != fi || t.hi != li || t == m.thread {
			continue
		}
		for _, text := range m.threadBox(t, col, width) {
			rows = append(rows, row{file: fi, line: -1, col: col, thread: t, text: text})
		}
	}
	return rows
}

// threadBox is a thread drawn from column col to width.
func (m *model) threadBox(t *thread, col, width int) []string {
	th := m.theme
	if width-col < 30 {
		col = 1
	}
	pad := strings.Repeat(" ", col)
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	edge := lipgloss.NewStyle().Foreground(th.ModFg)
	w := width - col - 1 // the box, borders included
	if t.resolved {
		c := t.comments[0]
		first, _, _ := strings.Cut(strings.TrimSpace(c.body), "\n")
		line := "✓ Resolved · " + c.author + ": " + first
		return []string{pad + dim.Render(ansi.Truncate(line, w, "…"))}
	}
	bw := w - 4
	var out []string
	title := " " + m.shortRange(t.file, t.lo, t.hi) + " "
	out = append(out, pad+edge.Render("╭─")+dim.Render(ansi.Truncate(title, w-3, "…"))+
		edge.Render(strings.Repeat("─", max(0, w-3-ansi.StringWidth(title)))+"╮"))
	body := m.threadBody(t, bw)
	body = append(body, "", dim.Render("click to reply"))
	for _, l := range body {
		l = ansi.Truncate(l, bw, "")
		out = append(out, pad+edge.Render("│ ")+l+strings.Repeat(" ", max(0, bw-ansi.StringWidth(l)))+edge.Render(" │"))
	}
	out = append(out, pad+edge.Render("╰"+strings.Repeat("─", max(0, w-2))+"╯"))
	return out
}
