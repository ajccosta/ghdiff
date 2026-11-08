package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

type focus int

const (
	focusDiff focus = iota
	focusTree
)

type model struct {
	files     []*File
	title     string
	collapsed map[int]bool

	width, height int
	dark          bool
	truecolor     bool
	theme         Theme

	showTree bool
	focus    focus // focusTree: the left pane has the keys
	left     int   // which left pane shows (leftpane.go)
	treeSel  int   // index into files
	pin      int   // the file n/p/the tree jumped to when the view could not scroll to it, or -1

	rows   []row
	starts []int
	tree   []treeRow
	off    int // first diff row on screen

	sel   selection
	flash string // a one-off note for the footer, cleared by the next key
	help  bool   // the help pane (h or ?) is showing
	drag  drag   // what the held mouse button moves
	// termPress: the held mouse button was pressed over Claude Code.
	termPress bool
	sizes
	chatState
	commitsState
	jump *jump // GHDIFF_GOTO: lines to open at
	expandState
	search searchState
	asking *prompt // a line being typed in the footer (prompt.go)

	ThreadView threadMode // where review threads show (threadview.go)
	Claude     string     // how new conversations run Claude (code.go)
	MdSource   bool       // markdown files show their source (md.go)
	Split      bool       // old and new side by side (split.go)
	mouse      hover
	pop        popup
}

func newModel(files []*File, title string, raw []byte, t target) *model {
	m := &model{
		chatState: chatState{target: t, diff: raw, repo: findRepo(files, t)},
		files:     files,
		title:     title,
		collapsed: deferLarge(files),
		dark:      true,
		truecolor: true,
		showTree:  len(files) > 1 || t.dash,
		pin:       -1,
		tree:      buildTree(files),
		jump:      parseJump(os.Getenv("GHDIFF_GOTO")),
	}
	m.theme = themeFor(m.dark, m.truecolor)
	m.loadConvos()
	m.loadSettings()
	if os.Getenv("GHDIFF_SPLIT") == "1" { // this time only
		m.Split = true
	}
	m.openPane(os.Getenv("GHDIFF_PANE"))
	return m
}

// A big diff opens like GitHub's: files past the first loadBudget diff lines,
// and any file over fileBudget, start collapsed and are only laid out (and
// their words and syntax marked) when opened.
const loadBudget, fileBudget = 4000, 2000

func deferLarge(files []*File) map[int]bool {
	c, n := map[int]bool{}, 0
	for i, f := range files {
		if len(f.Lines) > fileBudget || n+len(f.Lines) > loadBudget {
			c[i] = true
			continue
		}
		n += len(f.Lines)
	}
	return c
}

// load opens file fi if it was left collapsed and not loaded.
func (m *model) load(fi int) {
	if fi >= 0 && fi < len(m.files) && m.collapsed[fi] && !m.files[fi].prepared {
		delete(m.collapsed, fi)
		m.relayout()
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.fetchThreads(), m.fetchCommits())
}

func (m *model) treeWidth() int {
	if (!m.showTree && !m.leftThread()) || m.width < 60 {
		return 0
	}
	if m.TreeW > 0 {
		return max(12, min(m.TreeW, m.width-40))
	}
	return min(40, max(20, m.width/4))
}

func (m *model) diffWidth() int {
	w := m.width
	if tw := m.treeWidth(); tw > 0 {
		w -= tw + 1
	}
	if cw := m.chatWidth(); cw > 0 {
		w -= cw + 1
	}
	return w
}

func (m *model) paneHeight() int { return max(1, m.height-2-m.chatHeight()) }

func (m *model) relayout() {
	if m.width == 0 {
		return
	}
	// Keep the same file and offset into it across a relayout.
	cur, into := m.currentFile(), 0
	if len(m.starts) > 0 {
		into = m.off - m.starts[cur]
	}
	m.rows, m.starts = buildRows(m.theme, m.files, m.collapsed, m.diffWidth(), m)
	if m.sel.on && (m.collapsed[m.sel.cursor.file] || m.collapsed[m.sel.anchor.file]) {
		m.sel = selection{}
	}
	if len(m.starts) > 0 {
		m.off = m.starts[cur] + max(0, into)
	}
	m.clamp()
	m.findHits()
}

func (m *model) maxOff() int {
	if len(m.rows) <= m.paneHeight() {
		return 0
	}
	mo := len(m.rows) - m.paneHeight()
	if n := len(m.starts); n > 0 {
		mo = max(mo, m.starts[n-1])
	}
	return max(0, mo)
}

func (m *model) clamp() { m.off = min(max(0, m.off), m.maxOff()) }

// currentFile is the file whose rows are at the top of the diff pane.
func (m *model) currentFile() int {
	if m.pin >= 0 && m.pin < len(m.starts) {
		return m.pin
	}
	cur := 0
	for i, s := range m.starts {
		if s <= m.off {
			cur = i
		}
	}
	return cur
}

func (m *model) jumpTo(fi int) {
	if fi < 0 || fi >= len(m.starts) {
		return
	}
	m.off = m.starts[fi]
	m.clamp()
	// When the rest fits on screen (files collapsed, or short files at the
	// end) the view stops short of the file; remember it as the current one.
	m.pin = -1
	if m.off != m.starts[fi] {
		m.pin = fi
	}
}

func (m *model) scroll(n int) {
	old := m.off
	m.off += n
	m.clamp()
	if m.off != old {
		m.pin = -1
	}
	if m.focus == focusDiff {
		m.treeSel = m.currentFile()
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.ensureTerm())
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mm, ok := msg.(tea.MouseMsg); ok && m.toTerm(mm) {
		m.mouse = hover{} // no hover box over Claude Code
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		m.applyJump()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.theme = themeFor(m.dark, m.truecolor)
		m.relayout()
	case tea.ColorProfileMsg:
		m.truecolor = msg.Profile == colorprofile.TrueColor
		m.theme = themeFor(m.dark, m.truecolor)
		m.relayout()
	case tea.MouseWheelMsg:
		mo := msg.Mouse()
		step := 3
		if mo.Button == tea.MouseWheelUp {
			step = -3
		}
		if m.inPane(mo.X, mo.Y) {
			m.scrollPane(step)
		} else if mo.X < m.treeWidth() {
			m.leftPane().wheel(m, step/3)
		} else {
			m.scroll(step)
		}
	case tea.MouseClickMsg:
		return m, m.mouseClick(msg)
	case tea.MouseMotionMsg:
		mo := msg.Mouse()
		m.mouse = hover{x: mo.X, y: mo.Y, on: true}
		m.mouseMotion(msg)
	case tea.MouseReleaseMsg:
		m.mouseRelease()
	case answerMsg:
		m.gotAnswer(msg)
	case postedMsg:
		m.gotPosted(msg)
	case commitsMsg:
		m.gotCommits(msg)
	case diffMsg:
		return m, m.gotDiff(msg)
	case textMsg:
		m.gotText(msg)
	case threadsMsg:
		m.gotThreads(msg)
		m.applyJump()
	case replyMsg:
		m.gotReply(msg)
	case commentedMsg:
		return m, m.gotComment(msg)
	case termStartedMsg:
		return m, m.termStarted(msg)
	case termOutMsg:
		return m, msg.t.wait()
	case termDoneMsg:
		m.termDone(msg.t)
	case tea.PasteMsg:
		if m.termFocused() {
			m.chat.term.paste(msg.Content)
			return m, nil
		}
		if m.paneOpen() && m.typing {
			m.input.insert(strings.ReplaceAll(msg.Content, "\n", " "))
		}
	case tea.KeyPressMsg:
		k := msg.String()
		if m.termFocused() {
			if k == "ctrl+o" {
				m.switchFocus()
			} else {
				m.chat.postNote = "" // it covered Claude Code's last lines
				m.chat.term.key(msg)
			}
			return m, nil
		}
		if m.help {
			m.help = false
			return m, nil
		}
		if m.asking != nil {
			return m, m.promptKey(msg)
		}
		if (!m.typing || m.away) && !m.consent && !m.confirmPost {
			if k == "h" || k == "?" {
				m.help, m.flash = true, ""
				return m, nil
			}
			if m.sizeKey(k) {
				return m, nil
			}
			if k == "T" {
				m.cycleThreadMode()
				return m, nil
			}
		}
		if k == "ctrl+o" && m.paneOpen() && !m.consent && !m.confirmPost {
			m.switchFocus()
			return m, nil
		}
		if m.confirmPost && m.chat != nil {
			return m, m.chatKey(msg)
		}
		if m.away {
			return m, m.awayKey(msg.String())
		}
		if m.thread != nil {
			return m, m.threadKey(msg)
		}
		if m.chat != nil {
			return m, m.chatKey(msg)
		}
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *model) key(k string) tea.Cmd {
	page := m.paneHeight()
	m.flash = ""
	if m.searchNav(k) {
		return nil
	}
	if m.sel.on && m.focus == focusDiff {
		if ok, cmd := m.selectKey(k); ok {
			return cmd
		}
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "tab":
		if m.treeWidth() > 0 {
			m.nextFocus()
		}
		return nil
	case "s":
		m.toggleSide()
		return nil
	case "t":
		m.showTree = !m.showTree
		if !m.showTree {
			m.focus = focusDiff
		}
		m.relayout()
		return nil
	case "n", "]":
		m.jumpTo(m.currentFile() + 1)
		m.treeSel = m.currentFile()
		return nil
	case "p", "[":
		cur := m.currentFile()
		if len(m.starts) > 0 && m.off > m.starts[cur] {
			m.jumpTo(cur) // back to the top of this file first
		} else {
			m.jumpTo(cur - 1)
		}
		m.treeSel = m.currentFile()
		return nil
	case "c", "x":
		fi := m.currentFile()
		if m.focus == focusTree && m.left == 0 {
			fi = m.treeSel
		}
		m.collapsed[fi] = !m.collapsed[fi]
		m.relayout()
		m.jumpTo(fi)
		return nil
	case "C", "X":
		all := len(m.collapsed) < len(m.files) || anyFalse(m.collapsed)
		for i := range m.files {
			m.collapsed[i] = all
		}
		m.relayout()
		return nil
	}

	if m.focus == focusTree {
		return m.leftPane().key(m, k)
	}

	switch k {
	case "up", "k":
		m.scroll(-1)
	case "down", "j":
		m.scroll(1)
	case "pgdown", "space", "f":
		m.scroll(page - 1)
	case "pgup", "b":
		m.scroll(-(page - 1))
	case "ctrl+d":
		m.scroll(page / 2)
	case "ctrl+u":
		m.scroll(-page / 2)
	case "home", "g":
		m.scroll(-len(m.rows))
	case "end", "G":
		m.scroll(len(m.rows))
	case "v":
		m.startCursor()
	case "e", "E":
		return m.expandKey(k)
	case "m":
		m.toggleMarkdown()
	case "|":
		m.toggleSplit()
	case "left":
		if m.treeWidth() > 0 {
			m.focus, m.treeSel = focusTree, m.currentFile()
			if !m.leftPane().avail(m) {
				m.left = 0
			}
		}
	}
	return nil
}

func anyFalse(c map[int]bool) bool {
	for _, v := range c {
		if !v {
			return true
		}
	}
	return false
}

func (m *model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion // hovering shows review threads
	if m.width == 0 {
		return v
	}
	t := m.theme
	h := m.paneHeight()

	// Title bar.
	adds, dels := 0, 0
	for _, f := range m.files {
		adds += f.Adds
		dels += f.Dels
	}
	bar := lipgloss.NewStyle().Background(t.HeadBg)
	title := m.title
	if title == "" {
		title = "diff"
	}
	right := counts(t, adds, dels, bar) + bar.Render(" ")
	if len(m.files) > 0 {
		right = bar.Foreground(t.Dim).Render(fmt.Sprintf("file %d/%d  ", m.currentFile()+1, len(m.files))) + right
	}
	changed := fmt.Sprintf(" %d %s changed ", len(m.files), plural(len(m.files), "file", "files"))
	// A long title (a commit's) gives way to the counts.
	title = ansi.Truncate(title, max(10, m.width-ansi.StringWidth(right)-len(changed)-3), "…")
	left := bar.Bold(true).Render(" "+title+" ") + bar.Foreground(t.Dim).Render(changed)
	gap := max(0, m.width-ansi.StringWidth(left)-ansi.StringWidth(right))
	top := ansi.Truncate(left+bar.Render(strings.Repeat(" ", gap))+right, m.width, "")

	// Diff pane, with the current file's header kept at the top.
	diff := make([]string, h)
	cur := m.currentFile()
	curHeader := ""
	if len(m.files) > 0 {
		curHeader = fileHeader(t, m.files[cur], m.collapsed[cur], m.diffWidth(), true, fileNote(m.files[cur], m.collapsed[cur], m.MdSource))
	}
	for i := 0; i < h; i++ {
		if r := m.off + i; r < len(m.rows) {
			diff[i] = m.markSelected(r)
			if m.rows[r].header && m.rows[r].file == cur {
				diff[i] = curHeader
			}
			diff[i] = m.markHits(r, diff[i])
		}
	}
	if len(m.starts) > 0 && m.off > m.starts[cur] && !m.collapsed[cur] {
		diff[0] = curHeader
	}
	if len(m.files) == 0 {
		diff[0] = lipgloss.NewStyle().Foreground(t.Dim).Render("  No changes.")
	}

	// File tree.
	tw := m.treeWidth()
	lines := make([]string, h)
	if tw > 0 {
		left := m.leftLines(tw, h)
		if m.leftThread() && !m.away {
			left = m.leftView(tw, h)
		}
		sep := lipgloss.NewStyle().Foreground(t.Dim).Render("│")
		for i := 0; i < h; i++ {
			cell := ansi.Truncate(left[i], tw, "")
			pad := max(0, tw-ansi.StringWidth(cell))
			lines[i] = cell + strings.Repeat(" ", pad) + sep + diff[i]
		}
	} else {
		lines = diff
	}
	if cw := m.chatWidth(); cw > 0 {
		// Claude on the right: pad the diff out to its width, then the pane.
		sep := lipgloss.NewStyle().Foreground(t.Dim).Render("│")
		chat := m.paneView(cw, h)
		if m.typing {
			// The question is typed at the bottom of the pane, not the screen.
			chat[len(chat)-1] = m.inputLine(cw)
		}
		lw := m.width - cw - 1
		for i := range lines {
			l := ansi.Truncate(lines[i], lw, "")
			lines[i] = l + strings.Repeat(" ", max(0, lw-ansi.StringWidth(l))) + sep + chat[i]
		}
	} else if m.paneShown() {
		lines = append(lines, m.paneView(m.width, m.chatHeight())...)
	}
	m.drawPopup(lines[:h])

	if m.help {
		lines = m.helpView(m.width, len(lines))
	}

	// Footer.
	hints := "v select · n/p next/prev file · c collapse · t tree · q quit"
	if m.usedClaude() {
		hints = "v select · n/p next/prev file · c collapse · t tree · s Claude side/below · q quit"
	}
	if m.away {
		hints = "^O back to Claude · s side/below · v select other lines · n/p file · t tree · q quit"
		if m.leftThread() {
			hints = "t or ^O back to the thread · v select · n/p file · q quit"
		}
		if m.sel.on {
			hints = "^O back to Claude · " + m.selHints()
		}
	} else if m.thread != nil {
		hints = m.threadHints()
	} else if m.chat != nil {
		hints = m.chatHints()
	} else if m.sel.on {
		hints = m.selHints()
		if c := m.sel.cursor; !m.sel.anchored {
			if n := len(m.convosAt(c.file, c.line)); n > 0 {
				hints = fmt.Sprintf("%d Claude %s · o open · ", n, plural(n, "conversation", "conversations")) + hints
			}
			if n := len(m.threadsAt(c.file, c.line)); n > 0 {
				hints = fmt.Sprintf("%d review %s · r read · ", n, plural(n, "thread", "threads")) + hints
			}
		}
	} else if m.focus == focusTree && m.leftPane().name == "Files" {
		hints = "enter open · c collapse · tab next pane · q quit"
	} else if m.focus == focusTree {
		hints = "↑↓ choose · enter show its diff · tab next pane · q quit"
	}
	if !m.away && m.chat == nil && m.thread == nil && !m.sel.on && m.focus == focusDiff {
		hints += " · h help"
	}
	if m.help {
		hints = "any key closes help"
	}
	foot := lipgloss.NewStyle().Foreground(t.Dim).Render(ansi.Truncate(" "+hints, m.width, "…"))
	if m.flash != "" {
		foot = lipgloss.NewStyle().Foreground(t.Accent).Render(" " + m.flash)
	}
	if s := m.searchFoot(); s != "" && m.flash == "" {
		foot = s
	}
	if m.paneShown() && m.typing && !m.sideChat() && !m.away {
		foot = m.inputLine(m.width)
	}

	v.Content = top + "\n" + strings.Join(lines, "\n") + "\n" + foot
	return v
}

func (m *model) treeCell(r treeRow, tw, cur int) string {
	t := m.theme
	indent := strings.Repeat("  ", r.level)
	if r.file < 0 {
		return lipgloss.NewStyle().Foreground(t.Dim).Render(ansi.Truncate(" "+indent+"▾ "+r.name, tw, "…"))
	}
	f := m.files[r.file]
	cnt := fmt.Sprintf(" +%d −%d ", f.Adds, f.Dels)
	nameW := max(1, tw-ansi.StringWidth(indent)-3-len([]rune(cnt)))
	name := ansi.Truncate(r.name, nameW, "…")
	style := lipgloss.NewStyle()
	switch {
	case m.focus == focusTree && r.file == m.treeSel:
		style = style.Reverse(true)
	case r.file == cur:
		style = style.Bold(true).Foreground(t.Accent)
	}
	body := " " + indent + statusMark(t, f.Status, lipgloss.NewStyle()) + " " + style.Render(name)
	pad := max(1, tw-ansi.StringWidth(body)-len([]rune(cnt)))
	return body + strings.Repeat(" ", pad) + lipgloss.NewStyle().Foreground(t.Dim).Render(cnt)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
