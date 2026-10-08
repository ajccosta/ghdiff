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

## With gh-dash

```yaml
pager:
  diff: ghdiff   # or its full path, if it is not on your PATH
```

`d` on a PR then opens the whole PR diff, and `Enter` on a commit (Commits tab,
`navigation: arrows`) opens that commit. gh-dash sets `GHDIFF_TITLE` for the
title bar.

## Building

`go build -o ghdiff .` with Go 1.27 or newer, or with Nix (below).
