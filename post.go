package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Posting a conversation to the PR as a review comment on the selected lines.
// The first post starts a thread; later ones reply to it with the new turns.

type postedMsg struct {
	c     *convo
	id    int64
	url   string
	turns int // turns covered by this post
	err   string
}

func (m *model) canPost() bool {
	return m.target.repo != "" && m.target.pr > 0 && m.chat != nil &&
		!m.chat.busy && m.chat.answered() > m.chat.posted
}

// answered counts the turns with an answer, leaving out a trailing error.
func (c *convo) answered() int {
	n := 0
	for _, t := range c.turns {
		if t.a == "" || t.err {
			break
		}
		n++
	}
	return n
}

func (m *model) postBody(c *convo) string {
	var b strings.Builder
	if c.posted == 0 {
		fmt.Fprintf(&b, "*Asked Claude about these lines (from [ghdiff](https://github.com/ajccosta/ghdiff)):*\n\n")
	}
	for i, t := range c.turns[c.posted:c.answered()] {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		for _, l := range strings.Split(t.q, "\n") {
			b.WriteString("> " + l + "\n")
		}
		b.WriteString("\n" + t.a)
	}
	return b.String()
}

// side and number of a diff line as GitHub's review comments name them.
func ghSide(l Line) (string, int) {
	if l.Kind == Del {
		return "LEFT", l.Old
	}
	return "RIGHT", l.New
}

// postBlocked is why the conversation can't be a GitHub review comment, or
// "": a comment is on one range of lines in one hunk.
func (m *model) postBlocked() string {
	if len(m.chat.spans) > 1 {
		return "Only a conversation on one range of lines can be posted; this one has several."
	}
	return m.spansBlocked(m.chat.spans)
}

// spansBlocked is why the lines can't take one GitHub review comment, or "".
func (m *model) spansBlocked(spans []span) string {
	switch {
	case len(spans) == 0:
		return "Select the lines first (v, then v again)"
	case len(spans) > 1:
		return "A GitHub comment is on one range of lines; this selection has several."
	}
	sp := spans[0]
	for _, l := range m.files[sp.file].Lines[sp.lo : sp.hi+1] {
		if l.Kind == Hunk {
			return "A GitHub comment can't span hunks; these lines do."
		}
	}
	return ""
}

// spanEnds is the first and last code lines of sp.
func (m *model) spanEnds(sp span) (first, last Line) {
	found := false
	for _, l := range m.files[sp.file].Lines[sp.lo : sp.hi+1] {
		if selectable(l) {
			if !found {
				first, found = l, true
			}
			last = l
		}
	}
	return first, last
}

// ghError is gh's own message for a failed command.
func ghError(err error, out []byte) string {
	msg := strings.TrimSpace(string(out))
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		msg = strings.TrimSpace(string(ee.Stderr)) + " " + msg
	}
	if msg == "" {
		msg = err.Error()
	}
	return msg
}

// newComment starts a review thread on first..last of f; it blocks, so it
// runs inside a tea.Cmd. It gives the comment's id and URL, or gh's error.
func newComment(t target, f *File, first, last Line, body string) (int64, string, string) {
	commit := t.commit
	if commit == "" {
		out, err := exec.Command("gh", "pr", "view", fmt.Sprint(t.pr), "-R", t.repo,
			"--json", "headRefOid", "-q", ".headRefOid").Output()
		if err != nil {
			return 0, "", ghError(err, out)
		}
		commit = strings.TrimSpace(string(out))
	}
	req := map[string]any{"body": body, "commit_id": commit, "path": f.Path()}
	side, line := ghSide(last)
	req["side"], req["line"] = side, line
	if s, l := ghSide(first); s != side || l != line {
		req["start_side"], req["start_line"] = s, l
	}
	return ghPost(fmt.Sprintf("repos/%s/pulls/%d/comments", t.repo, t.pr), req)
}

func ghPost(endpoint string, req map[string]any) (int64, string, string) {
	in, _ := json.Marshal(req)
	cmd := exec.Command("gh", "api", "-X", "POST", endpoint, "--input", "-")
	cmd.Stdin = bytes.NewReader(in)
	out, err := cmd.Output()
	if err != nil {
		return 0, "", ghError(err, out)
	}
	var res struct {
		ID  int64  `json:"id"`
		URL string `json:"html_url"`
	}
	json.Unmarshal(out, &res)
	return res.ID, res.URL, ""
}

func (m *model) post() tea.Cmd {
	c := m.chat
	t := m.target
	sp := c.spans[0]
	f := m.files[sp.file]
	body := m.postBody(c)
	upTo := c.answered()
	first, last := m.spanEnds(sp)
	replyTo := c.commentID

	return func() tea.Msg {
		var id int64
		var url, err string
		if replyTo != 0 {
			id, url, err = ghPost(fmt.Sprintf("repos/%s/pulls/%d/comments/%d/replies", t.repo, t.pr, replyTo),
				map[string]any{"body": body})
		} else {
			id, url, err = newComment(t, f, first, last, body)
		}
		return postedMsg{c: c, id: id, url: url, turns: upTo, err: err}
	}
}

func (m *model) gotPosted(msg postedMsg) {
	c := msg.c
	c.posting = false
	if msg.err != "" {
		c.postNote, c.postErr = "Posting failed: "+msg.err, true
		return
	}
	if c.commentID == 0 {
		c.commentID = msg.id
	}
	c.posted = msg.turns
	c.postNote, c.postErr = "Posted: "+msg.url, false
	c.scroll = 1 << 30
	m.saveConvos()
}
