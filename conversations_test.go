package main

import (
	"strings"
	"testing"
	"time"

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
