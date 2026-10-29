package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const oneFile = `diff --git a/c.go b/c.go
--- a/c.go
+++ b/c.go
@@ -1,1 +1,1 @@
-old
+new
`

func TestCommitsPaneSwitchesTheDiff(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newModel(Parse(strings.NewReader(twoFiles)), "t", []byte(twoFiles),
		target{repo: "o/r", pr: 1, dash: true})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(commitsMsg{commits: []prCommit{{sha: "aaaaaaa1", title: "first"}, {sha: "bbbbbbb2", title: "second"}}})
	m.diffs = map[string][]byte{"bbbbbbb2": []byte(oneFile)}

	screen := func() string { return ansi.Strip(m.View().Content) }
	if !strings.Contains(screen(), "Commits") || !strings.Contains(screen(), "Files") {
		t.Fatal("no pane names")
	}
	m.press("tab", "tab") // diff → Files → Commits
	if m.focus != focusTree || leftPanes[m.left].name != "Commits" {
		t.Fatalf("tab did not reach the commits pane (%d)", m.left)
	}
	if !strings.Contains(screen(), "All commits (2)") || !strings.Contains(screen(), "second") {
		t.Fatal("commits not listed")
	}
	m.press("down", "down")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCmd(cmd)
	if len(m.files) != 1 || m.files[0].Path() != "c.go" || m.target.commit != "bbbbbbb2" {
		t.Fatalf("diff not switched: %d files, commit %q", len(m.files), m.target.commit)
	}
	if !strings.Contains(screen(), "@ bbbbbbb · second") || m.width != 120 {
		t.Fatal("title or size not kept")
	}
	m.press("up", "up", "enter") // back to all, from the cache
	if len(m.files) != 2 || m.target.commit != "" {
		t.Fatal("All commits did not come back")
	}
}

func TestNoCommitsPaneWithoutGhDash(t *testing.T) {
	m := newSelTestModel(t)
	if m.leftHead() != 0 || strings.Contains(ansi.Strip(m.View().Content), "Commits") {
		t.Fatal("commits pane outside gh-dash")
	}
}
