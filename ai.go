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
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A conversation with Claude about one selection. Questions go to the
// Claude Code CLI (claude -p), which runs on the user's own login; follow-ups
// resume the same Claude session.
type turn struct {
	q, a string
	err  bool

	md     string // a rendered as markdown, for mdW columns and mdDark
	mdW    int
	mdDark bool
}

type convo struct {
	file, lo, hi int // the selection: File.Lines indexes
	turns        []turn
	session      string
	busy         bool
	scroll       int
	cancel       context.CancelFunc
	created      time.Time
	dir          string // where claude runs; a resumed session must run there too
	readRepo     bool   // Claude may read the local clone (dir)

	// Posting to the PR (post.go).
	commentID int64 // the thread's first comment, once posted
	posted    int   // turns already posted
	posting   bool
	postNote  string // the last post's link, or why it failed
	postErr   bool
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
	target      target
	diff        []byte       // the diff as read, for context
	convos      []*convo     // every conversation on this diff, oldest first
	here        []*convo     // the ones on the open conversation's lines, for < >
	unplaced    []savedConvo // saved ones whose lines are not in this diff
	store       string       // the file they are saved in (store.go)
	chat        *convo       // open in the pane
	threads     []*thread    // the PR's review threads (threads.go)
	threadsHere []*thread    // the ones on the open thread's lines, for < >
	thread      *thread      // open in the pane instead of a conversation
	typing      bool
	input       lineInput
	raw         bool   // show answers as Claude wrote them, not rendered
	side        bool   // the pane goes right of the diff, not below it
	repo        string // local clone holding the diff's files, "" if none
	repoOK      int    // 0 not asked yet, 1 yes, -1 no
	consent     bool   // waiting for y/n on reading repo
	confirmPost bool   // waiting for y/n on posting to the PR
	pendingQ    string // the question held while asking for consent
	away        bool   // ctrl+o: the pane stays open but keys go to the diff
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

// sideChat is whether the pane is right of the diff (s toggles); with too
// little room beside the tree for both, it goes below after all.
func (m *model) sideChat() bool { return m.paneOpen() && m.side && m.besideTree() >= 80 }

// besideTree is the width right of the file tree.
func (m *model) besideTree() int {
	if tw := m.treeWidth(); tw > 0 {
		return m.width - tw - 1
	}
	return m.width
}

// paneOpen is whether a Claude conversation or a review thread is shown.
func (m *model) paneOpen() bool { return m.chat != nil || m.thread != nil }

// paneView draws whichever is open.
func (m *model) paneView(w, h int) []string {
	if m.thread != nil {
		return m.threadView(w, h)
	}
	return m.chatView(w, h)
}

func (m *model) chatHeight() int {
	if !m.paneOpen() || m.sideChat() {
		return 0
	}
	return max(6, (m.height-2)*2/5)
}

func (m *model) chatWidth() int {
	if !m.sideChat() {
		return 0
	}
	rest := m.besideTree()
	return min(max(40, rest*2/5), rest-40)
}

// chatRows is how many lines of the conversation are on screen.
func (m *model) chatRows() int {
	if m.sideChat() {
		return m.paneHeight() - 1
	}
	return m.chatHeight() - 1
}

// newChat starts a conversation on the given lines, keeping any earlier ones.
func (m *model) newChat(fi, lo, hi int) {
	c := &convo{file: fi, lo: lo, hi: hi, created: time.Now()}
	m.convos = append(m.convos, c)
	m.here = nil
	for _, o := range m.convos {
		if o.file == fi && o.lo == lo && o.hi == hi && (len(o.turns) > 0 || o == c) {
			m.here = append(m.here, o)
		}
	}
	m.showChat(c)
	m.typing = true
	m.input.reset()
}

// openChats opens the newest of the conversations on a line; < > switch.
func (m *model) openChats(cs []*convo) {
	m.here = cs
	m.showChat(cs[len(cs)-1])
	m.chat.scroll = 0
}

// showChat puts a conversation in the pane and selects its lines.
func (m *model) showChat(c *convo) {
	if old := m.chat; old != nil && old != c {
		if len(old.turns) == 0 {
			m.dropConvo(old)
		}
		m.typing = false
	}
	m.thread, m.threadsHere, m.away = nil, nil, false
	m.chat = c
	m.sel = selection{on: true, anchored: true, file: c.file, anchor: c.lo, cursor: c.hi}
	m.relayout()
	m.showCursor()
}

func (m *model) closeChat() {
	if c := m.chat; c != nil {
		if c.cancel != nil {
			c.cancel()
		}
		if len(c.turns) == 0 {
			m.dropConvo(c)
		}
	}
	m.chat, m.here, m.typing, m.consent, m.away = nil, nil, false, false, false
	m.sel.on = false
	m.relayout()
}

func (m *model) dropConvo(c *convo) {
	for i, o := range m.convos {
		if o == c {
			m.convos = append(m.convos[:i], m.convos[i+1:]...)
			return
		}
	}
}

// switchChat moves to the previous (-1) or next (1) conversation on these lines.
func (m *model) switchChat(d int) {
	for i, c := range m.here {
		if c == m.chat && i+d >= 0 && i+d < len(m.here) {
			if len(m.chat.turns) == 0 {
				return
			}
			m.showChat(m.here[i+d])
			m.chat.scroll = 0
			return
		}
	}
}

// chatKey handles keys while the conversation pane is open.
func (m *model) chatKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch {
	case m.confirmPost:
		m.confirmPost = false
		if k == "y" && m.canPost() {
			m.chat.posting, m.chat.postNote = true, ""
			return m.post()
		}
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
			q := strings.TrimSpace(m.input.String())
			m.typing = false
			m.input.reset()
			if q != "" {
				return m.ask(q)
			}
			if len(m.chat.turns) == 0 {
				m.closeChat()
			}
		case "esc":
			m.typing = false
			m.input.reset()
			if len(m.chat.turns) == 0 {
				m.dropConvo(m.chat)
				if len(m.here) > 1 {
					m.here = m.here[:len(m.here)-1]
					m.showChat(m.here[len(m.here)-1])
				} else {
					m.closeChat()
				}
			}
		case "ctrl+c":
			return tea.Quit
		case "tab":
			m.toggleSide()
		default:
			m.input.key(k, msg.Key().Text)
		}
	default:
		switch k {
		case "a", "enter":
			if !m.chat.busy {
				m.typing = true
			}
		case "esc":
			m.closeChat()
		case "m":
			m.raw = !m.raw
		case "s":
			m.toggleSide()
		case "N":
			if !m.chat.busy {
				m.newChat(m.chat.file, m.chat.lo, m.chat.hi)
			}
		case "<", ",":
			m.switchChat(-1)
		case ">", ".":
			m.switchChat(1)
		case "P":
			switch {
			case m.target.repo == "" || m.target.pr == 0:
				m.chat.postNote, m.chat.postErr = "No PR to post to: open the diff from gh-dash, or with ghdiff --pr N -R owner/repo.", true
			case m.canPost():
				m.confirmPost = true
			}
			m.chat.scroll = 1 << 30
		case "q", "ctrl+c":
			return tea.Quit
		case "up", "k":
			m.chat.scroll--
		case "down", "j":
			m.chat.scroll++
		case "pgup", "b":
			m.chat.scroll -= m.chatRows() - 1
		case "pgdown", "space", "f":
			m.chat.scroll += m.chatRows() - 1
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
	if m.repo != "" && m.repoOK == 0 && c.session == "" {
		m.consent, m.pendingQ = true, q
		return nil
	}
	c.turns = append(c.turns, turn{q: q})
	c.busy = true
	c.scroll = 1 << 30 // to the end; clamped when drawn

	// A new conversation takes the repo answer; a resumed one keeps its own.
	if c.session == "" {
		c.readRepo = m.repoOK == 1
		c.dir = filepath.Join(os.TempDir(), "ghdiff-claude")
		if c.readRepo {
			c.dir = m.repo
		}
	}
	dir := c.dir
	args := []string{"-p", "--output-format", "json", "--strict-mcp-config"}
	if c.readRepo {
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
		// Tabs would be dropped when drawn; Go code is all tabs.
		text := strings.ReplaceAll(strings.TrimSpace(msg.text), "\t", "    ")
		c.turns[n-1].a, c.turns[n-1].err = text, msg.err
	}
	m.saveConvos()
	// Show the new answer from its question.
	c.scroll = -1
}

const maxDiffContext = 400_000

func (m *model) firstPrompt(c *convo, q string) string {
	f := m.files[c.file]
	var b strings.Builder
	b.WriteString("You are helping someone review a code diff in a terminal diff viewer. " +
		"They selected some lines and have a question about them. Answer concisely; your Markdown is rendered " +
		"in a terminal pane about 100 columns wide, so prefer short paragraphs, lists and code blocks over " +
		"headings and wide tables.\n")
	if t := m.target.String(); t != "" {
		fmt.Fprintf(&b, "The diff is of %s on GitHub.\n", t)
	}
	if c.readRepo {
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
	diff := ansiRe.ReplaceAll(m.diff, nil)
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
func (m *model) selLabel(c *convo) string { return m.rangeLabel(c.file, c.lo, c.hi) }

func (m *model) rangeLabel(fi, lo, hi int) string {
	f := m.files[fi]
	num := func(l Line) int {
		if l.New != 0 {
			return l.New
		}
		return l.Old
	}
	a, z := num(f.Lines[lo]), num(f.Lines[hi])
	if a == z {
		return fmt.Sprintf("%s:L%d", f.Path(), a)
	}
	return fmt.Sprintf("%s:L%d–L%d", f.Path(), a, z)
}

// chatView draws the conversation pane: a title rule and the turns.
func (m *model) chatView(w, h int) []string {
	t := m.theme
	c := m.chat
	dim := lipgloss.NewStyle().Foreground(t.Dim)

	ctx := "diff only"
	if c.readRepo || (c.session == "" && m.repoOK == 1) {
		ctx = "reading " + filepath.Base(m.repo)
	}
	title := " Claude · " + m.selLabel(c) + " · " + ctx + " "
	if n := len(m.here); n > 1 {
		for i, o := range m.here {
			if o == c {
				title += fmt.Sprintf("· conversation %d/%d ", i+1, n)
			}
		}
	}
	if !c.created.IsZero() {
		title += "· " + c.created.Format("Jan 2 15:04") + " "
	}
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
		case m.raw:
			body = append(body, strings.Split(wrap.Render(tn.a), "\n")...)
		default:
			body = append(body, strings.Split(m.markdown(&c.turns[i], w-2), "\n")...)
		}
	}
	switch {
	case m.confirmPost:
		what := "this conversation"
		if c.posted > 0 {
			what = "the new questions and answers, as a reply"
		}
		body = append(body, "", lipgloss.NewStyle().Bold(true).Render(
			fmt.Sprintf("Post %s to %s on %s? y/n", what, m.target, m.selLabel(c))))
	case c.posting:
		body = append(body, "", dim.Render("Posting to GitHub…"))
	case c.postNote != "":
		st := dim
		if c.postErr {
			st = lipgloss.NewStyle().Foreground(t.DelFg)
		}
		body = append(body, "")
		body = append(body, strings.Split(wrap.Inherit(st).Render(c.postNote), "\n")...)
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
			line = ansi.Truncate(" "+body[j], w, "")
		}
		out = append(out, line)
	}
	return out
}

// switchFocus (ctrl+o) moves the keys from the pane to the diff, where they
// scroll and select as usual with the pane left open, and back.
func (m *model) switchFocus() {
	if m.away {
		m.backToPane()
		return
	}
	m.away, m.flash = true, ""
}

// backToPane gives the keys back to the pane, selecting its lines again.
func (m *model) backToPane() {
	m.away, m.focus = false, focusDiff
	fi, lo, hi := m.paneLines()
	m.sel = selection{on: true, anchored: true, file: fi, anchor: lo, cursor: hi}
	m.showCursor()
}

func (m *model) paneLines() (file, lo, hi int) {
	if t := m.thread; t != nil {
		return t.file, t.lo, t.hi
	}
	return m.chat.file, m.chat.lo, m.chat.hi
}

// awayKey is a key pressed in the diff while the pane is open. a, o and r
// open a pane on what is selected, and esc with nothing selected goes back.
func (m *model) awayKey(k string) tea.Cmd {
	if k == "esc" && !m.sel.on {
		m.backToPane()
		return nil
	}
	return m.key(k)
}

func (m *model) chatHints() string {
	switch {
	case m.confirmPost:
		return "y post · any other key cancels"
	case m.consent:
		return "y let Claude read the repo · n diff only · esc cancel"
	case m.typing:
		return "enter send · ^O diff · esc cancel"
	case m.chat.busy:
		return "↑↓ scroll · esc close (stops Claude)"
	}
	view := "m raw"
	if m.raw {
		view = "m formatted"
	}
	post := ""
	if m.canPost() {
		post = "P post to PR · "
	}
	other := ""
	if len(m.here) > 1 {
		other = "< > switch · "
	}
	layout := "s side"
	if m.side {
		layout = "s below"
	}
	return "a follow-up · N new · " + other + "↑↓ scroll · " + view + " · " + layout + " · " + post + "^O diff · esc close"
}

// markdown renders an answer, caching it until the width or theme changes.
func (m *model) markdown(tn *turn, w int) string {
	if tn.md != "" && tn.mdW == w && tn.mdDark == m.dark {
		return tn.md
	}
	tn.md, tn.mdW, tn.mdDark = m.renderMarkdown(tn.a, w), w, m.dark
	return tn.md
}

func (m *model) renderMarkdown(text string, w int) string {
	style := "light"
	if m.dark {
		style = "dark"
	}
	if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(w)); err == nil {
		if s, err := r.Render(text); err == nil {
			return strings.Trim(s, "\n")
		}
	}
	return text
}

// toggleSide moves the Claude pane between right of the diff and below it;
// with no conversation open, it picks where the next one opens.
func (m *model) toggleSide() {
	m.side = !m.side
	m.saveSettings()
	if !m.paneOpen() {
		m.flash = "Claude pane will open below the diff"
		if m.side {
			m.flash = "Claude pane will open right of the diff"
		}
		return
	}
	m.relayout()
	m.showCursor()
}

func (m *model) inputLine(w int) string {
	what := " Ask Claude: "
	if m.thread != nil {
		what = " Reply: "
	}
	label := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Accent).Render(what)
	if m.away {
		// Keys are in the diff: the draft, dimmed and without a cursor.
		label = lipgloss.NewStyle().Foreground(m.theme.Dim).Render(what)
		draft := ansi.Truncate(m.input.String(), max(1, w-ansi.StringWidth(label)-1), "…")
		return label + lipgloss.NewStyle().Foreground(m.theme.Dim).Render(draft)
	}
	return label + m.input.view(max(1, w-ansi.StringWidth(label)-1))
}
