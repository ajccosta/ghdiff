package main

import "testing"

func TestMarkdown_DropsMarkers(t *testing.T) {
	cases := map[string]string{
		"## Install it":                  "Install it",
		"Run **go build** and `ghdiff`.": "Run go build and ghdiff.",
		"See [the docs](https://x.y/z).": "See the docs.",
		"- [x] done":                     "☑ done",
		"  * item":                       "  • item",
		"> quoted":                       "│ quoted",
		"| a | b |":                      "│ a │ b │",
		"![logo](logo.png)":              "▣ logo",
		"snake_case_name stays":          "snake_case_name stays",
	}
	for in, want := range cases {
		if got := mdLine(Line{Kind: Ctx, Text: in}, &mdState{}).Text; got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestMarkdown_CodeBlockKeepsItsText(t *testing.T) {
	format := mdFormatter()
	var got []string
	for _, s := range []string{"```go", "x := **y**", "```", "**b**"} {
		got = append(got, format(Line{Kind: Ctx, Text: s}).Text)
	}
	want := []string{"```go", "x := **y**", "```", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMarkdown_SpansFollowTheText(t *testing.T) {
	l := mdLine(Line{Kind: Add, Text: "a **bold** word", Spans: []Span{{4, 8}}}, &mdState{})
	if len(l.Spans) != 1 || l.Text[l.Spans[0].From:l.Spans[0].To] != "bold" {
		t.Errorf("spans %v on %q", l.Spans, l.Text)
	}
}
