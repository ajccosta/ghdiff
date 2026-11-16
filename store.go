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

	tea "charm.land/bubbletea/v2"
)

// Conversations are kept on disk per diff (a PR, or a local repo), so they
// show up again the next time the same diff is opened. A conversation is tied
// to its lines by their content, so it stays put when other lines move.

type savedTurn struct {
	Q   string `json:"q"`
	A   string `json:"a"`
	Err bool   `json:"err,omitempty"`
}

// savedRange is one range of a conversation's lines.
type savedRange struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"` // "+text", "-text", " text" for lo..hi
	Old   int      `json:"old"`   // the first line's numbers, to prefer
	New   int      `json:"new"`
}

type savedConvo struct {
	savedRange              // the first range
	More       []savedRange `json:"more,omitempty"` // the others
	Turns      []savedTurn  `json:"turns"`
	Session    string       `json:"session,omitempty"`
	Dir        string       `json:"dir,omitempty"` // where claude ran; resume needs it
	Access     string       `json:"access,omitempty"`
	Code       bool         `json:"code,omitempty"`     // Claude Code, Session its session
	ReadRepo   bool         `json:"readRepo,omitempty"` // before access: true is "read"
	CommentID  int64        `json:"commentId,omitempty"`
	Posted     int          `json:"posted,omitempty"`
	Created    time.Time    `json:"created"`
}

type savedFile struct {
	Convos []savedConvo `json:"convos"`
}

// settings are ghdiff's remembered choices, in settings.json next to the
// conversations.
type settings struct {
	ChatSide   bool       `json:"chatSide"`
	ThreadView threadMode `json:"threadView,omitempty"`
	Claude     string     `json:"claude,omitempty"` // "code" (default) or "print"
	MdSource   bool       `json:"markdownSource,omitempty"`
	Split      bool       `json:"split,omitempty"`
	sizes
}

// stateDir is ghdiff's folder under $XDG_STATE_HOME (~/.local/state).
func stateDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "ghdiff")
}

func settingsPath() string { return filepath.Join(stateDir(), "settings.json") }

func (m *model) loadSettings() {
	var s settings
	if b, err := os.ReadFile(settingsPath()); err == nil && json.Unmarshal(b, &s) == nil {
		m.side, m.sizes, m.ThreadView, m.Claude, m.MdSource, m.Split = s.ChatSide, s.sizes, s.ThreadView, s.Claude, s.MdSource, s.Split
	}
}

func (m *model) saveSettings() {
	if stateDir() == "" {
		return
	}
	p := settingsPath()
	b, _ := json.MarshalIndent(settings{ChatSide: m.side, ThreadView: m.ThreadView, Claude: m.Claude, MdSource: m.MdSource, Split: m.Split, sizes: m.sizes}, "", "  ")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, b, 0o600)
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
	if stateDir() == "" {
		return ""
	}
	return filepath.Join(stateDir(), "conversations", unsafeRe.ReplaceAllString(key, "_")+".json")
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
	sf, mod := m.readStore()
	m.storeMod = mod
	for _, s := range sf.Convos {
		m.addSaved(s)
	}
}

// readStore is the saved file, and when it last changed (zero if missing).
func (m *model) readStore() (savedFile, time.Time) {
	var sf savedFile
	st, err := os.Stat(m.store)
	if err != nil {
		return sf, time.Time{}
	}
	if b, err := os.ReadFile(m.store); err != nil || json.Unmarshal(b, &sf) != nil {
		sf = savedFile{}
	}
	return sf, st.ModTime()
}

// addSaved places a saved conversation on its lines, or keeps it aside.
func (m *model) addSaved(s savedConvo) {
	var spans []span
	var rs []savedRange // none for the whole diff
	if s.Path != "" {
		rs = append([]savedRange{s.savedRange}, s.More...)
	}
	for _, r := range rs {
		fi, lo := m.place(r)
		if fi < 0 {
			m.unplaced = append(m.unplaced, s)
			return
		}
		spans = append(spans, span{fi, lo, lo + len(r.Lines) - 1})
	}
	if s.Access == "" && s.ReadRepo {
		s.Access = "read"
	}
	c := &convo{spans: spans, session: s.Session,
		dir: s.Dir, access: s.Access, code: s.Code, commentID: s.CommentID, posted: s.Posted, created: s.Created}
	m.takeTurns(c, s)
	m.convos = append(m.convos, c)
}

func (m *model) takeTurns(c *convo, s savedConvo) {
	c.saved = savedSig(s)
	c.turns = nil
	for _, t := range s.Turns {
		c.turns = append(c.turns, turn{q: t.Q, a: t.A, err: t.Err})
	}
	m.refreshTurns(c)
}

// savedSig is how many turns s has and its last, to see when another window
// changed it.
func savedSig(s savedConvo) string {
	if n := len(s.Turns); n > 0 {
		return strconv.Itoa(n) + s.Turns[n-1].Q + s.Turns[n-1].A
	}
	return ""
}

// convoKey tells conversations apart across ghdiff windows: by session, or
// by when it started while it has none.
func convoKey(session string, created time.Time) string {
	if session != "" {
		return session
	}
	return strconv.FormatInt(created.UnixNano(), 10)
}

// syncStore takes in what other ghdiff windows saved since this one last
// read or wrote the file: conversations it doesn't have, and the turns of
// ones another window changed (unless running or waiting on them here). True
// if anything changed.
func (m *model) syncStore() bool {
	if m.store == "" {
		return false
	}
	sf, mod := m.readStore()
	if mod.Equal(m.storeMod) {
		return false
	}
	m.storeMod = mod
	known := map[string]*convo{}
	for _, c := range m.convos {
		known[convoKey(c.session, c.created)] = c
	}
	aside := map[string]int{}
	for i, s := range m.unplaced {
		aside[convoKey(s.Session, s.Created)] = i
	}
	changed := false
	for _, s := range sf.Convos {
		k := convoKey(s.Session, s.Created)
		if c, ok := known[k]; ok {
			if savedSig(s) != c.saved && c.term == nil && !c.busy {
				before := lastTurn(c)
				m.takeTurns(c, s)
				c.commentID, c.posted = s.CommentID, s.Posted
				changed = changed || lastTurn(c) != before
			}
		} else if i, ok := aside[k]; ok {
			m.unplaced[i] = s
		} else {
			m.addSaved(s)
			changed = true
		}
	}
	return changed
}

// place finds where a saved conversation's lines are in this diff,
// preferring the same line numbers when the lines appear more than once.
func (m *model) place(s savedRange) (file, lo int) {
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

// saveConvos writes every conversation with at least one question, with
// what other ghdiff windows saved meanwhile.
func (m *model) saveConvos() {
	if m.store == "" {
		return
	}
	m.syncStore()
	sf := savedFile{Convos: append([]savedConvo{}, m.unplaced...)}
	for _, c := range m.convos {
		if len(c.turns) == 0 {
			continue
		}
		s := savedConvo{Session: c.session, Dir: c.dir, Access: c.access, Code: c.code,
			CommentID: c.commentID, Posted: c.posted, Created: c.created}
		for i, sp := range c.spans {
			f := m.files[sp.file]
			r := savedRange{Path: f.Path(), Old: f.Lines[sp.lo].Old, New: f.Lines[sp.lo].New}
			for _, l := range f.Lines[sp.lo : sp.hi+1] {
				r.Lines = append(r.Lines, lineSig(l))
			}
			if i == 0 {
				s.savedRange = r
			} else {
				s.More = append(s.More, r)
			}
		}
		for _, t := range c.turns {
			if t.a == "" && !t.err {
				continue // still waiting for the answer
			}
			s.Turns = append(s.Turns, savedTurn{Q: t.q, A: t.a, Err: t.err})
		}
		c.saved = savedSig(s)
		sf.Convos = append(sf.Convos, s)
	}
	b, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(m.store), 0o755)
	tmp := m.store + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, m.store) == nil {
		if st, err := os.Stat(m.store); err == nil {
			m.storeMod = st.ModTime()
		}
	}
}

type watchMsg struct{}

// watch is the next look at the running Claude Codes' transcripts and at the
// saved file: a conversation is kept from its first question on, and other
// ghdiff windows' conversations show up here.
func watch() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return watchMsg{} })
}

func (m *model) watched() {
	asked := false
	for _, c := range m.convos {
		if c.term == nil {
			continue
		}
		st, err := os.Stat(transcriptPath(c.session))
		if err != nil || st.Size() == c.seen {
			continue
		}
		c.seen = st.Size()
		before := lastTurn(c)
		m.refreshTurns(c)
		asked = asked || lastTurn(c) != before
	}
	if asked {
		m.saveConvos()
	} else {
		m.syncStore()
	}
}

// lastTurn is how many turns c has and its last, to see when they change.
func lastTurn(c *convo) string {
	if n := len(c.turns); n > 0 {
		return strconv.Itoa(n) + c.turns[n-1].q + c.turns[n-1].a
	}
	return ""
}

// covers reports whether line li of file fi is in one of c's ranges.
func (c *convo) covers(fi, li int) bool {
	for _, s := range c.spans {
		if s.covers(fi, li) {
			return true
		}
	}
	return false
}

// convosAt are the conversations covering line li of file fi, newest last.
func (m *model) convosAt(fi, li int) []*convo {
	var out []*convo
	for _, c := range m.convos {
		if c.covers(fi, li) && len(c.turns) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// convoMark is the gutter mark for row i: on the first row of a review
// thread's first line ◆ (and github true), of a Claude conversation's ●, │ on
// their other lines, "" for none.
func (m *model) convoMark(i int) (mark string, github bool) {
	r := m.rows[i]
	if r.line < 0 {
		return "", false
	}
	first := i == 0 || m.rows[i-1].file != r.file || m.rows[i-1].line != r.line
	for _, t := range m.threads {
		if t.file != r.file || r.line < t.lo || r.line > t.hi {
			continue
		}
		if r.line == t.lo && first {
			return "◆", true
		}
		mark, github = "│", true
	}
	for _, c := range m.convos {
		if !c.covers(r.file, r.line) || len(c.turns) == 0 {
			continue
		}
		if s := c.spans[0]; s.file == r.file && r.line == s.lo && first {
			return "●", false
		}
		if mark == "" {
			mark = "│"
		}
	}
	return mark, github
}
