package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The PR's review threads from GitHub, shown on their lines (◆ in the gutter)
// and opened with r in the same pane as Claude, where a reply can be posted.

type comment struct {
	id      int64
	author  string
	body    string
	created time.Time

	md     string // body rendered as markdown, for mdW columns and mdDark
	mdW    int
	mdDark bool
}

type thread struct {
	file, lo, hi int // File.Lines indexes
	resolved     bool
	comments     []*comment
	scroll       int
	posting      bool
	note         string // the last reply's result
	noteErr      bool
}

type threadsMsg struct {
	threads []rawThread
	err     string
}

type rawThread struct {
	IsResolved    bool   `json:"isResolved"`
	IsOutdated    bool   `json:"isOutdated"`
	Path          string `json:"path"`
	Line          int    `json:"line"`
	StartLine     int    `json:"startLine"`
	DiffSide      string `json:"diffSide"`
	StartDiffSide string `json:"startDiffSide"`
	Comments      struct {
		Nodes []struct {
			DatabaseID int64     `json:"databaseId"`
			Body       string    `json:"body"`
			CreatedAt  time.Time `json:"createdAt"`
			Author     struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"nodes"`
	} `json:"comments"`
}

type replyMsg struct {
	t   *thread
	c   *comment
	err string
}

const threadsQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) {
        nodes {
          isResolved isOutdated path line startLine diffSide startDiffSide
          comments(first: 100) { nodes { databaseId body createdAt author { login } } }
        }
      }
    }
  }
}`

// fetchThreads loads the review threads of a whole-PR diff; a single
// commit's line numbers would not match GitHub's.
func (m *model) fetchThreads() tea.Cmd {
	t := m.target
	owner, name, ok := strings.Cut(t.repo, "/")
	if !ok || t.pr == 0 || t.commit != "" {
		return nil
	}
	return func() tea.Msg {
		out, err := exec.Command("gh", "api", "graphql", "-f", "query="+threadsQuery,
			"-F", "owner="+owner, "-F", "name="+name, "-F", fmt.Sprintf("number=%d", t.pr)).Output()
		if err != nil {
			msg := err.Error()
			if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
				msg = strings.TrimSpace(string(ee.Stderr))
			}
			return threadsMsg{err: msg}
		}
		var res struct {
			Data struct {
				Repository struct {
					PullRequest struct {
						ReviewThreads struct {
							Nodes []rawThread `json:"nodes"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return threadsMsg{err: err.Error()}
		}
		return threadsMsg{threads: res.Data.Repository.PullRequest.ReviewThreads.Nodes}
	}
}

// findLine is the File.Lines index of a line by GitHub's side and number.
func findLine(f *File, side string, n int) int {
	for i, l := range f.Lines {
		if side == "LEFT" && l.Old == n && (l.Kind == Del || l.Kind == Ctx) {
			return i
		}
		if side != "LEFT" && l.New == n && (l.Kind == Add || l.Kind == Ctx) {
			return i
		}
	}
	return -1
}

func (m *model) gotThreads(msg threadsMsg) {
	if msg.err != "" {
		m.flash = "Could not load review threads: " + msg.err
		return
	}
	m.threads = nil
	for _, rt := range msg.threads {
		if rt.IsOutdated || rt.Line == 0 || len(rt.Comments.Nodes) == 0 {
			continue
		}
		for fi, f := range m.files {
			if f.Path() != rt.Path {
				continue
			}
			hi := findLine(f, rt.DiffSide, rt.Line)
			lo := hi
			if rt.StartLine != 0 {
				side := rt.StartDiffSide
				if side == "" {
					side = rt.DiffSide
				}
				if i := findLine(f, side, rt.StartLine); i >= 0 && i <= hi {
					lo = i
				}
			}
			if hi < 0 {
				continue
			}
			t := &thread{file: fi, lo: lo, hi: hi, resolved: rt.IsResolved}
			for _, c := range rt.Comments.Nodes {
				t.comments = append(t.comments, &comment{id: c.DatabaseID, author: c.Author.Login,
					body: strings.ReplaceAll(c.Body, "\t", "    "), created: c.CreatedAt})
			}
			m.threads = append(m.threads, t)
		}
	}
}

// threadsAt are the review threads covering line li of file fi.
func (m *model) threadsAt(fi, li int) []*thread {
	var out []*thread
	for _, t := range m.threads {
		if t.file == fi && li >= t.lo && li <= t.hi {
			out = append(out, t)
		}
	}
	return out
}

func (m *model) openThreads(ts []*thread) {
	m.threadsHere = ts
	m.showThread(ts[len(ts)-1])
}

func (m *model) showThread(t *thread) {
	if c := m.chat; c != nil {
		if len(c.turns) == 0 {
			m.dropConvo(c)
		}
		m.chat, m.here = nil, nil
	}
	m.away = false
	m.thread = t
	m.typing = false
	m.sel = selection{on: true, anchored: true, file: t.file, anchor: t.lo, cursor: t.hi}
	m.relayout()
	m.showCursor()
}

func (m *model) closeThread() {
	m.thread, m.threadsHere, m.typing, m.away = nil, nil, false, false
	m.sel.on = false
	m.relayout()
}

func (m *model) threadKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	t := m.thread
	if m.typing {
		switch k {
		case "enter":
			body := strings.TrimSpace(m.input.String())
			m.typing = false
			m.input.reset()
			if body != "" && !t.posting {
				t.posting, t.note = true, ""
				t.scroll = 1 << 30
				return m.reply(t, body)
			}
		case "esc":
			m.typing = false
			m.input.reset()
		case "ctrl+c":
			return tea.Quit
		case "tab":
			m.toggleSide()
		default:
			m.input.key(k, msg.Key().Text)
		}
		return nil
	}
	switch k {
	case "a", "enter":
		m.typing = true
		m.input.reset()
	case "esc":
		m.closeThread()
	case "q", "ctrl+c":
		return tea.Quit
	case "m":
		m.raw = !m.raw
	case "s":
		m.toggleSide()
	case "<", ",", ">", ".":
		d := 1
		if k == "<" || k == "," {
			d = -1
		}
		for i, o := range m.threadsHere {
			if o == t && i+d >= 0 && i+d < len(m.threadsHere) {
				m.showThread(m.threadsHere[i+d])
				break
			}
		}
	case "up", "k":
		t.scroll--
	case "down", "j":
		t.scroll++
	case "pgup", "b":
		t.scroll -= m.chatRows() - 1
	case "pgdown", "space", "f":
		t.scroll += m.chatRows() - 1
	}
	return nil
}

// reply posts a comment in the thread, as a reply to its first comment.
func (m *model) reply(t *thread, body string) tea.Cmd {
	tg := m.target
	first := t.comments[0].id
	return func() tea.Msg {
		in, _ := json.Marshal(map[string]string{"body": body})
		cmd := exec.Command("gh", "api", "-X", "POST",
			fmt.Sprintf("repos/%s/pulls/%d/comments/%d/replies", tg.repo, tg.pr, first), "--input", "-")
		cmd.Stdin = bytes.NewReader(in)
		out, err := cmd.Output()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
				msg = strings.TrimSpace(string(ee.Stderr)) + " " + msg
			}
			return replyMsg{t: t, err: msg}
		}
		var res struct {
			ID        int64     `json:"id"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
			User      struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		json.Unmarshal(out, &res)
		return replyMsg{t: t, c: &comment{id: res.ID, author: res.User.Login, body: res.Body, created: res.CreatedAt}}
	}
}

func (m *model) gotReply(msg replyMsg) {
	t := msg.t
	t.posting = false
	if msg.err != "" {
		t.note, t.noteErr = "Reply failed: "+msg.err, true
		return
	}
	t.comments = append(t.comments, msg.c)
	t.note, t.noteErr = "", false
	t.scroll = 1 << 30
}

func (m *model) threadView(w, h int) []string {
	th := m.theme
	t := m.thread
	dim := lipgloss.NewStyle().Foreground(th.Dim)

	title := "GitHub · " + m.shortRange(t.file, t.lo, t.hi)
	if n := len(m.threadsHere); n > 1 {
		for i, o := range m.threadsHere {
			if o == t {
				title += fmt.Sprintf(" · %d/%d", i+1, n)
			}
		}
	}
	details := ""
	if t.resolved {
		details = "resolved"
	}
	rule := m.paneRule(w, title, details, th.ModFg)

	wrap := lipgloss.NewStyle().Width(w - 2)
	var body []string
	for i, c := range t.comments {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, lipgloss.NewStyle().Bold(true).Render(c.author)+dim.Render(" · "+c.created.Local().Format("Jan 2 15:04")))
		if m.raw {
			body = append(body, strings.Split(wrap.Render(c.body), "\n")...)
		} else {
			if c.md == "" || c.mdW != w-2 || c.mdDark != m.dark {
				c.md, c.mdW, c.mdDark = m.renderMarkdown(c.body, w-2), w-2, m.dark
			}
			body = append(body, strings.Split(c.md, "\n")...)
		}
	}
	switch {
	case t.posting:
		body = append(body, "", dim.Render("Posting reply…"))
	case t.note != "":
		body = append(body, "")
		body = append(body, strings.Split(wrap.Inherit(lipgloss.NewStyle().Foreground(th.DelFg)).Render(t.note), "\n")...)
	}

	view := h - 1
	t.scroll = min(max(0, t.scroll), max(0, len(body)-view))
	out := []string{rule}
	for i := 0; i < view; i++ {
		line := ""
		if j := t.scroll + i; j < len(body) {
			line = ansi.Truncate(" "+body[j], w, "")
		}
		out = append(out, line)
	}
	return out
}

func (m *model) threadHints() string {
	if m.typing {
		return "enter posts the reply · ^O diff · esc cancel"
	}
	if m.thread.posting {
		return "esc close"
	}
	other := ""
	if len(m.threadsHere) > 1 {
		other = "< > switch · "
	}
	view := "m raw"
	if m.raw {
		view = "m formatted"
	}
	layout := "s side"
	if m.side {
		layout = "s below"
	}
	return "a reply · " + other + view + " · " + layout + " · ^O diff · esc close"
}
