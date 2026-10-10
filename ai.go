package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A conversation with Claude about one selection. Questions go to the
// Claude Code CLI (claude -p), which runs on the user's own login; follow-ups
// resume the same Claude session.
type turn struct {
	q, a string
	err  bool
}

type convo struct {
	file, lo, hi int // the selection: File.Lines indexes
	turns        []turn
	session      string
	busy         bool
	scroll       int
	cancel       context.CancelFunc
}

type answerMsg struct {
	c       *convo
	text    string
	session string
	err     bool
}

// target is what the diff is of, when known: from --pr/-R, or from gh-dash
// through GHDIFF_REPO, GHDIFF_PR, GHDIFF_COMMIT and GHDIFF_REPO_PATH.
type target struct {
	repo     string // owner/name
	pr       int
	commit   string
	repoPath string // local clone, from gh-dash's repoPaths
}

func (t target) String() string {
	s := t.repo
	if t.pr > 0 {
		s += fmt.Sprintf("#%d", t.pr)
	}
	if t.commit != "" {
		s += " @ " + t.commit[:min(7, len(t.commit))]
	}
	return s
}

// chatState is the model's part for asking: the open conversation, the
// question being typed, and whether Claude may read the local repo.
type chatState struct {
	target   target
	raw      []byte            // the diff as read, for context
	convos   map[string]*convo // by selection, so reopening one resumes it
	chat     *convo            // open in the bottom pane
	typing   bool
	input    []rune
	repo     string // local clone holding the diff's files, "" if none
	repoOK   int    // 0 not asked yet, 1 yes, -1 no
	consent  bool   // waiting for y/n on reading repo
	pendingQ string // the question held while asking for consent
}

// findRepo returns a local clone Claude can read: gh-dash's repoPaths entry,
// or the git repo around the working directory. Either must hold the diff's
// files, and the latter must have the PR's repo as a remote when it is known.
func findRepo(files []*File, t target) string {
	if p := t.repoPath; p != "" {
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[2:])
			}
		}
		if hasFiles(p, files) {
			return p
		}
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	top := strings.TrimSpace(string(out))
	if t.repo != "" {
		remotes, _ := exec.Command("git", "-C", top, "remote", "-v").Output()
		if !strings.Contains(strings.ToLower(string(remotes)), strings.ToLower(t.repo)) {
			return ""
		}
	}
	if hasFiles(top, files) {
		return top
	}
	return ""
}

func hasFiles(top string, files []*File) bool {
	for _, f := range files {
		if f.Status == "deleted" {
			continue
		}
		if _, err := os.Stat(filepath.Join(top, f.Path())); err == nil {
			return true
		}
	}
	return false
}

func (m *model) chatHeight() int {
	if m.chat == nil {
		return 0
	}
	return max(6, (m.height-2)*2/5)
}

// openChat starts (or reopens) the conversation for the current selection.
func (m *model) openChat() {
	lo, hi := m.sel.bounds()
	k := fmt.Sprintf("%d:%d:%d", m.sel.file, lo, hi)
	c := m.convos[k]
	if c == nil {
		c = &convo{file: m.sel.file, lo: lo, hi: hi}
		m.convos[k] = c
	}
	m.chat, m.typing, m.input = c, true, nil
	m.clamp()
	m.showCursor()
}

func (m *model) closeChat() {
	if m.chat != nil && m.chat.cancel != nil {
		m.chat.cancel()
	}
	m.chat, m.typing, m.consent = nil, false, false
	m.sel.on = false
	m.clamp()
}

// chatKey handles keys while the conversation pane is open.
func (m *model) chatKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch {
	case m.consent:
		switch k {
		case "y", "n":
			m.repoOK = map[string]int{"y": 1, "n": -1}[k]
			m.consent = false
			return m.ask(m.pendingQ)
		case "esc":
			m.consent = false
		}
	case m.typing:
		switch k {
		case "enter":
			q := strings.TrimSpace(string(m.input))
			m.typing, m.input = false, nil
			if q != "" {
				return m.ask(q)
			}
			if len(m.chat.turns) == 0 {
				m.closeChat()
			}
		case "esc":
			m.typing, m.input = false, nil
			if len(m.chat.turns) == 0 {
				m.closeChat()
			}
		case "backspace":
			if n := len(m.input); n > 0 {
				m.input = m.input[:n-1]
			}
		case "ctrl+u":
			m.input = nil
		case "ctrl+c":
			return tea.Quit
		default:
			m.input = append(m.input, []rune(msg.Key().Text)...)
		}
	default:
		switch k {
		case "a", "enter":
			if !m.chat.busy {
				m.typing = true
			}
		case "esc":
			m.closeChat()
		case "q", "ctrl+c":
			return tea.Quit
		case "up", "k":
			m.chat.scroll--
		case "down", "j":
			m.chat.scroll++
		case "pgup", "b":
			m.chat.scroll -= m.chatHeight() - 2
		case "pgdown", "space", "f":
			m.chat.scroll += m.chatHeight() - 2
		}
	}
	return nil
}

// ask sends a question, asking first whether Claude may read the local repo.
func (m *model) ask(q string) tea.Cmd {
	c := m.chat
	if c == nil || c.busy {
		return nil
	}
	if m.repo != "" && m.repoOK == 0 {
		m.consent, m.pendingQ = true, q
		return nil
	}
	c.turns = append(c.turns, turn{q: q})
	c.busy = true
	c.scroll = 1 << 30 // to the end; clamped when drawn

	args := []string{"-p", "--output-format", "json", "--strict-mcp-config"}
	dir := filepath.Join(os.TempDir(), "ghdiff-claude")
	if m.repoOK == 1 {
		dir = m.repo
		args = append(args, "--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob")
	} else {
		os.MkdirAll(dir, 0o755)
		args = append(args, "--tools", "")
	}
	prompt := q
	if c.session != "" {
		args = append(args, "--resume", c.session)
	} else {
		prompt = m.firstPrompt(c, q)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	return func() tea.Msg {
		cmd := exec.CommandContext(ctx, "claude", args...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(prompt)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		var res struct {
			Result    string `json:"result"`
			SessionID string `json:"session_id"`
			IsError   bool   `json:"is_error"`
		}
		if jerr := json.Unmarshal(out.Bytes(), &res); jerr != nil || res.Result == "" {
			msg := strings.TrimSpace(errb.String() + "\n" + out.String())
			if err != nil && msg == "" {
				msg = err.Error()
			}
			if ctx.Err() != nil {
				msg = "cancelled"
			}
			return answerMsg{c: c, text: msg, err: true}
		}
		return answerMsg{c: c, text: res.Result, session: res.SessionID, err: res.IsError}
	}
}

func (m *model) gotAnswer(msg answerMsg) {
	c := msg.c
	c.busy, c.cancel = false, nil
	if msg.session != "" {
		c.session = msg.session
	}
	if n := len(c.turns); n > 0 {
		c.turns[n-1].a, c.turns[n-1].err = strings.TrimSpace(msg.text), msg.err
	}
	// Show the new answer from its question.
	c.scroll = -1
}

const maxDiffContext = 400_000

func (m *model) firstPrompt(c *convo, q string) string {
	f := m.files[c.file]
	var b strings.Builder
	b.WriteString("You are helping someone review a code diff in a terminal diff viewer. " +
		"They selected some lines and have a question about them. Answer concisely in plain text " +
		"for an 80-column terminal: short paragraphs or lists, no headings or tables; short code snippets are fine.\n")
	if t := m.target.String(); t != "" {
		fmt.Fprintf(&b, "The diff is of %s on GitHub.\n", t)
	}
	if m.repoOK == 1 {
		b.WriteString("The working directory is a clone of the repository the diff applies to; you may read files there " +
			"for context. It may not be checked out at the diff's commit, so trust the diff where they differ.\n")
	} else {
		b.WriteString("You only have the diff below; you cannot read other files.\n")
	}
	fmt.Fprintf(&b, "\nFile: %s\n", f.Path())
	for i := c.lo; i >= 0; i-- {
		if f.Lines[i].Kind == Hunk {
			fmt.Fprintf(&b, "Hunk: %s\n", f.Lines[i].Text)
			break
		}
	}
	b.WriteString("Selected lines (old and new line numbers, then +/-/space and the text):\n")
	for _, l := range f.Lines[c.lo : c.hi+1] {
		mark := map[LineKind]string{Add: "+", Del: "-", Ctx: " "}[l.Kind]
		if mark == "" {
			continue
		}
		fmt.Fprintf(&b, "%5s %5s %s%s\n", lineNo(l.Old), lineNo(l.New), mark, l.Text)
	}
	fmt.Fprintf(&b, "\nQuestion: %s\n", q)
	diff := ansiRe.ReplaceAll(m.raw, nil)
	if len(diff) > maxDiffContext {
		b.WriteString("\n(The full diff is too large to include.)\n")
	} else {
		b.WriteString("\nThe full diff, for context:\n\n")
		b.Write(diff)
	}
	return b.String()
}

func lineNo(n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprint(n)
}

// selLabel names a selection like "model.go:L12–L18" by its new-side lines.
func (m *model) selLabel(c *convo) string {
	f := m.files[c.file]
	num := func(l Line) int {
		if l.New != 0 {
			return l.New
		}
		return l.Old
	}
	a, z := num(f.Lines[c.lo]), num(f.Lines[c.hi])
	if a == z {
		return fmt.Sprintf("%s:L%d", f.Path(), a)
	}
	return fmt.Sprintf("%s:L%d–L%d", f.Path(), a, z)
}

// chatView draws the conversation pane: a title rule and the turns.
func (m *model) chatView() []string {
	t := m.theme
	c := m.chat
	h := m.chatHeight()
	w := m.width
	dim := lipgloss.NewStyle().Foreground(t.Dim)

	ctx := "diff only"
	if m.repoOK == 1 {
		ctx = "reading " + filepath.Base(m.repo)
	}
	title := " Claude · " + m.selLabel(c) + " · " + ctx + " "
	rule := dim.Render("──") + lipgloss.NewStyle().Bold(true).Foreground(t.Accent).Render(ansi.Truncate(title, w-4, "…"))
	rule += dim.Render(strings.Repeat("─", max(0, w-ansi.StringWidth(rule))))

	wrap := lipgloss.NewStyle().Width(w - 2)
	var body []string
	start := 0 // first body line of the last turn
	for i, tn := range c.turns {
		if i > 0 {
			body = append(body, "")
		}
		if i == len(c.turns)-1 {
			start = len(body)
		}
		you := lipgloss.NewStyle().Bold(true).Render("You: ") + tn.q
		body = append(body, strings.Split(wrap.Render(you), "\n")...)
		switch {
		case tn.a == "" && c.busy && i == len(c.turns)-1:
			body = append(body, dim.Render("Claude is thinking… (esc closes)"))
		case tn.err:
			body = append(body, strings.Split(wrap.Foreground(t.DelFg).Render("Error: "+tn.a), "\n")...)
		default:
			body = append(body, strings.Split(wrap.Render(tn.a), "\n")...)
		}
	}
	if m.consent {
		body = append(body, "", lipgloss.NewStyle().Bold(true).Render(
			fmt.Sprintf("Let Claude read %s (read-only: Read, Grep, Glob)? y/n", m.repo)))
	}

	view := h - 1
	if c.scroll < 0 {
		c.scroll = start
	}
	c.scroll = min(max(0, c.scroll), max(0, len(body)-view))
	out := []string{rule}
	for i := 0; i < view; i++ {
		line := ""
		if j := c.scroll + i; j < len(body) {
			line = " " + body[j]
		}
		out = append(out, line)
	}
	return out
}

func (m *model) chatHints() string {
	switch {
	case m.consent:
		return "y let Claude read the repo · n diff only · esc cancel"
	case m.typing:
		return ""
	case m.chat.busy:
		return "↑↓ scroll · esc close (stops Claude)"
	}
	return "a ask a follow-up · ↑↓ scroll · esc close"
}

func (m *model) inputLine() string {
	label := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Accent).Render(" Ask Claude: ")
	text := string(m.input)
	room := max(1, m.width-ansi.StringWidth(label)-2)
	if w := ansi.StringWidth(text); w > room {
		text = ansi.TruncateLeft(text, w-room, "…")
	}
	return label + text + lipgloss.NewStyle().Reverse(true).Render(" ")
}
