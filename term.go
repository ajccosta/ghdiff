package main

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// A program (Claude Code) running in a pseudo-terminal, its screen kept by a
// terminal emulator and drawn in the pane. Keys and pastes are passed on while
// the pane has the focus.
type term struct {
	emu    *vt.SafeEmulator
	pty    *os.File
	cmd    *exec.Cmd
	w, h   int
	scroll int           // lines back into the scrollback, 0 at the live screen
	out    chan struct{} // a signal per batch of output
	exited chan struct{}
}

type termOutMsg struct{ t *term }  // new output to draw
type termDoneMsg struct{ t *term } // the program exited

func startTerm(dir, name string, args []string, w, h int) (*term, error) {
	w, h = max(w, 20), max(h, 5)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	if err != nil {
		return nil, err
	}
	t := &term{emu: vt.NewSafeEmulator(w, h), pty: f, cmd: cmd, w: w, h: h,
		out: make(chan struct{}, 1), exited: make(chan struct{})}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				t.emu.Write(buf[:n])
				select {
				case t.out <- struct{}{}:
				default:
				}
			}
			if err != nil {
				break
			}
		}
		cmd.Wait()
		close(t.exited)
	}()
	go io.Copy(f, t.emu) // keys, and the emulator's replies to queries
	return t, nil
}

// wait is the next termOutMsg or termDoneMsg; each termOutMsg re-arms it.
func (t *term) wait() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-t.out:
			return termOutMsg{t}
		case <-t.exited:
			return termDoneMsg{t}
		}
	}
}

func (t *term) resize(w, h int) {
	w, h = max(w, 20), max(h, 5)
	if w == t.w && h == t.h {
		return
	}
	t.w, t.h = w, h
	t.emu.Resize(w, h)
	pty.Setsize(t.pty, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
}

func (t *term) key(msg tea.KeyPressMsg) {
	t.scroll = 0
	t.emu.SendKey(uv.KeyPressEvent(msg))
}

func (t *term) paste(s string) {
	t.scroll = 0
	t.emu.Paste(s)
}

// wheel scrolls back through what went off the top of the screen.
func (t *term) wheel(n int) {
	t.scroll = min(max(0, t.scroll-n), t.emu.ScrollbackLen())
}

// fullScreen is whether the program draws the whole screen itself (the
// alternate screen, as Claude Code does): then it gets the mouse, if it asked.
func (t *term) fullScreen() bool { return t.emu.IsAltScreen() }

// mouse passes on a mouse event at cell x, y of the screen.
func (t *term) mouse(ev tea.MouseMsg, x, y int) {
	mo := ev.Mouse()
	mo.X, mo.Y = x, y
	u := uv.Mouse(mo)
	switch ev.(type) {
	case tea.MouseClickMsg:
		t.emu.SendMouse(uv.MouseClickEvent(u))
	case tea.MouseReleaseMsg:
		t.emu.SendMouse(uv.MouseReleaseEvent(u))
	case tea.MouseWheelMsg:
		t.emu.SendMouse(uv.MouseWheelEvent(u))
	case tea.MouseMotionMsg:
		t.emu.SendMouse(uv.MouseMotionEvent(u))
	}
}

// lines are the screen's h lines, w cells wide, from scroll lines back. With
// cursor, the program's cursor is drawn (reversed) where it is, unless it hid
// it: the pane is text in ghdiff's screen, which has no cursor of its own.
func (t *term) lines(cursor bool) []string {
	screen := strings.Split(t.emu.Render(), "\n")
	var out []string
	if s := t.scroll; s > 0 {
		sb := t.emu.Scrollback()
		n := t.emu.ScrollbackLen()
		for y := n - s; y < n && len(out) < t.h; y++ {
			out = append(out, sb.Line(y).Render())
		}
	}
	for _, l := range screen {
		if len(out) == t.h {
			break
		}
		out = append(out, l)
	}
	for len(out) < t.h {
		out = append(out, "")
	}
	for i, l := range out {
		out[i] = ansi.Truncate(l, t.w, "") + "\x1b[0m"
	}
	if p := t.emu.CursorPosition(); cursor && t.scroll == 0 && !t.emu.CursorHidden() &&
		p.Y >= 0 && p.Y < len(out) && p.X >= 0 && p.X < t.w {
		out[p.Y] = withCursor(out[p.Y], p.X)
	}
	return out
}

// withCursor reverses the cell at column x of line l.
func withCursor(l string, x int) string {
	if pad := x + 1 - ansi.StringWidth(l); pad > 0 {
		l += strings.Repeat(" ", pad)
	}
	cell := ansi.Cut(l, x, x+1)
	if ansi.StringWidth(cell) == 0 {
		cell = " "
	}
	return ansi.Truncate(l, x, "") + "\x1b[7m" + cell + "\x1b[27m" + ansi.TruncateLeft(l, x+1, "")
}

// stop ends the program (SIGHUP, as when a terminal closes).
func (t *term) stop() {
	if t.cmd.Process != nil {
		t.cmd.Process.Signal(syscall.SIGHUP)
	}
	t.pty.Close()
}
