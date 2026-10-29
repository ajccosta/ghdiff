package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// drive feeds msg to the model and the messages its commands make, until
// none is left or until stops being false.
func drive(t *testing.T, m *model, msg tea.Msg, until func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	queue := []tea.Msg{msg}
	for len(queue) > 0 && time.Now().Before(deadline) {
		msg, queue = queue[0], queue[1:]
		_, cmd := m.Update(msg)
		if until() {
			return
		}
		queue = append(queue, runCmd(cmd)...)
	}
	if !until() {
		t.Fatal("timed out")
	}
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, runCmd(c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func TestClaudeCodeRunsInThePane(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(fake, []byte("#!/bin/sh\necho started\nexec cat\n"), 0o755)
	old := claudeBin
	claudeBin = fake
	t.Cleanup(func() { claudeBin = old })

	m := newSelTestModel(t)
	m.Claude = "code"
	m.newChat([]span{{0, 1, 1}})
	c := m.chat
	if !c.code || m.typing {
		t.Fatal("not a Claude Code conversation")
	}
	shows := func(s string) func() bool {
		return func() bool { return strings.Contains(ansi.Strip(m.View().Content), s) }
	}
	drive(t, m, nil, shows("started"))
	if c.term == nil || c.session == "" {
		t.Fatal("no terminal or session")
	}
	for _, r := range "hi" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	drive(t, m, termOutMsg{c.term}, shows("hi"))

	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !m.away {
		t.Fatal("ctrl+o did not switch to the diff")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}) // to the diff, not cat
	if c.term == nil {
		t.Fatal("ctrl+d in the diff reached the terminal")
	}
	m.target.repo, m.target.pr = "o/r", 1
	c.turns = []turn{{q: "why?", a: "because"}}
	m.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
	if !m.confirmPost || !shows("y/n")() {
		t.Fatalf("P in the diff did not ask to post: %q", c.postNote)
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.confirmPost || c.term == nil {
		t.Fatal("n did not cancel posting")
	}
	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	tm := c.term
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}) // cat ends
	drive(t, m, termOutMsg{tm}, func() bool { return c.term == nil })
	if m.chat != nil {
		t.Fatal("the pane stayed open after Claude Code exited")
	}
}

func TestTranscriptTurns(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(`{"type":"user","message":{"content":"why?"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Let me look."},{"type":"tool_use"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"x"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Because."}]}}
{"type":"user","isMeta":true,"message":{"content":"meta"}}
{"type":"user","message":{"content":"<command-name>/exit</command-name>"}}
`), 0o644)
	ts := transcriptTurns(p)
	if len(ts) != 1 || ts[0].q != "why?" || ts[0].a != "Let me look.\n\nBecause." {
		t.Fatalf("%+v", ts)
	}
}
