package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestTermCursor(t *testing.T) {
	tm := &term{emu: vt.NewSafeEmulator(20, 3), w: 20, h: 3}
	tm.emu.Write([]byte("> ab\x1b[32mcd\x1b[0m\x1b[1;5H")) // cursor on "c"
	got := tm.lines(true)[0]
	if ansi.Strip(got) != "> abcd" || !strings.Contains(got, "\x1b[7m") {
		t.Fatalf("got %q, want the text with its \"c\" reversed", got)
	}
	if l := tm.lines(false)[0]; strings.Contains(l, "\x1b[7m") {
		t.Errorf("unfocused: %q has a cursor", l)
	}
	tm.emu.Write([]byte("\x1b[1;10H")) // past the text
	if l := tm.lines(true)[0]; ansi.Strip(l) != "> abcd    " || ansi.StringWidth(l) != 10 {
		t.Errorf("past the text: %q", l)
	}
	tm.emu.Write([]byte("\x1b[?25l"))
	if l := tm.lines(true)[0]; strings.Contains(l, "\x1b[7m") {
		t.Errorf("hidden cursor drawn: %q", l)
	}
}
