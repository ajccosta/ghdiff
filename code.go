package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// How a new conversation runs Claude ("claude" in settings.json; the first is
// the default). A conversation keeps the way it started.
//   - code: Claude Code itself, the terminal app, in the pane. It can do
//     anything it is allowed to where it runs (a worktree, with w), asking
//     you as usual; ctrl+o switches to the diff, /exit ends it.
//   - print: questions typed in ghdiff, answered with claude -p.
var claudeModes = []string{"code", "print"}

func (m *model) claudeMode() string {
	for _, c := range claudeModes {
		if c == m.Claude {
			return c
		}
	}
	return claudeModes[0]
}

// claudeBin is the Claude Code command; tests swap it.
var claudeBin = "claude"

type termStartedMsg struct {
	c   *convo
	t   *term
	dir string
	err error
}

// termFocused: keys go to a running Claude Code.
func (m *model) termFocused() bool {
	return m.chat != nil && m.chat.term != nil && !m.away && !m.help && !m.confirmPost
}

// ensureTerm starts Claude Code for the pane's conversation if it is one and
// is not running: after the consent question for a new one, resumed
// (--resume) for an old one.
func (m *model) ensureTerm() tea.Cmd {
	c := m.chat
	if c == nil || !c.code || c.term != nil || c.starting || c.startErr != "" || m.consent {
		return nil
	}
	if m.repo != "" && m.access == "" && c.session == "" {
		m.consent = true
		return nil
	}
	c.starting = true
	if c.session == "" {
		c.access = accessFor(m.access).id
	}
	a := accessFor(c.access)
	args := append([]string{"--tools", a.tools, "--append-system-prompt", m.codePrompt(c)}, a.args...)
	if c.session == "" {
		c.session = newUUID()
		args = append(args, "--session-id", c.session)
	} else {
		args = append(args, "--resume", c.session)
	}
	dir := c.dir
	w, h := m.termSize()
	return func() tea.Msg {
		if dir == "" {
			var err error
			if dir, err = a.dir(m, c); err != nil {
				return termStartedMsg{c: c, err: err}
			}
		}
		t, err := startTerm(dir, claudeBin, args, w, h)
		return termStartedMsg{c: c, t: t, dir: dir, err: err}
	}
}

// termSize is a first guess at the pane's size; chatView sets the real one.
func (m *model) termSize() (int, int) {
	if cw := m.chatWidth(); cw > 0 {
		return cw, m.chatRows()
	}
	return m.width, m.chatRows()
}

func (m *model) termStarted(msg termStartedMsg) tea.Cmd {
	c := msg.c
	c.starting = false
	if msg.err != nil {
		c.startErr = msg.err.Error()
		return nil
	}
	c.term, c.dir = msg.t, msg.dir
	m.saveConvos()
	return msg.t.wait()
}

// termDone: Claude Code exited; its pane closes, and o (or ●) resumes it.
func (m *model) termDone(t *term) {
	for _, c := range m.convos {
		if c.term != t {
			continue
		}
		c.term = nil
		m.refreshTurns(c)
		if m.chat == c {
			m.closeChat()
			m.flash = "Claude Code exited · o (or a click on its ●) resumes it"
		} else if len(c.turns) == 0 {
			m.dropConvo(c)
		}
		m.saveConvos()
	}
}

// leave is a conversation going out of the pane: Claude Code keeps running,
// unless nothing was asked yet; then it and the conversation go.
func (m *model) leave(c *convo) {
	m.refreshTurns(c)
	if len(c.turns) > 0 {
		m.saveConvos()
		return
	}
	if c.term != nil {
		c.term.stop()
		c.term = nil
	}
	m.dropConvo(c)
}

// shutdown stops every Claude Code when ghdiff quits, keeping what was said.
func (m *model) shutdown() {
	for _, c := range m.convos {
		if c.term != nil {
			c.term.stop()
			c.term = nil
		}
		m.refreshTurns(c)
	}
	m.saveConvos()
}

func (m *model) codeView(w, h int, rule string) []string {
	c := m.chat
	out := []string{rule}
	if c.term != nil {
		c.term.resize(w, h-1)
		out = append(out, c.term.lines()...)
		if st := m.postStatus(w - 2); len(st) > 0 { // over Claude Code's last lines
			st = st[max(0, len(st)-(h-1)):]
			for i, l := range st {
				out[h-len(st)+i] = ansi.Truncate(" "+l, w, "")
			}
		}
		return out
	}
	st := lipgloss.NewStyle().Width(w - 2)
	var text string
	switch {
	case m.consent:
		text = lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("How may Claude Code use %s? %s", m.repo, consentHints()))
	case c.startErr != "":
		text = st.Foreground(m.theme.DelFg).Render("Could not start Claude Code: " + c.startErr)
	default:
		text = lipgloss.NewStyle().Foreground(m.theme.Dim).Render("Starting Claude Code…")
	}
	for _, l := range strings.Split(st.Render(text), "\n") {
		out = append(out, ansi.Truncate(" "+l, w, ""))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// codePrompt is what Claude Code is told about the diff, the selected lines
// and where it runs, as part of its system prompt.
func (m *model) codePrompt(c *convo) string {
	return "You are running inside ghdiff, a terminal diff viewer, in a pane beside a code diff the user is " +
		"reviewing. They selected some lines (maybe several ranges, in different files) and will ask about " +
		"them or ask you to work on them.\n" + m.diffContext(c, "")
}

// The transcript Claude Code keeps of a session, under ~/.claude/projects.
func transcriptPath(session string) string {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".claude")
	}
	ms, _ := filepath.Glob(filepath.Join(root, "projects", "*", session+".jsonl"))
	if len(ms) == 0 {
		return ""
	}
	return ms[0]
}

// refreshTurns takes a Claude Code conversation's questions and answers from
// its transcript, for its note and for posting.
func (m *model) refreshTurns(c *convo) {
	if !c.code || c.session == "" {
		return
	}
	if ts := transcriptTurns(transcriptPath(c.session)); len(ts) > 0 {
		c.turns = ts
	}
}

// transcriptTurns are the typed prompts in a transcript, each with the text
// Claude wrote after it (not tool calls or their results).
func transcriptTurns(path string) []turn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.IsMeta {
			continue
		}
		text := contentText(e.Message.Content)
		switch {
		case e.Type == "user" && text != "" && !strings.HasPrefix(text, "<") && !strings.HasPrefix(text, "Caveat:"):
			out = append(out, turn{q: text})
		case e.Type == "assistant" && text != "" && len(out) > 0:
			if a := &out[len(out)-1].a; *a != "" {
				*a += "\n\n"
			}
			out[len(out)-1].a += text
		}
	}
	return out
}

// contentText is a message's text: a string, or its text blocks.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &blocks)
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
