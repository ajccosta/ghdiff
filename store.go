package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Conversations are kept on disk per diff (a PR, or a local repo), so they
// show up again the next time the same diff is opened. A conversation is tied
// to its lines by their content, so it stays put when other lines move.

type savedTurn struct {
	Q   string `json:"q"`
	A   string `json:"a"`
	Err bool   `json:"err,omitempty"`
}

type savedConvo struct {
	Path      string      `json:"path"`
	Lines     []string    `json:"lines"` // "+text", "-text", " text" for lo..hi
	Old       int         `json:"old"`   // the first line's numbers, to prefer
	New       int         `json:"new"`
	Turns     []savedTurn `json:"turns"`
	Session   string      `json:"session,omitempty"`
	Dir       string      `json:"dir,omitempty"` // where claude ran; resume needs it
	ReadRepo  bool        `json:"readRepo,omitempty"`
	CommentID int64       `json:"commentId,omitempty"`
	Posted    int         `json:"posted,omitempty"`
	Created   time.Time   `json:"created"`
}

type savedFile struct {
	Convos []savedConvo `json:"convos"`
}

var unsafeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// storePath names the file for this diff: by PR, by repo and commit, or by
// the local repo; "" when the diff has no stable identity (a piped file).
func storePath(t target) string {
	var key string
	switch {
	case t.repo != "" && t.pr > 0:
		key = t.repo + "#" + strconv.Itoa(t.pr)
	case t.repo != "" && t.commit != "":
		key = t.repo + "@" + t.commit
	default:
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return ""
		}
		key = "local" + strings.TrimSpace(string(out))
	}
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "ghdiff", "conversations", unsafeRe.ReplaceAllString(key, "_")+".json")
}

func lineSig(l Line) string {
	return map[LineKind]string{Add: "+", Del: "-", Ctx: " ", Note: "\\"}[l.Kind] + l.Raw
}

// loadConvos reads the saved conversations and places each on the lines it
// was about; ones whose lines are gone are kept aside, to be saved again.
func (m *model) loadConvos() {
	m.store = storePath(m.target)
	if m.store == "" {
		return
	}
	b, err := os.ReadFile(m.store)
	if err != nil {
		return
	}
	var sf savedFile
	if json.Unmarshal(b, &sf) != nil {
		return
	}
	for _, s := range sf.Convos {
		fi, lo := m.place(s)
		if fi < 0 {
			m.unplaced = append(m.unplaced, s)
			continue
		}
		c := &convo{file: fi, lo: lo, hi: lo + len(s.Lines) - 1, session: s.Session,
			dir: s.Dir, readRepo: s.ReadRepo, commentID: s.CommentID, posted: s.Posted, created: s.Created}
		for _, t := range s.Turns {
			c.turns = append(c.turns, turn{q: t.Q, a: t.A, err: t.Err})
		}
		m.convos = append(m.convos, c)
	}
}

// place finds where a saved conversation's lines are in this diff,
// preferring the same line numbers when the lines appear more than once.
func (m *model) place(s savedConvo) (file, lo int) {
	file, lo = -1, -1
	for fi, f := range m.files {
		if f.Path() != s.Path || len(s.Lines) == 0 {
			continue
		}
	next:
		for i := 0; i+len(s.Lines) <= len(f.Lines); i++ {
			for j, sig := range s.Lines {
				if lineSig(f.Lines[i+j]) != sig {
					continue next
				}
			}
			if f.Lines[i].Old == s.Old && f.Lines[i].New == s.New {
				return fi, i
			}
			if lo < 0 {
				file, lo = fi, i
			}
		}
	}
	return file, lo
}

// saveConvos writes every conversation with at least one question.
func (m *model) saveConvos() {
	if m.store == "" {
		return
	}
	sf := savedFile{Convos: append([]savedConvo{}, m.unplaced...)}
	for _, c := range m.convos {
		if len(c.turns) == 0 {
			continue
		}
		f := m.files[c.file]
		s := savedConvo{Path: f.Path(), Old: f.Lines[c.lo].Old, New: f.Lines[c.lo].New,
			Session: c.session, Dir: c.dir, ReadRepo: c.readRepo,
			CommentID: c.commentID, Posted: c.posted, Created: c.created}
		for _, l := range f.Lines[c.lo : c.hi+1] {
			s.Lines = append(s.Lines, lineSig(l))
		}
		for _, t := range c.turns {
			if t.a == "" && !t.err {
				continue // still waiting for the answer
			}
			s.Turns = append(s.Turns, savedTurn{Q: t.q, A: t.a, Err: t.err})
		}
		sf.Convos = append(sf.Convos, s)
	}
	b, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(m.store), 0o755)
	tmp := m.store + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, m.store)
	}
}

// convosAt are the conversations covering line li of file fi, newest last.
func (m *model) convosAt(fi, li int) []*convo {
	var out []*convo
	for _, c := range m.convos {
		if c.file == fi && li >= c.lo && li <= c.hi && len(c.turns) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// convoMark is the gutter mark for row i: ● on the first row of a
// conversation's first line, │ on the rest, "" for none.
func (m *model) convoMark(i int) string {
	r := m.rows[i]
	if r.line < 0 {
		return ""
	}
	first := i == 0 || m.rows[i-1].file != r.file || m.rows[i-1].line != r.line
	mark := ""
	for _, c := range m.convos {
		if c.file != r.file || r.line < c.lo || r.line > c.hi || len(c.turns) == 0 {
			continue
		}
		if r.line == c.lo && first {
			return "●"
		}
		mark = "│"
	}
	return mark
}
