package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRender_LinesAndMark(t *testing.T) {
	in := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -10,3 +10,3 @@\n a := 1\n-b := 2\n+b := 3\n c := 4\n"
	var out bytes.Buffer
	if err := renderMain([]string{"60", "--mark", "RIGHT:11:11"}, strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want the 4 code lines, got %d:\n%s", len(lines), ansi.Strip(out.String()))
	}
	for i, l := range lines {
		plain := ansi.Strip(l)
		if w := ansi.StringWidth(l); w != 60 {
			t.Errorf("line %d is %d wide: %q", i, w, plain)
		}
		if marked := strings.HasPrefix(plain, "▌"); marked != (i == 2) {
			t.Errorf("line %d marked=%v: %q", i, marked, plain)
		}
	}
	if !strings.Contains(ansi.Strip(lines[2]), "b := 3") {
		t.Errorf("third line: %q", ansi.Strip(lines[2]))
	}
}
