package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const twoFiles = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,2 @@
 one
-two
+TWO
@@ -10,2 +10,2 @@
 ten
-eleven
+ELEVEN
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,1 +1,1 @@
-bee
+BEE
`

func newSelTestModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	files := Parse(strings.NewReader(twoFiles))
	m := newModel(files, "t", []byte(twoFiles), target{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return m
}

func (m *model) press(keys ...string) {
	for _, k := range keys {
		m.key(k)
	}
}

func TestSelectionCrossesHunksAndFiles(t *testing.T) {
	m := newSelTestModel(t)
	m.sel = selection{on: true, anchor: pos{0, 2}, cursor: pos{0, 2}} // "-two"
	m.press("v")
	for i := 0; i < 5; i++ { // +TWO, ten, -eleven, +ELEVEN, then b.go's -bee
		m.press("down")
	}
	if got := m.sel.cursor; got != (pos{1, 1}) {
		t.Fatalf("cursor = %v, want b.go's first line", got)
	}
	spans := m.selSpans()
	want := []span{{0, 2, 7}, {1, 1, 1}}
	if len(spans) != 2 || spans[0] != want[0] || spans[1] != want[1] {
		t.Fatalf("spans = %v, want %v", spans, want)
	}
	if n := m.selectedCount(); n != 6 {
		t.Fatalf("selectedCount = %d, want 6 (the hunk header is not code)", n)
	}
}

func TestMultipleRanges(t *testing.T) {
	m := newSelTestModel(t)
	m.sel = selection{on: true, anchor: pos{0, 1}, cursor: pos{0, 1}} // " one"
	m.press("v", "v")                                                 // anchor, then keep the one-line range
	if m.sel.anchored || len(m.sel.kept) != 1 {
		t.Fatalf("v on a range should keep it: %+v", m.sel)
	}
	m.sel.cursor = pos{1, 2} // "+BEE"
	m.press("v")
	spans := m.selSpans()
	if len(spans) != 2 || spans[0] != (span{0, 1, 1}) || spans[1] != (span{1, 2, 2}) {
		t.Fatalf("spans = %v", spans)
	}
	if !strings.Contains(m.selHints(), "2 lines in 2 ranges") {
		t.Fatalf("hints = %q", m.selHints())
	}
	m.press("a")
	if m.chat == nil || len(m.chat.spans) != 2 {
		t.Fatal("a should open a conversation on both ranges")
	}
	p := m.firstPrompt(m.chat, "why?")
	if !strings.Contains(p, "File: a.go") || !strings.Contains(p, "File: b.go") || !strings.Contains(p, "+BEE") {
		t.Fatalf("prompt misses a range:\n%s", p)
	}
	if m.postBlocked() == "" {
		t.Fatal("a conversation on several ranges can't be posted")
	}
	m.press("esc")
}

func TestCopyText(t *testing.T) {
	m := newSelTestModel(t)
	// One side only, over a hunk header: plain code with ⋮ for the gap.
	if got, want := m.copyText([]span{{0, 3, 5}}), "TWO\n⋮\nten\n"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
	// Both sides: a diff, with the hunk header.
	if got, want := m.copyText([]span{{0, 2, 5}}), "-two\n+TWO\n@@ -10,2 +10,2 @@\n ten\n"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
	// Several ranges: each starts with its place.
	if got, want := m.copyText([]span{{0, 1, 1}, {1, 2, 2}}), "a.go:L1\none\n\nb.go:L1\nBEE\n"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
}

func TestWheelScrollsThePaneUnderThePointer(t *testing.T) {
	m := newSelTestModel(t)
	m.newChat([]span{{0, 1, 1}})
	m.typing = false
	m.chat.turns = []turn{{q: "q", a: strings.Repeat("line\n", 200)}}
	m.relayout()
	m.View() // clamps the scroll
	off, scroll := m.off, m.chat.scroll
	y := m.paneHeight() + 3 // below the diff: the pane, when it is below
	x := 5
	if cw := m.chatWidth(); cw > 0 {
		x, y = m.width-2, 5
	}
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if m.chat.scroll != scroll+3 || m.off != off {
		t.Fatalf("wheel over the pane: scroll %d→%d, diff %d→%d", scroll, m.chat.scroll, off, m.off)
	}
	m.Update(tea.MouseWheelMsg{X: m.treeWidth() + 5, Y: 2, Button: tea.MouseWheelDown})
	if m.chat.scroll != scroll+3 {
		t.Fatal("wheel over the diff scrolled the pane")
	}
}

func TestClaudeConversationIsANoteOnItsLine(t *testing.T) {
	m := newSelTestModel(t)
	m.newChat([]span{{0, 2, 3}})
	c := m.chat
	c.turns = []turn{{q: "why?", a: "because"}}
	m.typing = false
	m.closeChat()
	if m.chat != nil {
		t.Fatal("esc kept the pane open")
	}
	m.sel = selection{}
	y := -1
	for sy := 1; sy <= m.paneHeight(); sy++ {
		if r := m.diffRow(sy); r >= 0 && m.rows[r].file == 0 && m.rows[r].line == 2 {
			y = sy
			break
		}
	}
	m.Update(tea.MouseMotionMsg{X: m.diffX(), Y: y})
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "Claude · ") || !strings.Contains(v, "because") {
		t.Fatalf("hovering ● shows no box:\n%s", v)
	}
	m.Update(tea.MouseClickMsg{X: m.pop.x + 2, Y: m.pop.y + 1, Button: tea.MouseLeft})
	if m.chat != c {
		t.Fatal("clicking the box did not reopen the conversation")
	}
	m.closeChat()
	m.Update(tea.MouseClickMsg{X: m.diffX(), Y: y, Button: tea.MouseLeft})
	if m.chat != c {
		t.Fatal("clicking ● did not reopen the conversation")
	}
}
