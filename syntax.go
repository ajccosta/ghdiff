package main

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Syntax highlighting: each file is tokenised by its name, one side at a
// time (the old side's lines, then the new side's, each as one text), so a
// comment or string that starts above a line still colours it. Lines keep
// the token types; the theme turns them into colours when drawn.

// tok is a byte range of Line.Text and its token type.
type tok struct {
	From, To int
	Type     chroma.TokenType
}

// highlight sets Line.Syntax on the code lines of f, if chroma knows it.
func highlight(f *File) {
	lexer := lexers.Match(f.Path())
	if lexer == nil || f.Status == "binary" {
		return
	}
	lexer = chroma.Coalesce(lexer)
	highlightSide(lexer, f.Lines, func(l Line) bool { return l.Kind == Ctx || l.Kind == Del }, func(l Line) bool { return l.Kind == Del })
	highlightSide(lexer, f.Lines, func(l Line) bool { return l.Kind == Ctx || l.Kind == Add }, func(l Line) bool { return l.Kind != Del })
}

// highlightSide tokenises the lines on one side (in) as one text, and keeps
// the tokens of the lines it owns (set): context lines take the new side's.
func highlightSide(lexer chroma.Lexer, lines []Line, in, set func(Line) bool) {
	var idx []int
	var b strings.Builder
	for i, l := range lines {
		if in(l) {
			idx = append(idx, i)
			b.WriteString(l.Text + "\n")
		}
	}
	if len(idx) == 0 {
		return
	}
	it, err := lexer.Tokenise(nil, b.String())
	if err != nil {
		return
	}
	k, pos := 0, 0 // the line being filled, and the byte offset into it
	var cur []tok
	flush := func() {
		if l := &lines[idx[k]]; set(*l) {
			l.Syntax = cur
		}
		cur, pos = nil, 0
		k++
	}
	for t := it(); t != chroma.EOF && k < len(idx); t = it() {
		for parts := strings.Split(t.Value, "\n"); ; parts = parts[1:] {
			if n := len(parts[0]); n > 0 {
				cur = append(cur, tok{pos, pos + n, t.Type})
				pos += n
			}
			if len(parts) == 1 {
				break
			}
			flush()
			if k >= len(idx) {
				break
			}
		}
	}
}

// syntaxStyle is chroma's GitHub style for the background.
func syntaxStyle(dark bool) *chroma.Style {
	if dark {
		return styles.Get("github-dark")
	}
	return styles.Get("github")
}

// syntaxColor is the colour of a token type, or nil for the default text.
func (t Theme) syntaxColor(tt chroma.TokenType) color.Color {
	if t.Syntax == nil {
		return nil
	}
	e := t.Syntax.Get(tt)
	if !e.Colour.IsSet() || tt == chroma.Text || tt == chroma.Whitespace {
		return nil
	}
	return lipgloss.Color(e.Colour.String())
}
