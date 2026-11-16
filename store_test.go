package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOtherWindowsConversationsShowUp(t *testing.T) {
	a := newSelTestModel(t)
	b := newSelTestModel(t)
	b.store, b.storeMod = a.store, time.Time{}
	a.newChat([]span{{0, 1, 1}})
	a.chat.session, a.chat.turns = "s1", []turn{{q: "why?", a: "because"}}
	a.saveConvos()
	b.watched()
	if len(b.convos) != 1 || len(b.convos[0].turns) != 1 || b.convos[0].turns[0].a != "because" {
		t.Fatalf("the other window's conversation did not show up: %d", len(b.convos))
	}
	b.newChat([]span{{1, 1, 1}})
	b.chat.session, b.chat.turns = "s2", []turn{{q: "and?", a: "so"}}
	b.saveConvos()
	a.chat.turns = append(a.chat.turns, turn{q: "more?", a: "yes"})
	a.saveConvos() // keeps b's
	if sf, _ := a.readStore(); len(sf.Convos) != 2 {
		t.Fatalf("saving dropped the other window's conversation: %d saved", len(sf.Convos))
	}
	b.watched()
	if len(b.convos[0].turns) != 2 {
		t.Fatal("the other window's new turn did not show up")
	}
}

func TestClaudeCodeIsKeptFromItsFirstQuestion(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	m := newSelTestModel(t)
	m.Claude = "code"
	m.newChat([]span{{0, 1, 1}})
	c := m.chat
	c.session, c.term = "sess", &term{}
	p := filepath.Join(root, "projects", "x", "sess.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"type":"user","message":{"content":"why?"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"because"}]}}
`), 0o644)
	m.watched()
	b, _ := os.ReadFile(m.store)
	if !strings.Contains(string(b), "because") {
		t.Fatalf("not saved after the first question:\n%s", b)
	}
}
