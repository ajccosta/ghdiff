package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ghdiff --render WIDTH [--mark SIDE:START:END] [--light] prints a diff's
// lines as the viewer draws them, without the file header or @@ rows, and
// exits: gh-dash shows the code a review comment is about this way. --mark
// puts the selection bar on those lines (sides as in GHDIFF_GOTO). Colours
// are for a dark background unless --light; truecolor when $COLORTERM says so.

// noExpand is an expander with nothing hidden, shown or drawn under lines.
type noExpand struct{}

func (noExpand) gap(int, int) []Line            { return nil }
func (noExpand) expandLabel(int, int) string    { return "" }
func (noExpand) mdSource() bool                 { return false }
func (noExpand) under(int, int, int, int) []row { return nil }

func renderMain(args []string, in io.Reader, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("--render needs a width")
	}
	width, err := strconv.Atoi(args[0])
	if err != nil || width < 20 {
		return fmt.Errorf("bad width %q", args[0])
	}
	dark, mark := true, (*jump)(nil)
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--light":
			dark = false
		case args[i] == "--mark" && i+1 < len(args):
			i++
			if mark = parseJump(":" + args[i]); mark == nil {
				return fmt.Errorf("bad --mark %q", args[i])
			}
		default:
			return fmt.Errorf("unknown argument %q", args[i])
		}
	}
	b, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	files := Parse(bytes.NewReader(b))
	ct := os.Getenv("COLORTERM")
	t := themeFor(dark, ct == "truecolor" || ct == "24bit")
	rows, _ := buildRows(t, files, nil, width, noExpand{})
	bar := lipgloss.NewStyle().Foreground(t.Accent).Render("▌")
	var lines []string
	for _, r := range rows {
		if r.header || r.line < 0 {
			continue
		}
		l := files[r.file].Lines[r.line]
		if l.Kind == Hunk || l.Kind == Note {
			continue
		}
		text := r.text
		if mark != nil && mark.covers(l) {
			text = bar + ansi.TruncateLeft(text, 1, "")
		}
		lines = append(lines, text)
	}
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

// covers: l is on the mark's side and within its lines.
func (j *jump) covers(l Line) bool {
	n := l.New
	if j.side == "LEFT" {
		if l.Kind == Add {
			return false
		}
		n = l.Old
	} else if l.Kind == Del {
		return false
	}
	return n >= j.start && n <= j.end
}
