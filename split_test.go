package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

type splitExpand struct{ noExpand }

func (splitExpand) split() bool { return true }

func splitView(t *testing.T, diff string, width int) []string {
	t.Helper()
	rows, _ := buildRows(themeFor(true, true), Parse(strings.NewReader(diff)), nil, width, splitExpand{})
	var out []string
	for _, r := range rows {
		out = append(out, strings.TrimRight(ansi.Strip(r.text), " "))
	}
	return out
}

func TestSplit_PairsRemovedWithAdded(t *testing.T) {
	got := splitView(t, `diff --git a/x.go b/x.go
--- a/x.go
+++ b/x.go
@@ -1,4 +1,4 @@
 same
-old one
-old two
+new one
 end
`, 50)
	want := []string{
		"            @@ -1,4 +1,4 @@",
		"   1    same             │  1    same",
		"   2  - old one          │  2  + new one",
		"   3  - old two          │╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱╱",
		"   4    end              │  3    end",
	}
	for i, w := range want {
		if got[1+i] != w {
			t.Fatalf("row %d:\n got %q\nwant %q\nall:\n%s", 1+i, got[1+i], w, strings.Join(got, "\n"))
		}
	}
}

func TestSplit_ConflictShowsCurrentBesideIncoming(t *testing.T) {
	got := splitView(t, `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,3 +1,7 @@
 start
+<<<<<<< gh-dash/19/main
 mine
+=======
+theirs
+>>>>>>> gh-dash/19/pr-19
 end
`, 60)
	all := strings.Join(got, "\n")
	for _, w := range []string{
		" Current · gh-dash/19/main",
		" Incoming · gh-dash/19/pr-19",
		"   2    mine                  │  5    theirs",
		"   3    end                   │  7    end",
	} {
		if !strings.Contains(all, w) {
			t.Fatalf("no %q in\n%s", w, all)
		}
	}
	for _, marker := range []string{"=======", "<<<<<<<", ">>>>>>>"} {
		if strings.Contains(all, " "+marker) {
			t.Fatalf("%s drawn as a line:\n%s", marker, all)
		}
	}
}
