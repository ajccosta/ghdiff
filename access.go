package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// How far Claude may reach in a conversation. The consent question (asked
// once, when there is a local clone) offers each level by its key; a new
// level is one more entry.
type access struct {
	id    string   // saved with the conversation
	key   string   // its answer to the consent question
	hint  string   // the consent question's hint for key
	tools string   // claude --tools ("" none)
	args  []string // for claude -p and Claude Code
	// printArgs only for claude -p, where nobody can answer a prompt;
	// Claude Code starts in codePermissionMode instead.
	printArgs []string
	prompt    string // what the first prompt tells Claude about its directory

	// dir makes the directory claude runs in. It runs in the background
	// before the first question, so it may be slow (a fetch).
	dir func(m *model, c *convo) (string, error)
	// label is the pane title's word for it.
	label func(m *model, c *convo) string
}

var accesses = []access{
	{
		id: "diff", key: "n", hint: "diff only",
		prompt: "You only have the diff below; you cannot read other files.\n",
		dir: func(*model, *convo) (string, error) {
			d := filepath.Join(os.TempDir(), "ghdiff-claude")
			return d, os.MkdirAll(d, 0o755)
		},
		label: func(*model, *convo) string { return "diff only" },
	},
	{
		id: "read", key: "y", hint: "read the repo",
		tools: "Read,Grep,Glob", args: []string{"--allowedTools", "Read,Grep,Glob"},
		prompt: "The working directory is a clone of the repository the diff applies to; you may read files there " +
			"for context. It may not be checked out at the diff's commit, so trust the diff where they differ.\n",
		dir:   func(m *model, _ *convo) (string, error) { return m.repo, nil },
		label: func(m *model, _ *convo) string { return "reading " + filepath.Base(m.repo) },
	},
	{
		id: "work", key: "w", hint: "work in a worktree",
		// --restricted keeps the file tools in the worktree; Bash may only
		// run git, and not on another directory or a remote.
		tools: "Read,Grep,Glob,Edit,Write,Bash",
		args: []string{"--restricted",
			"--allowedTools", "Read,Grep,Glob,Bash(git:*)",
			"--disallowedTools", "Bash(git -C:*),Bash(git --git-dir:*),Bash(git --work-tree:*),Bash(git push:*)"},
		prompt: "The working directory is a git worktree of the repository the diff applies to, made for this " +
			"conversation and checked out at the diff's head (detached). It is yours: you may edit and write files " +
			"there, and run git (switch to another branch or commit, show history, commit your changes) to do what " +
			"they ask. Stay in this directory, and say what you changed and which commits you made.\n",
		printArgs: []string{"--permission-mode", "acceptEdits", "--permission-prompts", "none"},
		dir:       makeWorktree,
		label:     func(_ *model, c *convo) string { return "worktree " + c.dir },
	},
}

// accessFor is the level with id or key s, the diff-only one if none.
func accessFor(s string) access {
	for _, a := range accesses {
		if a.id == s || a.key == s {
			return a
		}
	}
	return accesses[0]
}

// consentHints offer every level, diff-only's last.
func consentHints() string {
	var hs []string
	for _, a := range accesses[1:] {
		hs = append(hs, a.key+" "+a.hint)
	}
	return strings.Join(append(hs, accesses[0].key+" "+accesses[0].hint, "esc cancel"), " · ")
}

// makeWorktree adds a detached worktree of the local clone under ghdiff's
// state folder, never the clone itself, at the PR's head (fetched) or the
// commit, else the clone's HEAD.
func makeWorktree(m *model, c *convo) (string, error) {
	if m.repo == "" {
		return "", fmt.Errorf("no local clone to make a worktree of")
	}
	base := stateDir()
	if base == "" {
		base = os.TempDir()
	}
	name := filepath.Base(m.repo)
	if m.target.pr > 0 {
		name += fmt.Sprintf("-pr%d", m.target.pr)
	}
	dir := filepath.Join(base, "worktrees", name+"-"+c.created.Format("20060102-150405"))
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", m.repo}, args...)...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	rev := "HEAD"
	switch t := m.target; {
	case t.commit != "":
		rev = t.commit
	case t.pr > 0 && t.repo != "":
		if _, err := git("fetch", "--quiet", "https://github.com/"+t.repo, fmt.Sprintf("pull/%d/head", t.pr)); err == nil {
			if sha, err := git("rev-parse", "FETCH_HEAD"); err == nil {
				rev = sha
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if _, err := git("worktree", "add", "--quiet", "--detach", dir, rev); err != nil {
		return "", err
	}
	return dir, nil
}
