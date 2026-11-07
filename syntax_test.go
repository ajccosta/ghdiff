package main

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
)

func TestHighlightFollowsEachSide(t *testing.T) {
	d := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n /* a comment\n-old */ var a = 1\n+new */ var b = 2\n func f() {}\n"
	f := Parse(strings.NewReader(d))[0]
	f.prepare()
	typeAt := func(li int, word string) chroma.TokenType {
		l := f.Lines[li]
		i := strings.Index(l.Text, word)
		for _, tk := range l.Syntax {
			if i >= tk.From && i < tk.To {
				return tk.Type
			}
		}
		t.Fatalf("no token for %q on line %d (%q): %v", word, li, l.Text, l.Syntax)
		return 0
	}
	// The comment opened on the context line goes on into both sides.
	if tt := typeAt(2, "old"); tt.Category() != chroma.Comment {
		t.Errorf("old: %v, want a comment", tt)
	}
	if tt := typeAt(3, "new"); tt.Category() != chroma.Comment {
		t.Errorf("new: %v, want a comment", tt)
	}
	if tt := typeAt(3, "var"); tt.Category() != chroma.Keyword {
		t.Errorf("var after the comment: %v, want a keyword", tt)
	}
	if tt := typeAt(4, "func"); tt.Category() != chroma.Keyword {
		t.Errorf("func: %v, want a keyword", tt)
	}
}

func TestNoHighlightForUnknownFiles(t *testing.T) {
	d := "diff --git a/notes.zzz b/notes.zzz\n--- a/notes.zzz\n+++ b/notes.zzz\n@@ -1 +1 @@\n-a\n+b\n"
	f := Parse(strings.NewReader(d))[0]
	f.prepare()
	for _, l := range f.Lines {
		if l.Syntax != nil {
			t.Fatalf("unexpected tokens: %v", l.Syntax)
		}
	}
}
