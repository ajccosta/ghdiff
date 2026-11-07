package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// gitDiff makes a repo with a 30-line file, changes lines 10 and 25, and
// returns the repo and git's diff (3 lines of context: two hunks).
func gitDiff(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) []byte {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatal(string(out))
		}
		return out
	}
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	write := func() { os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644) }
	write()
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "one")
	lines[9], lines[24] = "changed 10", "changed 25"
	write()
	return dir, git("diff", "--no-color")
}

func screenText(m *model) string {
	var b strings.Builder
	for _, r := range m.rows {
		b.WriteString(strings.TrimRight(ansi.Strip(r.text), " ") + "\n")
	}
	return b.String()
}

func TestExpand_KeyShowsAndHidesTheGap(t *testing.T) {
	dir, raw := gitDiff(t)
	m := newModel(Parse(bytes.NewReader(raw)), "t", raw, target{repoPath: dir})
	m.width, m.height = 100, 60
	m.relayout()
	s := screenText(m)
	if !strings.Contains(s, "↕ 6 hidden lines") || !strings.Contains(s, "↕ 8 hidden lines") ||
		!strings.Contains(s, "↕ show the rest of the file") {
		t.Fatalf("labels:\n%s", s)
	}

	drive(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"}, func() bool { return m.texts[m.files[0]] != nil })
	s = screenText(m)
	if !strings.Contains(s, "line 1\n") || !strings.Contains(s, "line 6\n") || !strings.Contains(s, "▴ hide 6 lines") {
		t.Fatalf("first gap not shown:\n%s", s)
	}
	if strings.Contains(s, "line 15\n") {
		t.Fatalf("e opened more than the first gap:\n%s", s)
	}

	m.key("E")
	s = screenText(m)
	for _, want := range []string{"line 1\n", "line 15\n", "line 30\n"} {
		if !strings.Contains(s, want) {
			t.Fatalf("E did not show %q:\n%s", want, s)
		}
	}
	m.key("E")
	if s = screenText(m); strings.Contains(s, "line 1\n") || strings.Contains(s, "line 30\n") {
		t.Fatalf("E again did not hide them:\n%s", s)
	}
}

func TestExpand_ClickOnHunkRow(t *testing.T) {
	dir, raw := gitDiff(t)
	m := newModel(Parse(bytes.NewReader(raw)), "t", raw, target{repoPath: dir})
	m.width, m.height = 100, 60
	m.relayout()
	r := -1
	for i, w := range m.rows {
		if strings.Contains(ansi.Strip(w.text), "@@ -22") {
			r = i
		}
	}
	y := r - m.off + 1
	drive(t, m, tea.MouseClickMsg{X: 30, Y: y, Button: tea.MouseLeft}, func() bool { return m.texts[m.files[0]] != nil })
	s := screenText(m)
	if !strings.Contains(s, "line 14\n") || strings.Contains(s, "line 1\n") {
		t.Fatalf("click did not open the second gap only:\n%s", s)
	}
}

func TestExpand_TextMustAgree(t *testing.T) {
	f := &File{Lines: []Line{{Kind: Ctx, Old: 2, New: 2, Raw: "b"}}}
	if !agrees(f, []string{"a", "b"}, false) || agrees(f, []string{"a", "x"}, false) || agrees(f, []string{"a"}, true) {
		t.Fatal("agrees")
	}
}
