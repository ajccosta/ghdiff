package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

func TestConversationsPaneListsAndOpensNotes(t *testing.T) {
	m := newSelTestModel(t)
	if m.leftHead() != 0 {
		t.Fatal("conversations pane with nothing in it, outside gh-dash")
	}
	m.threads = []*thread{{file: 1, lo: 1, hi: 1, comments: []*comment{
		{author: "alice", body: "\nwhy bee?\nmore", created: time.Now()},
		{author: "me", body: "because", created: time.Now()},
	}}}
	m.convos = []*convo{{spans: []span{{0, 2, 3}}, created: time.Now(), turns: []turn{{q: "is TWO right?", a: "yes"}}}}
	m.relayout()

	m.openPane("conversations")
	if m.focus != focusTree || leftPanes[m.left].name != "Conversations" {
		t.Fatal("GHDIFF_PANE=conversations did not open the pane")
	}
	screen := ansi.Strip(m.View().Content)
	ci, ti := strings.Index(screen, "You: is TWO right?"), strings.Index(screen, "alice: why")
	if ci < 0 || ti < 0 || ci > ti {
		t.Fatalf("notes not listed in the diff's order:\n%s", screen)
	}
	m.press("down", "enter")
	if m.thread != m.threads[0] || m.focus != focusDiff {
		t.Fatal("enter did not open the review thread")
	}
	m.press("esc")
	m.focus = focusTree
	m.press("up", "enter")
	if m.chat != m.convos[0] {
		t.Fatal("enter did not open the Claude conversation")
	}
}

func TestAskWithNothingSelected(t *testing.T) {
	m := newSelTestModel(t)
	m.press("v", "a") // the cursor, but nothing selected
	c := m.chat
	if c == nil || len(c.spans) != 0 || m.sel.on {
		t.Fatal("a with nothing selected did not open a conversation on the whole diff")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "the whole diff") {
		t.Fatal("pane not titled for the whole diff")
	}
	if p := m.firstPrompt(c, "q"); !strings.Contains(p, "selected no lines") {
		t.Fatal("Claude not told nothing is selected")
	}
	c.turns = []turn{{q: "what changed?", a: "two lines"}}
	m.saveConvos()
	m.closeChat()

	// Opened again, it is kept, on no line, and the Conversations pane opens it.
	m2 := newModel(m.files, "t", m.diff, target{})
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if len(m2.convos) != 1 || len(m2.convos[0].spans) != 0 || len(m2.unplaced) != 0 {
		t.Fatalf("not reloaded on the whole diff: %d convos, %d aside", len(m2.convos), len(m2.unplaced))
	}
	m2.openPane("conversations")
	if !strings.Contains(ansi.Strip(m2.View().Content), "You: what changed?") {
		t.Fatal("not listed")
	}
	m2.press("enter")
	if m2.chat != m2.convos[0] {
		t.Fatal("not opened from the pane")
	}
}

func TestAskFromALeftPane(t *testing.T) {
	m := newSelTestModel(t)
	m.press("tab") // the keys in the file tree
	if m.focus != focusTree || !strings.Contains(ansi.Strip(m.View().Content), "a ask Claude") {
		t.Fatal("the tree's footer does not offer a")
	}
	m.press("a")
	if m.chat == nil || len(m.chat.spans) != 0 || m.focus != focusDiff {
		t.Fatal("a in the tree did not open a conversation on the whole diff")
	}
}
