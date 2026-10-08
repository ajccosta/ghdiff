package main

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type LineKind int

const (
	Ctx LineKind = iota
	Add
	Del
	Hunk
	Note // "\ No newline at end of file", "Binary files differ", ...
)

// Span is a byte range of Line.Text that changed within an edited line.
type Span struct{ From, To int }

type Line struct {
	Kind     LineKind
	Old, New int // line numbers, 0 when the side has none
	Text     string
	Spans    []Span
}

type File struct {
	Old, New   string
	Status     string // modified, added, deleted, renamed, binary
	Adds, Dels int
	Lines      []Line
}

// Path is the name to show: the new path, or the old one for deletions.
func (f *File) Path() string {
	if f.Status == "deleted" || f.New == "" {
		return f.Old
	}
	return f.New
}

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$`)
)

func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

func stripPrefix(p string) string {
	if p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

// Parse reads a unified diff as written by git diff, gh pr diff or the
// GitHub API. Colour codes are dropped.
func Parse(r io.Reader) []*File {
	var files []*File
	var f *File
	oldNo, newNo := 0, 0
	inHunk := false

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := ansiRe.ReplaceAllString(sc.Text(), "")
		line = strings.TrimSuffix(line, "\r")

		if strings.HasPrefix(line, "diff --git ") {
			f = &File{Status: "modified"}
			files = append(files, f)
			inHunk = false
			if parts := strings.SplitN(strings.TrimPrefix(line, "diff --git "), " b/", 2); len(parts) == 2 {
				f.Old, f.New = stripPrefix(parts[0]), parts[1]
			}
			continue
		}
		if f == nil {
			// A diff without "diff --git" headers (plain diff -u).
			if strings.HasPrefix(line, "--- ") {
				f = &File{Status: "modified"}
				files = append(files, f)
			} else {
				continue
			}
		}

		if m := hunkRe.FindStringSubmatch(line); m != nil {
			oldNo, _ = strconv.Atoi(m[1])
			newNo, _ = strconv.Atoi(m[2])
			inHunk = true
			f.Lines = append(f.Lines, Line{Kind: Hunk, Text: line})
			continue
		}

		if inHunk && line != "" {
			switch line[0] {
			case ' ':
				f.Lines = append(f.Lines, Line{Kind: Ctx, Old: oldNo, New: newNo, Text: expandTabs(line[1:])})
				oldNo++
				newNo++
				continue
			case '+':
				f.Lines = append(f.Lines, Line{Kind: Add, New: newNo, Text: expandTabs(line[1:])})
				newNo++
				f.Adds++
				continue
			case '-':
				f.Lines = append(f.Lines, Line{Kind: Del, Old: oldNo, Text: expandTabs(line[1:])})
				oldNo++
				f.Dels++
				continue
			case '\\':
				f.Lines = append(f.Lines, Line{Kind: Note, Text: strings.TrimPrefix(line, "\\ ")})
				continue
			}
		}
		if inHunk && line == "" {
			// Some tools drop the leading space of empty context lines.
			f.Lines = append(f.Lines, Line{Kind: Ctx, Old: oldNo, New: newNo})
			oldNo++
			newNo++
			continue
		}

		switch {
		case strings.HasPrefix(line, "--- "):
			f.Old = stripPrefix(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			f.New = stripPrefix(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "new file mode"):
			f.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			f.Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			f.Status, f.Old = "renamed", strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			f.New = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "Binary files"):
			f.Status = "binary"
			f.Lines = append(f.Lines, Line{Kind: Note, Text: "Binary file not shown"})
		}
	}
	for _, f := range files {
		markWordChanges(f.Lines)
	}
	return files
}

// markWordChanges pairs each run of deleted lines with the run of added lines
// that follows it, line by line, and marks the words that differ, so an edit
// inside a line is highlighted rather than the whole line.
func markWordChanges(lines []Line) {
	for i := 0; i < len(lines); {
		if lines[i].Kind != Del {
			i++
			continue
		}
		d0 := i
		for i < len(lines) && lines[i].Kind == Del {
			i++
		}
		a0 := i
		for i < len(lines) && lines[i].Kind == Add {
			i++
		}
		nd, na := a0-d0, i-a0
		for k := 0; k < nd && k < na; k++ {
			del, add := &lines[d0+k], &lines[a0+k]
			ds, as, ok := wordDiff(del.Text, add.Text)
			if ok {
				del.Spans, add.Spans = ds, as
			}
		}
	}
}

func tokenize(s string) []string {
	var toks []string
	isWord := func(r rune) bool {
		return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127
	}
	rs := []rune(s)
	for i := 0; i < len(rs); {
		j := i + 1
		switch {
		case isWord(rs[i]):
			for j < len(rs) && isWord(rs[j]) {
				j++
			}
		case rs[i] == ' ':
			for j < len(rs) && rs[j] == ' ' {
				j++
			}
		}
		toks = append(toks, string(rs[i:j]))
		i = j
	}
	return toks
}

// wordDiff returns the changed byte ranges of a and b. ok is false when the
// lines have too little in common for word highlighting to help.
func wordDiff(a, b string) (as, bs []Span, ok bool) {
	ta, tb := tokenize(a), tokenize(b)
	if len(ta)*len(tb) > 250000 {
		return nil, nil, false
	}
	// LCS table over tokens.
	n, m := len(ta), len(tb)
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if ta[i] == tb[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	keepA, keepB := make([]bool, n), make([]bool, m)
	same := 0
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case ta[i] == tb[j]:
			keepA[i], keepB[j] = true, true
			same += len(strings.TrimSpace(ta[i]))
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	longest := max(len(strings.ReplaceAll(a, " ", "")), len(strings.ReplaceAll(b, " ", "")))
	if longest == 0 || float64(same)/float64(longest) < 0.35 {
		return nil, nil, false
	}
	return spans(ta, keepA), spans(tb, keepB), true
}

// spans turns the unmatched tokens into byte ranges, joining ranges that are
// separated only by spaces so one edit reads as one block.
func spans(toks []string, keep []bool) []Span {
	var out []Span
	pos := 0
	for i, t := range toks {
		end := pos + len(t)
		if !keep[i] {
			if n := len(out); n > 0 && strings.TrimSpace(gapText(toks, keep, out[n-1].To, pos)) == "" {
				out[n-1].To = end
			} else {
				out = append(out, Span{pos, end})
			}
		}
		pos = end
	}
	return out
}

func gapText(toks []string, keep []bool, from, to int) string {
	var b strings.Builder
	pos := 0
	for _, t := range toks {
		if pos >= from && pos < to {
			b.WriteString(t)
		}
		pos += len(t)
	}
	return b.String()
}
