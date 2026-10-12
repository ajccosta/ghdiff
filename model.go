package main

import (
	"fmt"
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
	focus    focus
	treeSel  int // index into files

	rows   []row
	starts []int
	tree   []treeRow
	off    int // first diff row on screen

	sel   selection
	flash string // a one-off note for the footer, cleared by the next key
	chatState
}

func newModel(files []*File, title string, raw []byte, t target) *model {
	m := &model{
		chatState: chatState{target: t, diff: raw, repo: findRepo(files, t)},
		files:     files,
		title:     title,
		collapsed: map[int]bool{},
		dark:      true,
		truecolor: true,
		showTree:  len(files) > 1,
		tree:      buildTree(files),
	}
	m.theme = themeFor(m.dark, m.truecolor)
	m.loadConvos()
	m.loadSettings()
	return m
}

func (m *model) Init() tea.Cmd { return tea.Batch(tea.RequestBackgroundColor, m.fetchThreads()) }

func (m *model) treeWidth() int {
	if !m.showTree || m.width < 60 {
		return 0
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
	m.rows, m.starts = buildRows(m.theme, m.files, m.collapsed, m.diffWidth())
	if m.sel.on && m.collapsed[m.sel.file] {
		m.sel.on = false
	}
	if len(m.starts) > 0 {
		m.off = m.starts[cur] + max(0, into)
	}
	m.clamp()
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
}

func (m *model) scroll(n int) {
	m.off += n
	m.clamp()
	if m.focus == focusDiff {
		m.treeSel = m.currentFile()
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
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
		if mo.X < m.treeWidth() {
			m.moveTree(step / 3)
		} else {
			m.scroll(step)
		}
	case tea.MouseClickMsg:
		mo := msg.Mouse()
		if mo.X < m.treeWidth() {
			if i := m.treeTop() + mo.Y - 1; mo.Y >= 1 && i < len(m.tree) && m.tree[i].file >= 0 {
				m.treeSel = m.tree[i].file
				m.jumpTo(m.treeSel)
			}
		}
	case answerMsg:
		m.gotAnswer(msg)
	case postedMsg:
		m.gotPosted(msg)
	case threadsMsg:
		m.gotThreads(msg)
	case replyMsg:
		m.gotReply(msg)
	case tea.PasteMsg:
		if m.paneOpen() && m.typing {
			m.input.insert(strings.ReplaceAll(msg.Content, "\n", " "))
		}
	case tea.KeyPressMsg:
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

func (m *model) moveTree(d int) {
	m.treeSel = min(max(0, m.treeSel+d), len(m.files)-1)
	m.jumpTo(m.treeSel)
}

func (m *model) key(k string) tea.Cmd {
	page := m.paneHeight()
	m.flash = ""
	if m.sel.on && m.focus == focusDiff {
		if ok, cmd := m.selectKey(k); ok {
			return cmd
		}
	}
	switch k {
	case "q", "esc", "ctrl+c":
		return tea.Quit
	case "tab":
		if m.treeWidth() > 0 {
			if m.focus == focusTree {
				m.focus = focusDiff
			} else {
				m.focus, m.treeSel = focusTree, m.currentFile()
			}
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
		if m.focus == focusTree {
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
		switch k {
		case "up", "k":
			m.moveTree(-1)
		case "down", "j":
			m.moveTree(1)
		case "home", "g":
			m.moveTree(-len(m.files))
		case "end", "G":
			m.moveTree(len(m.files))
		case "right", "l", "enter":
			m.jumpTo(m.treeSel)
			m.focus = focusDiff
		}
		return nil
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
	case "left", "h":
		if m.treeWidth() > 0 {
			m.focus, m.treeSel = focusTree, m.currentFile()
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

// treeTop is the first tree row on screen, chosen so the selection is visible.
func (m *model) treeTop() int {
	sel := 0
	for i, r := range m.tree {
		if r.file == m.treeSel {
			sel = i
		}
	}
	h := m.paneHeight()
	return min(max(0, sel-h/2), max(0, len(m.tree)-h))
}

func (m *model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
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
	left := bar.Bold(true).Render(" "+title+" ") +
		bar.Foreground(t.Dim).Render(fmt.Sprintf(" %d %s changed ", len(m.files), plural(len(m.files), "file", "files")))
	right := counts(t, adds, dels, bar) + bar.Render(" ")
	if len(m.files) > 0 {
		right = bar.Foreground(t.Dim).Render(fmt.Sprintf("file %d/%d  ", m.currentFile()+1, len(m.files))) + right
	}
	gap := max(0, m.width-ansi.StringWidth(left)-ansi.StringWidth(right))
	top := ansi.Truncate(left+bar.Render(strings.Repeat(" ", gap))+right, m.width, "")

	// Diff pane, with the current file's header kept at the top.
	diff := make([]string, h)
	for i := 0; i < h; i++ {
		if r := m.off + i; r < len(m.rows) {
			diff[i] = m.markSelected(r)
		}
	}
	if len(m.starts) > 0 {
		cur := m.currentFile()
		if m.off > m.starts[cur] && !m.collapsed[cur] {
			diff[0] = m.rows[m.starts[cur]].text
		}
	}
	if len(m.files) == 0 {
		diff[0] = lipgloss.NewStyle().Foreground(t.Dim).Render("  No changes.")
	}

	// File tree.
	tw := m.treeWidth()
	lines := make([]string, h)
	if tw > 0 {
		cur := m.currentFile()
		tt := m.treeTop()
		sep := lipgloss.NewStyle().Foreground(t.Dim).Render("│")
		for i := 0; i < h; i++ {
			cell := ""
			if ti := tt + i; ti < len(m.tree) {
				cell = m.treeCell(m.tree[ti], tw, cur)
			}
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
	} else if m.paneOpen() {
		lines = append(lines, m.paneView(m.width, m.chatHeight())...)
	}

	// Footer.
	hints := "↑↓ scroll · v select · n/p next/prev file · ← tree · c collapse · t tree · s Claude side/below · q quit"
	if m.thread != nil {
		hints = m.threadHints()
	} else if m.chat != nil {
		hints = m.chatHints()
	} else if m.sel.on && !m.sel.anchored {
		hints = "move to the first line with ↑↓, then press v to start selecting · esc cancel"
		if n := len(m.convosAt(m.sel.file, m.sel.cursor)); n > 0 {
			hints = fmt.Sprintf("%d Claude %s · o open · ", n, plural(n, "conversation", "conversations")) + hints
		}
		if n := len(m.threadsAt(m.sel.file, m.sel.cursor)); n > 0 {
			hints = fmt.Sprintf("%d review %s · r read · ", n, plural(n, "thread", "threads")) + hints
		}
	} else if m.sel.on {
		hints = fmt.Sprintf("%d %s selected · ↑↓ extend · a ask Claude · y copy · esc cancel", m.selectedCount(), plural(m.selectedCount(), "line", "lines"))
	} else if m.focus == focusTree {
		hints = "↑↓ pick file · enter/→ open · c collapse · tab diff · q quit"
	}
	foot := lipgloss.NewStyle().Foreground(t.Dim).Render(ansi.Truncate(" "+hints, m.width, "…"))
	if m.flash != "" {
		foot = lipgloss.NewStyle().Foreground(t.Accent).Render(" " + m.flash)
	}
	if m.paneOpen() && m.typing && !m.sideChat() {
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
