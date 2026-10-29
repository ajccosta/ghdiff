package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
