package main

import (
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// lineInput is a one-line text field with the usual terminal editing keys
// (readline/emacs style, plus macOS Option and Cmd arrows).
type lineInput struct {
	text []rune
	cur  int
}

func (in *lineInput) String() string { return string(in.text) }

func (in *lineInput) reset() { in.text, in.cur = nil, 0 }

func (in *lineInput) insert(s string) {
	r := []rune(s)
	in.text = append(in.text[:in.cur], append(r, in.text[in.cur:]...)...)
	in.cur += len(r)
}

func (in *lineInput) cut(from, to int) {
	if from < to {
		in.text = append(in.text[:from], in.text[to:]...)
		in.cur = from
	}
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// wordLeft and wordRight find the start and end of the word next to the
// cursor; with spaceOnly, a word is anything between spaces (as Ctrl+W).
func (in *lineInput) wordLeft(spaceOnly bool) int {
	in2 := func(r rune) bool { return !unicode.IsSpace(r) && (spaceOnly || isWord(r)) }
	i := in.cur
	for i > 0 && !in2(in.text[i-1]) {
		i--
	}
	for i > 0 && in2(in.text[i-1]) {
		i--
	}
	return i
}

func (in *lineInput) wordRight() int {
	i := in.cur
	for i < len(in.text) && !isWord(in.text[i]) {
		i++
	}
	for i < len(in.text) && isWord(in.text[i]) {
		i++
	}
	return i
}

// key applies an editing key; false means it is not one, and text (the
// key's printable text, if any) was not inserted either.
func (in *lineInput) key(k, text string) bool {
	switch k {
	case "ctrl+a", "home", "super+left":
		in.cur = 0
	case "ctrl+e", "end", "super+right":
		in.cur = len(in.text)
	case "left", "ctrl+b":
		in.cur = max(0, in.cur-1)
	case "right", "ctrl+f":
		in.cur = min(len(in.text), in.cur+1)
	case "alt+left", "alt+b", "ctrl+left":
		in.cur = in.wordLeft(false)
	case "alt+right", "alt+f", "ctrl+right":
		in.cur = in.wordRight()
	case "backspace", "ctrl+h":
		in.cut(max(0, in.cur-1), in.cur)
	case "delete", "ctrl+d":
		if in.cur < len(in.text) {
			in.cut(in.cur, in.cur+1)
		}
	case "ctrl+w":
		in.cut(in.wordLeft(true), in.cur)
	case "alt+backspace", "ctrl+backspace":
		in.cut(in.wordLeft(false), in.cur)
	case "alt+d", "alt+delete":
		cur := in.cur
		end := in.wordRight()
		in.cur = cur
		in.cut(cur, end)
	case "ctrl+u", "super+backspace":
		in.cut(0, in.cur)
	case "ctrl+k":
		in.text = in.text[:in.cur]
	default:
		if text == "" {
			return false
		}
		in.insert(text)
	}
	return true
}

// view draws the text in width columns with a block cursor, scrolled so the
// cursor stays visible.
func (in *lineInput) view(width int) string {
	before, after := string(in.text[:in.cur]), ""
	at := " "
	if in.cur < len(in.text) {
		at, after = string(in.text[in.cur]), string(in.text[in.cur+1:])
	}
	room := max(1, width-1)
	if w := ansi.StringWidth(before); w > room {
		before = "…" + ansi.TruncateLeft(before, w-room+1, "")
	}
	after = ansi.Truncate(after, max(0, width-ansi.StringWidth(before)-1), "…")
	return before + lipgloss.NewStyle().Reverse(true).Render(at) + after
}
