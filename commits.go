package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The Commits pane, for a PR opened from gh-dash: "All commits" (the whole
// PR, when it has more than one) and each commit, oldest first. Opening one
// shows its diff instead.

type prCommit struct {
	sha, title, author string
	date               time.Time
}

type commitsState struct {
	commits    []prCommit
	commitsErr string
	commitSel  int               // the entry under the cursor
	loading    *string           // the commit being fetched ("" all), nil if none
	diffs      map[string][]byte // fetched diffs by commit ("" all)
}

type commitsMsg struct {
	commits []prCommit
	err     string
}

type diffMsg struct {
	sha string
	raw []byte
	err string
}

func ghErr(err error) string {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return strings.TrimSpace(string(ee.Stderr))
	}
	return err.Error()
}

func (m *model) fetchCommits() tea.Cmd {
	t := m.target
	if !t.dash {
		return nil
	}
	return func() tea.Msg {
		out, err := exec.Command("gh", "api", "--paginate",
			fmt.Sprintf("repos/%s/pulls/%d/commits?per_page=100", t.repo, t.pr),
			"--jq", `.[] | [.sha, (.commit.message | split("\n")[0]), (.author.login // .commit.author.name), .commit.author.date] | @tsv`,
		).Output()
		if err != nil {
			return commitsMsg{err: ghErr(err)}
		}
		var cs []prCommit
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.Split(l, "\t")
			if len(f) < 4 {
				continue
			}
			d, _ := time.Parse(time.RFC3339, f[3])
			cs = append(cs, prCommit{sha: f[0], title: f[1], author: f[2], date: d})
		}
		return commitsMsg{commits: cs}
	}
}

func (m *model) gotCommits(msg commitsMsg) {
	m.commits, m.commitsErr = msg.commits, msg.err
	m.commitSel = m.shownEntry()
}

// An entry of the pane: a commit, or all of them ("").
func (m *model) entries() []string {
	var out []string
	if len(m.commits) > 1 {
		out = append(out, "")
	}
	for _, c := range m.commits {
		out = append(out, c.sha)
	}
	return out
}

// shownEntry is the entry whose diff is showing: the commit, or all.
func (m *model) shownEntry() int {
	es := m.entries()
	for i, e := range es {
		if e == m.target.commit {
			return i
		}
	}
	return 0 // a PR of one commit: its diff is the PR's
}

func (m *model) commitAt(sha string) *prCommit {
	for i := range m.commits {
		if m.commits[i].sha == sha {
			return &m.commits[i]
		}
	}
	return nil
}

func (m *model) commitLines(w, h int) []string {
	t := m.theme
	dim := lipgloss.NewStyle().Foreground(t.Dim)
	out := make([]string, h)
	switch {
	case m.commitsErr != "":
		return wrapRows(lipgloss.NewStyle().Foreground(t.DelFg), "Could not load the commits: "+m.commitsErr, w, h)
	case m.commits == nil:
		return wrapRows(dim, "Loading commits…", w, h)
	}
	es, shown, top := m.entries(), m.shownEntry(), m.commitTop()
	for i := range out {
		e := top + i
		if e >= len(es) {
			break
		}
		mark, label, right := " ", "", ""
		if es[e] == "" {
			label = fmt.Sprintf("All commits (%d)", len(m.commits))
		} else {
			c := m.commitAt(es[e])
			label = dim.Render(c.sha[:7]) + " " + c.title
			right = ago(c.date)
		}
		if m.loading != nil && *m.loading == es[e] {
			right = "…"
		}
		st := lipgloss.NewStyle()
		switch {
		case m.focus == focusTree && e == m.commitSel:
			st = st.Reverse(true)
		case e == shown:
			st = st.Bold(true).Foreground(t.Accent)
		}
		if e == shown {
			mark = "▸"
		}
		body := ansi.Truncate(" "+mark+" "+label, max(1, w-len(right)-1), "…")
		pad := max(1, w-ansi.StringWidth(body)-len(right))
		out[i] = st.Render(body) + strings.Repeat(" ", pad) + dim.Render(right)
	}
	return out
}

func wrapRows(st lipgloss.Style, s string, w, h int) []string {
	out := make([]string, h)
	for i, l := range strings.Split(st.Width(max(1, w-2)).Render(s), "\n") {
		if i < h {
			out[i] = " " + l
		}
	}
	return out
}

// ago is a short age: 5m, 3h, 2d, or the date.
func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch d := time.Since(t); {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return t.Local().Format("Jan 2")
}

// commitTop is the first entry on screen, chosen so the cursor is visible.
func (m *model) commitTop() int {
	h := m.paneHeight() - m.leftHead()
	return min(max(0, m.commitSel-h/2), max(0, len(m.entries())-h))
}

func (m *model) moveCommit(d int) {
	m.commitSel = min(max(0, m.commitSel+d), len(m.entries())-1)
}

func (m *model) commitKey(k string) tea.Cmd {
	switch k {
	case "up", "k":
		m.moveCommit(-1)
	case "down", "j":
		m.moveCommit(1)
	case "home", "g":
		m.moveCommit(-len(m.entries()))
	case "end", "G":
		m.moveCommit(len(m.entries()))
	case "right", "l", "enter":
		return m.openEntry(m.commitSel)
	}
	return nil
}

func (m *model) commitClick(row int) tea.Cmd {
	e := m.commitTop() + row
	if e >= len(m.entries()) {
		return nil
	}
	m.commitSel, m.focus = e, focusTree
	return m.openEntry(e)
}

// openEntry shows entry e's diff, fetching it first if needed.
func (m *model) openEntry(e int) tea.Cmd {
	es := m.entries()
	if e < 0 || e >= len(es) || e == m.shownEntry() {
		return nil
	}
	sha := es[e]
	if raw, ok := m.diffs[sha]; ok {
		return m.showDiff(sha, raw)
	}
	m.loading = &sha
	t := m.target
	return func() tea.Msg {
		var out []byte
		var err error
		if sha == "" {
			out, err = exec.Command("gh", "pr", "diff", fmt.Sprint(t.pr), "-R", t.repo, "--color", "never").Output()
		} else {
			out, err = exec.Command("gh", "api", "-H", "Accept: application/vnd.github.diff",
				fmt.Sprintf("repos/%s/commits/%s", t.repo, sha)).Output()
		}
		if err != nil {
			return diffMsg{sha: sha, err: ghErr(err)}
		}
		return diffMsg{sha: sha, raw: out}
	}
}

func (m *model) gotDiff(msg diffMsg) tea.Cmd {
	if m.loading == nil || *m.loading != msg.sha {
		return nil // another was picked since
	}
	m.loading = nil
	if msg.err != "" {
		m.flash = "Could not load the diff: " + msg.err
		return nil
	}
	return m.showDiff(msg.sha, msg.raw)
}

// diffTitle names the PR, and the commit when one is shown.
func (m *model) diffTitle(sha string) string {
	s := fmt.Sprintf("%s#%d", m.target.repo, m.target.pr)
	if c := m.commitAt(sha); c != nil {
		s += " @ " + c.sha[:7] + " · " + c.title
	}
	return s
}

// showDiff puts commit sha's diff (all of them for "") in place of this one.
// What is on screen stays (sizes, panes, focus); conversations are saved and
// placed again on the new diff, and a running Claude Code goes with its
// conversation if that is in the new diff too, else it stops (it resumes
// when its conversation is opened again).
func (m *model) showDiff(sha string, raw []byte) tea.Cmd {
	if m.diffs == nil {
		m.diffs = map[string][]byte{}
	}
	m.diffs[m.target.commit] = m.diff
	m.diffs[sha] = raw

	for _, c := range m.convos {
		m.refreshTurns(c)
	}
	m.saveConvos()
	t := m.target
	t.commit = sha
	nm := newModel(Parse(bytes.NewReader(raw)), m.diffTitle(sha), raw, t)
	nm.jump = nil
	nm.width, nm.height, nm.dark, nm.truecolor, nm.theme = m.width, m.height, m.dark, m.truecolor, m.theme
	nm.showTree, nm.focus, nm.left, nm.commitsState = m.showTree, m.focus, m.left, m.commitsState
	nm.access, nm.raw = m.access, m.raw

	for _, oc := range m.convos {
		if oc.term == nil {
			continue
		}
		var nc *convo
		for _, c := range nm.convos {
			if c.session == oc.session {
				nc = c
			}
		}
		if nc == nil {
			oc.term.stop()
			continue
		}
		nc.term, nc.dir, nc.code = oc.term, oc.dir, oc.code
		if m.chat == oc {
			nm.openChats([]*convo{nc})
		}
	}
	*m = *nm
	m.relayout()
	return m.fetchThreads()
}
