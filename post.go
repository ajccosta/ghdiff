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

func (m *model) post() tea.Cmd {
	c := m.chat
	t := m.target
	f := m.files[c.file]
	body := m.postBody(c)
	upTo := c.answered()

	// The selection's first and last code lines.
	var first, last Line
	found := false
	for _, l := range f.Lines[c.lo : c.hi+1] {
		if selectable(l) {
			if !found {
				first, found = l, true
			}
			last = l
		}
	}
	replyTo := c.commentID

	return func() tea.Msg {
		fail := func(err error, out []byte) tea.Msg {
			msg := strings.TrimSpace(string(out))
			if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
				msg = strings.TrimSpace(string(ee.Stderr)) + " " + msg
			}
			if msg == "" {
				msg = err.Error()
			}
			return postedMsg{c: c, err: msg}
		}

		var endpoint string
		req := map[string]any{"body": body}
		if replyTo != 0 {
			endpoint = fmt.Sprintf("repos/%s/pulls/%d/comments/%d/replies", t.repo, t.pr, replyTo)
		} else {
			commit := t.commit
			if commit == "" {
				out, err := exec.Command("gh", "pr", "view", fmt.Sprint(t.pr), "-R", t.repo,
					"--json", "headRefOid", "-q", ".headRefOid").Output()
				if err != nil {
					return fail(err, out)
				}
				commit = strings.TrimSpace(string(out))
			}
			endpoint = fmt.Sprintf("repos/%s/pulls/%d/comments", t.repo, t.pr)
			side, line := ghSide(last)
			req["commit_id"], req["path"], req["side"], req["line"] = commit, f.Path(), side, line
			if s, l := ghSide(first); s != side || l != line {
				req["start_side"], req["start_line"] = s, l
			}
		}

		in, _ := json.Marshal(req)
		cmd := exec.Command("gh", "api", "-X", "POST", endpoint, "--input", "-")
		cmd.Stdin = bytes.NewReader(in)
		out, err := cmd.Output()
		if err != nil {
			return fail(err, out)
		}
		var res struct {
			ID  int64  `json:"id"`
			URL string `json:"html_url"`
		}
		json.Unmarshal(out, &res)
		return postedMsg{c: c, id: res.ID, url: res.URL, turns: upTo}
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
}
