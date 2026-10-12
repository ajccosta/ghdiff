# ghdiff

A terminal diff viewer laid out like GitHub's "Files changed" tab:

- unified diff with old and new line numbers
- the words that changed inside an edited line get a stronger background
  (`&& h->numFree() > 0` alone, not the whole line)
- file tree on the left (`←` / `Tab`), sticky file headers, collapsible files
- GitHub's colours on truecolor terminals, a stronger palette on 256-colour
  ones (Terminal.app, tmux); follows a light or dark background

```
ghdiff [file.diff]                 # or pipe a diff in on stdin
ghdiff --git [git diff args...]
ghdiff --pr N [-R owner/repo]
ghdiff --commit SHA [-R owner/repo]
```

Keys: `↑↓`/`jk` scroll · `space`/`b` page · `n`/`p` next/previous file ·
`←` or `Tab` file tree (`↑↓` pick, `Enter`/`→` open) · `c` collapse file ·
`C` collapse all · `t` hide tree · `q`/`Esc` quit.

`v` shows a line cursor: move it with `↑↓`, press `v` again to start selecting,
then `↑↓` extend the selection (within one hunk). `Esc` cancels at any point.
`y` copies the selected lines (with `+`/`-` when they mix removed and added
lines).

## Ask Claude about a selection

With lines selected, `a` asks Claude Code about them (`claude -p`, your normal
Claude login; no API key). The answer opens in a pane under the diff: `a` asks a
follow-up in the same Claude session, `N` starts a new conversation on the same
lines (the old one is kept), `<` `>` switch between the conversations there,
`↑↓` scroll, `m` switches between formatted Markdown and Claude's raw text,
`s` moves the pane to the right of the diff and back below it, remembered for
next time (`s` anywhere in ghdiff, or `Tab` while typing a question, picks it
before asking), `Esc` closes.

`Ctrl+O` leaves the pane open and hands the keys to the diff, like switching
panes in tmux: scroll, change files, select other lines (`Esc` drops the
selection, `v` starts a new one, `a` asks about it), `s` side/below. A question
being typed is kept. `Ctrl+O` again, or `Esc` with nothing selected, goes back.
The same works in a review thread.

Conversations are saved per PR (or per local repo for `git diff`) in
`~/.local/state/ghdiff/conversations/`, so they are there the next time the diff
is opened. Lines with a conversation get a `●` (first line) and `│` in the
gutter; put the cursor (`v`) on one and press `o` to read or continue it. They
follow their lines by content, so they stay put when other lines move.

The question prompt has the usual terminal editing keys: `Ctrl+A`/`Ctrl+E`
(start/end), `←→`, `Option+←→` or `Alt+B`/`Alt+F` (by word), `Ctrl+W` and
`Option+Backspace` (delete word), `Alt+D` (delete next word), `Ctrl+U`/`Ctrl+K`
(delete to start/end), `Delete`/`Ctrl+D`.

`P` posts the conversation to the PR (after a y/n) as a review comment on the
selected lines, at the PR's head commit (or the commit being viewed). Posting
again after more follow-ups replies in the same thread with just the new
questions and answers. This needs the PR: open the diff from gh-dash, or use
`ghdiff --pr N -R owner/repo`.

Claude gets the selected lines, their hunk and the whole diff. When there is a
local clone with the diff's files (gh-dash's `repoPaths`, or the repo ghdiff
runs in, if its remote is the PR's repo), the first question asks whether
Claude may read that repo; yes gives it Read, Grep and Glob there and nothing
else, no gives it no tools at all.

## GitHub review threads

On a PR diff (from gh-dash, or `--pr N -R owner/repo`), the PR's review threads
are fetched in the background and marked `◆` on their first line. Put the
cursor (`v`) on one and press `r` to read it in the same pane as Claude: each
comment with its author and date, rendered as Markdown (`m` raw). `a` writes a
reply and `Enter` posts it; `<` `>` switch between threads on the same lines,
`s` side/below, `Esc` closes. Outdated threads, and threads on single-commit
diffs, are not shown, since their line numbers do not match.

## With gh-dash

```yaml
pager:
  diff: ghdiff   # or its full path, if it is not on your PATH
```

`d` on a PR then opens the whole PR diff, and `Enter` on a commit (Commits tab,
`navigation: arrows`) opens that commit. gh-dash sets `GHDIFF_TITLE` for the
title bar, and `GHDIFF_REPO`, `GHDIFF_PR`, `GHDIFF_COMMIT` and `GHDIFF_REPO_PATH`
(its `repoPaths` entry) so Ask Claude knows the PR and can read the local clone.

## As git's diff pager

```
git config --global pager.diff ghdiff
```

Only `git diff` changes; `git log`/`git show` keep `core.pager`. An empty diff
exits at once, and `git diff --stat` / `--name-only` print as usual.

## Building

`go build -o ghdiff .` with Go 1.27 or newer, or with Nix (below).
