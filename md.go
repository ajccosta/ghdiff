package main

import (
	"image/color"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Markdown files are shown formatted (m shows their source, and back): each
// source line stays one line, so the diff, its numbers and its colours hold,
// but markers are dropped (# ** ` [](url)), headings, emphasis, code and
// links are styled, lists get bullets, quotes a bar, tables lines, and code
// blocks their language's colours. A new markdown form is one more case in
// mdLine or one more alternative in mdInlineRe.

func isMarkdown(f *File) bool {
	switch strings.ToLower(filepath.Ext(f.Path())) {
	case ".md", ".markdown", ".mdown", ".mkd", ".mdx":
		return true
	}
	return false
}

// Token types for markdown styles, beside chroma's (Theme.tokStyle).
const (
	mdHeading chroma.TokenType = -900000 - iota
	mdStrong
	mdEmph
	mdCode
	mdLink
	mdDim
	mdStrike
)

type tokStyle struct {
	fg                              color.Color
	bold, italic, underline, strike bool
}

// tokStyle is how a token type is drawn: markdown's own, else chroma's colour.
func (t Theme) tokStyle(tt chroma.TokenType) tokStyle {
	switch tt {
	case mdHeading:
		return tokStyle{fg: t.Accent, bold: true}
	case mdStrong:
		return tokStyle{bold: true}
	case mdEmph:
		return tokStyle{italic: true}
	case mdCode:
		return tokStyle{fg: t.ModFg}
	case mdLink:
		return tokStyle{fg: t.Accent, underline: true}
	case mdDim:
		return tokStyle{fg: t.Dim}
	case mdStrike:
		return tokStyle{fg: t.Dim, strike: true}
	}
	return tokStyle{fg: t.syntaxColor(tt)}
}

// mdState is what one side's earlier lines leave open: a code block.
type mdState struct {
	fence string // "```" or "~~~" while inside a block
	lexer chroma.Lexer
}

// mdOut builds a formatted line and maps the source's bytes to its own.
type mdOut struct {
	b    strings.Builder
	toks []tok
	at   []int // source byte -> output byte, -1 when dropped
}

func (o *mdOut) style(from int, tt chroma.TokenType) {
	if tt != 0 && o.b.Len() > from {
		o.toks = append(o.toks, tok{from, o.b.Len(), tt})
	}
}

// keep copies source bytes [i, j) of s.
func (o *mdOut) keep(s string, i, j int, tt chroma.TokenType) {
	from := o.b.Len()
	for k := i; k < j; k++ {
		o.at[k] = o.b.Len()
		o.b.WriteByte(s[k])
	}
	o.style(from, tt)
}

// add writes text that is not in the source.
func (o *mdOut) add(s string, tt chroma.TokenType) {
	from := o.b.Len()
	o.b.WriteString(s)
	o.style(from, tt)
}

var (
	mdFenceRe   = regexp.MustCompile("^\\s*(```+|~~~+)\\s*([\\w+#.-]*)")
	mdHeadingRe = regexp.MustCompile(`^(#{1,6})\s+`)
	mdRuleRe    = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	mdQuoteRe   = regexp.MustCompile(`^\s*>\s?`)
	mdTaskRe    = regexp.MustCompile(`^(\s*)[-*+]\s+\[([ xX])\]\s+`)
	mdBulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	mdTableRe   = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	mdTableSep  = regexp.MustCompile(`^[\s|:-]+$`)
	mdCommentRe = regexp.MustCompile(`^\s*<!--.*-->\s*$`)
	// Inline forms, in the order they win at the same place; the group with
	// the text to keep is named after its style.
	mdInlineRe = regexp.MustCompile("`(?P<code>[^`]+)`" +
		`|!\[(?P<image>[^\]]*)\]\([^)]*\)` +
		`|\[(?P<link>[^\]]+)\]\([^)]*\)` +
		`|<(?P<url>https?://[^>\s]+)>` +
		`|\*\*(?P<strong>[^*]+)\*\*|__(?P<strong2>[^_]+)__` +
		`|~~(?P<strike>[^~]+)~~` +
		`|\*(?P<emph>[^*\s][^*]*)\*|\b_(?P<emph2>[^_]+)_\b`)
	mdInlineType = map[string]chroma.TokenType{
		"code": mdCode, "image": mdLink, "link": mdLink, "url": mdLink,
		"strong": mdStrong, "strong2": mdStrong, "strike": mdStrike, "emph": mdEmph, "emph2": mdEmph,
	}
)

// inline formats s[i:j], styling plain text as base.
func (o *mdOut) inline(s string, i, j int, base chroma.TokenType) {
	for i < j {
		m := mdInlineRe.FindStringSubmatchIndex(s[i:j])
		if m == nil {
			o.keep(s, i, j, base)
			return
		}
		o.keep(s, i, i+m[0], base)
		for g, name := range mdInlineRe.SubexpNames() {
			if name == "" || m[2*g] < 0 {
				continue
			}
			if name == "image" {
				o.add("▣ ", mdLink)
			}
			tt := mdInlineType[name]
			if base == mdHeading && tt != mdCode {
				tt = mdHeading
			}
			o.keep(s, i+m[2*g], i+m[2*g+1], tt)
		}
		i += m[1]
	}
}

// mdLine is l formatted, with st carried to the next line on its side.
func mdLine(l Line, st *mdState) Line {
	s := l.Text
	o := &mdOut{at: make([]int, len(s))}
	for k := range o.at {
		o.at[k] = -1
	}
	switch fm := mdFenceRe.FindStringSubmatch(s); {
	case st.fence != "":
		if fm != nil && strings.HasPrefix(fm[1], st.fence) {
			st.fence = ""
			o.keep(s, 0, len(s), mdDim)
			break
		}
		o.keep(s, 0, len(s), 0)
		if st.lexer != nil {
			code := codeToks(st.lexer, s)
			o.toks = code
		} else {
			o.toks = []tok{{0, len(s), mdCode}}
		}
	case fm != nil:
		st.fence = fm[1][:3]
		st.lexer = nil
		if lx := lexers.Get(fm[2]); fm[2] != "" && lx != nil {
			st.lexer = chroma.Coalesce(lx)
		}
		o.keep(s, 0, len(s), mdDim)
	case mdCommentRe.MatchString(s):
		o.keep(s, 0, len(s), mdDim)
	case mdRuleRe.MatchString(s):
		o.add(strings.Repeat("─", 40), mdDim)
	case mdTableRe.MatchString(s):
		sep := mdTableSep.MatchString(s)
		start := 0
		for k := 0; k <= len(s); k++ {
			if k < len(s) && (s[k] != '|' || k > 0 && s[k-1] == '\\') {
				continue
			}
			if sep {
				o.keep(s, start, k, mdDim)
			} else {
				o.inline(s, start, k, 0)
			}
			if k < len(s) {
				o.add("│", mdDim)
			}
			start = k + 1
		}
	default:
		i := 0
		for q := mdQuoteRe.FindStringIndex(s[i:]); q != nil; q = mdQuoteRe.FindStringIndex(s[i:]) {
			o.add("│ ", mdDim)
			i += q[1]
		}
		if h := mdHeadingRe.FindStringSubmatchIndex(s[i:]); h != nil {
			end := len(strings.TrimRight(strings.TrimRight(s, " "), "#"))
			if end <= i+h[1] {
				end = len(s)
			}
			o.inline(s, i+h[1], end, mdHeading)
			break
		}
		if t := mdTaskRe.FindStringSubmatchIndex(s[i:]); t != nil {
			o.keep(s, i+t[2], i+t[3], 0)
			if s[i+t[4]] == ' ' {
				o.add("☐ ", 0)
			} else {
				o.add("☑ ", 0)
			}
			i += t[1]
		} else if b := mdBulletRe.FindStringSubmatchIndex(s[i:]); b != nil {
			o.keep(s, i+b[2], i+b[3], 0)
			o.add("• ", mdDim)
			i += b[1]
		}
		o.inline(s, i, len(s), 0)
	}

	out := l
	out.Text, out.Syntax = o.b.String(), o.toks
	out.Spans = nil
	for _, sp := range l.Spans {
		from, to := -1, -1
		for k := sp.From; k < sp.To && k < len(o.at); k++ {
			if o.at[k] >= 0 {
				if from < 0 {
					from = o.at[k]
				}
				to = o.at[k] + 1
			}
		}
		if from >= 0 {
			out.Spans = append(out.Spans, Span{from, to})
		}
	}
	return out
}

func codeToks(lexer chroma.Lexer, s string) []tok {
	it, err := lexer.Tokenise(nil, s)
	if err != nil {
		return nil
	}
	var out []tok
	p := 0
	for t := it(); t != chroma.EOF; t = it() {
		v := strings.TrimSuffix(t.Value, "\n")
		if n := len(v); n > 0 && p+n <= len(s) {
			out = append(out, tok{p, p + n, t.Type})
			p += n
		}
	}
	return out
}

// mdFormatter formats a file's lines in diff order, keeping each side's
// code-block state: context lines move both, - lines the old, + the new.
func mdFormatter() func(Line) Line {
	var old, new mdState
	return func(l Line) Line {
		switch l.Kind {
		case Del:
			return mdLine(l, &old)
		case Add:
			return mdLine(l, &new)
		case Ctx:
			o := old
			out := mdLine(l, &new)
			mdLine(l, &o)
			old = o
			return out
		}
		return l
	}
}

// mdNote is a markdown file's header note: how it shows, and m to switch.
func mdNote(f *File, source bool) string {
	switch {
	case !isMarkdown(f):
		return ""
	case source:
		return "source · m formats"
	}
	return "formatted · m shows source"
}

func (m *model) mdSource() bool { return m.MdSource }

// toggleMarkdown switches every markdown file between formatted and source.
func (m *model) toggleMarkdown() {
	for _, f := range m.files {
		if isMarkdown(f) {
			m.MdSource = !m.MdSource
			m.saveSettings()
			m.relayout()
			return
		}
	}
	m.flash = "No markdown files in this diff"
}
