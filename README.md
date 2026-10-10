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
follow-up in the same Claude session, `↑↓` scroll, `Esc` closes. Reopening the
same selection picks the conversation up again.

Claude gets the selected lines, their hunk and the whole diff. When ghdiff runs
inside a clone that has the diff's files, the first question asks whether
Claude may read that repo; yes gives it Read, Grep and Glob there and nothing
else, no gives it no tools at all.

## With gh-dash

```yaml
pager:
  diff: ghdiff   # or its full path, if it is not on your PATH
```

`d` on a PR then opens the whole PR diff, and `Enter` on a commit (Commits tab,
`navigation: arrows`) opens that commit. gh-dash sets `GHDIFF_TITLE` for the
title bar.

## As git's diff pager

```
git config --global pager.diff ghdiff
```

Only `git diff` changes; `git log`/`git show` keep `core.pager`. An empty diff
exits at once, and `git diff --stat` / `--name-only` print as usual.

## Building

`go build -o ghdiff .` with Go 1.27 or newer, or with Nix (below).
