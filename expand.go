package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// The lines a diff leaves out (above each @@, and after the last hunk) can
// be shown: e shows or hides the ones at the cursor (or the first on screen),
// E all of the file's, and a click on an @@ row (or the ↕ row at a file's
// end) does what e does on it. They come from the first of textSources whose
// text agrees with the diff's own lines, and are not selectable: GitHub
// takes comments only on lines in the diff.

// fileText is one side of a file, whole.
type fileText struct {
	lines  []string // without line ends or tab expansion
	syntax [][]tok
	old    bool // the old side: Line.Old numbers it, else Line.New
}

type expandState struct {
	texts   map[*File]*fileText
	failed  map[*File]string // why its text could not be had
	reading map[*File]bool   // being fetched
	open    map[pos]bool     // the hunk rows whose hidden lines show; line len(Lines) is the file's end
}

// textSource reads one side of f; a new place to read files from is one
// more entry. Each runs off the UI thread, so it gets values, not the model.
type textSource struct {
	old  bool
	read func(repo string, t target, f *File) ([]byte, error)
}

var errNoSource = errors.New("not here")

var textSources = []textSource{
	{true, func(repo string, _ target, f *File) ([]byte, error) { return gitBlob(repo, f.OldBlob) }},
	{false, func(repo string, _ target, f *File) ([]byte, error) { return gitBlob(repo, f.NewBlob) }},
	{false, func(repo string, _ target, f *File) ([]byte, error) { // git diff of the working tree
		if repo == "" {
			return nil, errNoSource
		}
		return os.ReadFile(filepath.Join(repo, f.New))
	}},
	{false, githubText},
}

func gitBlob(repo, blob string) ([]byte, error) {
	if blob == "" || strings.Trim(blob, "0") == "" {
		return nil, errNoSource
	}
	args := []string{"cat-file", "-p", blob}
	if repo != "" {
		args = append([]string{"-C", repo}, args...)
	}
	return exec.Command("git", args...).Output()
}

// githubText reads the new side at the commit, or at the PR's head.
func githubText(_ string, t target, f *File) ([]byte, error) {
	if t.repo == "" || (t.commit == "" && t.pr == 0) {
		return nil, errNoSource
	}
	ref := t.commit
	if ref == "" {
		var err error
		if ref, err = prHead(t); err != nil {
			return nil, err
		}
	}
	segs := strings.Split(f.New, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return exec.Command("gh", "api", "-H", "Accept: application/vnd.github.raw",
		fmt.Sprintf("repos/%s/contents/%s?ref=%s", t.repo, strings.Join(segs, "/"), ref)).Output()
}

func prHead(t target) (string, error) {
	out, err := exec.Command("gh", "pr", "view", fmt.Sprint(t.pr), "-R", t.repo,
		"--json", "headRefOid", "-q", ".headRefOid").Output()
	return strings.TrimSpace(string(out)), err
}

type textMsg struct {
	f    *File
	text *fileText
	err  string
}

// fetchText tries each source in turn and keeps the first that agrees.
func (m *model) fetchText(f *File) tea.Cmd {
	repo, t := m.repo, m.target
	return func() tea.Msg {
		for _, s := range textSources {
			if s.old && f.Status == "added" || !s.old && f.Status == "deleted" {
				continue
			}
			b, err := s.read(repo, t, f)
			if err != nil {
				continue
			}
			lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
			for i, l := range lines {
				lines[i] = strings.TrimSuffix(l, "\r")
			}
			if agrees(f, lines, s.old) {
				return textMsg{f: f, text: &fileText{lines: lines, syntax: syntaxOf(f, lines), old: s.old}}
			}
		}
		return textMsg{f: f, err: "the rest of " + f.Path() + " could not be read here"}
	}
}

// agrees: every line of the diff on that side is the file's line there.
func agrees(f *File, lines []string, old bool) bool {
	for _, l := range f.Lines {
		n := l.New
		if old {
			n = l.Old
		}
		if l.Kind == Hunk || l.Kind == Note || n == 0 {
			continue
		}
		if n > len(lines) || lines[n-1] != l.Raw {
			return false
		}
	}
	return true
}

func syntaxOf(f *File, text []string) [][]tok {
	lexer := lexers.Match(f.Path())
	if lexer == nil {
		return nil
	}
	lines := make([]Line, len(text))
	for i, s := range text {
		lines[i] = Line{Text: expandTabs(s)}
	}
	all := func(Line) bool { return true }
	highlightSide(chroma.Coalesce(lexer), lines, all, all)
	out := make([][]tok, len(lines))
	for i, l := range lines {
		out[i] = l.Syntax
	}
	return out
}

// before is the last old and new line numbers before line index li.
func before(f *File, li int) (int, int) {
	o, n := 0, 0
	for _, l := range f.Lines[:li] {
		o, n = max(o, l.Old), max(n, l.New)
	}
	return o, n
}

// hidden is how many lines the diff leaves out above Lines[li] (an @@ row),
// or after the last hunk when li is len(Lines); -1 when not known yet.
func (m *model) hidden(f *File, li int) int {
	if f.Status == "binary" || f.Status == "added" || f.Status == "deleted" {
		return 0
	}
	o, n := before(f, li)
	if li < len(f.Lines) {
		if f.Lines[li].Kind != Hunk {
			return 0
		}
		h := hunkRe.FindStringSubmatch(f.Lines[li].Text)
		if h == nil {
			return 0
		}
		var start int
		fmt.Sscan(h[2], &start)
		return max(0, start-1-n)
	}
	ft := m.texts[f]
	if ft == nil {
		if len(f.Lines) == 0 || m.failed[f] != "" {
			return 0
		}
		return -1
	}
	if ft.old {
		return max(0, len(ft.lines)-o)
	}
	return max(0, len(ft.lines)-n)
}

// gap is the hidden lines above Lines[li] when they show, as context lines.
func (m *model) gap(fi, li int) []Line {
	f := m.files[fi]
	ft := m.texts[f]
	if !m.open[pos{fi, li}] || ft == nil {
		return nil
	}
	o, n := before(f, li)
	var out []Line
	for k := 1; k <= m.hidden(f, li); k++ {
		i := n + k - 1
		if ft.old {
			i = o + k - 1
		}
		if i >= len(ft.lines) {
			break
		}
		l := Line{Kind: Ctx, Old: o + k, New: n + k, Text: expandTabs(ft.lines[i]), Raw: ft.lines[i]}
		if i < len(ft.syntax) {
			l.Syntax = ft.syntax[i]
		}
		out = append(out, l)
	}
	return out
}

// expandLabel is what an @@ row (or the end row) says about its hidden lines.
func (m *model) expandLabel(fi, li int) string {
	f := m.files[fi]
	k := m.hidden(f, li)
	switch {
	case m.reading[f]:
		return "↕ loading…"
	case k == 0:
		return ""
	case m.open[pos{fi, li}] && m.texts[f] != nil:
		return fmt.Sprintf("▴ hide %d %s", k, plural(k, "line", "lines"))
	case k < 0:
		return "↕ show the rest of the file"
	}
	return fmt.Sprintf("↕ %d hidden %s", k, plural(k, "line", "lines"))
}

// toggleGap shows or hides the lines above Lines[li] (or the file's end).
func (m *model) toggleGap(fi, li int) tea.Cmd {
	f := m.files[fi]
	p := pos{fi, li}
	if m.hidden(f, li) == 0 {
		return nil
	}
	if m.open == nil {
		m.open, m.texts, m.failed, m.reading = map[pos]bool{}, map[*File]*fileText{}, map[*File]string{}, map[*File]bool{}
	}
	m.open[p] = !m.open[p]
	switch {
	case !m.open[p] || m.texts[f] != nil:
	case m.failed[f] != "":
		m.open[p] = false
		m.flash = m.failed[f]
	case !m.reading[f]:
		m.reading[f] = true
		m.relayout()
		return m.fetchText(f)
	}
	m.relayout()
	return nil
}

func (m *model) gotText(msg textMsg) {
	if m.reading == nil || !m.reading[msg.f] {
		return // another diff is showing now
	}
	delete(m.reading, msg.f)
	if msg.err != "" {
		m.failed[msg.f] = msg.err
		m.flash = msg.err
		for p := range m.open {
			if m.files[p.file] == msg.f {
				delete(m.open, p)
			}
		}
	} else {
		m.texts[msg.f] = msg.text
	}
	m.relayout()
}

// expandKey is e (the gap at the cursor, or the first on screen) and E
// (every gap in the current file, shown or hidden together).
func (m *model) expandKey(k string) tea.Cmd {
	if k == "E" {
		fi := m.currentFile()
		f := m.files[fi]
		var gaps []int
		show := false
		for li := 0; li <= len(f.Lines); li++ {
			if m.hidden(f, li) != 0 {
				gaps = append(gaps, li)
				show = show || !m.open[pos{fi, li}]
			}
		}
		var cmds []tea.Cmd
		for _, li := range gaps {
			if m.open[pos{fi, li}] != show {
				cmds = append(cmds, m.toggleGap(fi, li))
			}
		}
		return tea.Batch(cmds...)
	}
	if m.sel.on { // the hunk the cursor is in
		c := m.sel.cursor
		for li := c.line; li >= 0; li-- {
			if m.hidden(m.files[c.file], li) != 0 {
				return m.toggleGap(c.file, li)
			}
		}
	}
	for r := m.off; r < min(len(m.rows), m.off+m.paneHeight()); r++ {
		if fi, li, ok := m.gapRow(r); ok {
			return m.toggleGap(fi, li)
		}
	}
	m.flash = "No hidden lines on screen"
	return nil
}

// gapRow: row r shows or hides hidden lines (an @@ row or a file's end row).
func (m *model) gapRow(r int) (int, int, bool) {
	if r < 0 || r >= len(m.rows) {
		return 0, 0, false
	}
	w := m.rows[r]
	li := w.line
	if w.tail {
		li = len(m.files[w.file].Lines)
	} else if li < 0 {
		return 0, 0, false
	}
	return w.file, li, m.hidden(m.files[w.file], li) != 0
}
