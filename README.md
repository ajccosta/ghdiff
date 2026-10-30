# ghdiff

A terminal diff viewer laid out like GitHub's "Files changed" tab:

- unified diff with old and new line numbers
- the words that changed inside an edited line get a stronger background
  (`&& h->numFree() > 0` alone, not the whole line)
- syntax highlighting (chroma, GitHub's colours), by file name; each side is
  read as a whole, so a comment or string opened above a line still colours it
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
`C` collapse all · `t` hide tree · `h` or `?` help · `q` quit.

The current file's header is highlighted, like its name in the tree; `n`/`p`,
scrolling, the tree and clicking a header all move it.

Mouse: click a file header's arrow to collapse or expand it, the header to
make it the current file; click a line to select it and drag to extend (Option- or
Ctrl-click starts another range and keeps the others); drag the border between
the tree and the diff, or around the Claude pane, to resize. `+`/`-` make the
Claude pane bigger or smaller and `{`/`}` the tree narrower or wider; sizes
are remembered.

`v` shows a line cursor: move it with `↑↓`, press `v` again to start selecting,
then `↑↓` extend the selection, across hunks and into other files. `v` once
more keeps that range and shows the cursor again for the next one: select as
many ranges as you like. `Esc` cancels them all.
`y` copies the selected lines (with `+`/`-` when they mix removed and added
lines; a skipped part of the file between hunks is `⋮`, or the `@@` line in a
diff). With several ranges, each starts with its place (`model.go:L12–L18`).
`a` asks Claude about all of them at once.

## Ask Claude about a selection

With lines selected, `a` opens Claude Code itself, the terminal app with your
normal Claude login, in a pane under the diff. It is told about the diff and the
selected lines, and is used as usual: type to it, answer its questions, let it
edit (in a worktree, see below). It starts in auto mode (`Shift+Tab` changes
it). `Ctrl+O` switches to the diff and back. There,
`Ctrl+X` hides the pane while Claude Code keeps running, and `P` posts the
conversation. `/exit` ends it, and reopening the conversation resumes it
(`claude --resume`). `N` starts a new conversation on the same lines (the old
one is kept), and `<` `>` switch between the conversations there. The mouse
works over the pane as in a terminal: the wheel scrolls Claude Code, clicks
reach it. Quitting ghdiff stops Claude Code and keeps what was said.

With `"claude": "print"` in `~/.local/state/ghdiff/settings.json`, `a` asks with
`claude -p` instead: the question is typed in ghdiff and the answer opens in
the pane. There, `a` asks a follow-up in the same Claude session, `↑↓` or the
wheel scroll, `m` switches between formatted Markdown and Claude's raw text,
and `Esc` closes. Either way, `s` moves the pane to the right of the diff and
back below it, remembered for next time (`s` anywhere in ghdiff, or `Tab`
while typing a question, picks it before asking).

`Ctrl+O` leaves the pane open and hands the keys to the diff, like switching
panes in tmux: `s` side/below, scroll, change files, or select other lines
(`v`, then `a` asks about them). A question being typed is kept. `Ctrl+O` again, or `Esc` with nothing selected, goes back.
The same works in a review thread.

Conversations are saved per PR (or per local repo for `git diff`) in
`~/.local/state/ghdiff/conversations/`, so they are there the next time the diff
is opened. Lines with a conversation get a `●` (first line) and `│` in the
gutter, like a review comment but kept only in ghdiff (it goes to the PR only
if you press `P`). Hovering the `●` shows the conversation in a box, as for a
review thread's `◆`; clicking the `●` or the box, or `o` with the cursor (`v`)
on the line, reopens it to read or continue. They follow their lines by
content, so they stay put when other lines move.

The question prompt has the usual terminal editing keys: `Ctrl+A`/`Ctrl+E`
(start/end), `←→`, `Option+←→` or `Alt+B`/`Alt+F` (by word), `Ctrl+W` and
`Option+Backspace` (delete word), `Alt+D` (delete next word), `Ctrl+U`/`Ctrl+K`
(delete to start/end), `Delete`/`Ctrl+D`.

`P` posts the conversation to the PR (after a y/n) as a review comment on the
selected lines (one range in one hunk, as GitHub comments are), at the PR's head commit (or the commit being viewed). Posting
again after more follow-ups replies in the same thread with just the new
questions and answers. This needs the PR: open the diff from gh-dash, or use
`ghdiff --pr N -R owner/repo`.

Claude gets the selected lines, their hunk and the whole diff. When there is a
local clone with the diff's files (gh-dash's `repoPaths`, or the repo ghdiff
runs in, if its remote is the PR's repo), the first question asks how Claude
may use that repo (the answer holds for the rest of the session):

- `y`: read it, with Read, Grep and Glob there and nothing else.
- `w`: work in a new git worktree of it, under
  `~/.local/state/ghdiff/worktrees/`, never your own checkout. It starts
  detached at the PR's head (fetched), or the commit, else the clone's `HEAD`.
  Claude may edit and write files there (`claude --restricted` keeps them in the
  worktree) and run git (switch branches or commits, commit a fix), but not
  `git -C` elsewhere or `git push`. The pane's title shows the worktree's path;
  each conversation gets its own and keeps it. Remove one with
  `git worktree remove PATH`.
- `n`: no tools at all, only the diff.

## GitHub review threads

On a PR diff (from gh-dash, or `--pr N -R owner/repo`), the PR's review threads
are fetched in the background and marked `◆` on their first line. Each
comment shows with its author and date, rendered as Markdown (`m` raw). `T`
switches how threads show (remembered in `settings.json` as `threadView`):

- `hover` (the default): a box under the `◆` (and a conversation's `●`) while
  the mouse is on it (or on the box), or the cursor is on that line.
- `left`: no box; put the cursor on the line and press `r`.

Clicking the `◆` or the box, or `r`, opens the thread in the file tree's column, where
`t` switches between the tree and the thread. (`"threadView": "pane"` puts
open threads in the pane Claude uses instead; `T` skips it.)

In an open thread `a` writes a reply and `Enter` posts it; `<` `>` switch
between threads on the same lines, `Esc` closes. Outdated threads, and threads
on single-commit diffs, are not shown, since their line numbers do not match.

## With gh-dash

```yaml
pager:
  diff: ghdiff   # or its full path, if it is not on your PATH
```

`d` on a PR then opens the whole PR diff, and `Enter` on a commit (Commits tab,
`navigation: arrows`) opens that commit. gh-dash sets `GHDIFF_TITLE` for the
title bar, and `GHDIFF_REPO`, `GHDIFF_PR`, `GHDIFF_COMMIT` and `GHDIFF_REPO_PATH`
(its `repoPaths` entry) so Ask Claude knows the PR and can read the local clone.
Clicking a review comment's `path#L12` on the Activity tab also sets
`GHDIFF_GOTO=path:RIGHT|LEFT:start:end`: ghdiff opens at those lines with
them selected, and shows the review thread there as `T` is set.

Opened from gh-dash, the left pane also has the PR's commits: `Tab` goes from
the diff to Files, then to Commits, then back (or click their names on top).
`↑↓` and `Enter` (or a click) show a commit's diff in place of the PR's, and
"All commits" (when there is more than one) the whole PR again. The diffs are
fetched once each. Claude conversations follow their lines into the new diff,
a running Claude Code with them; one whose lines are not in it stops, and
resumes when it is opened again.

## As git's diff pager

```
git config --global pager.diff ghdiff
```

Only `git diff` changes; `git log`/`git show` keep `core.pager`. An empty diff
exits at once, and `git diff --stat` / `--name-only` print as usual.

## Building

`go build -o ghdiff .` with Go 1.27 or newer, or with Nix (below).

## With Nix

```
nix profile install github:ajccosta/ghdiff
git config --global pager.diff ghdiff
```

or `nix run github:ajccosta/ghdiff`, or the flake's `overlays.default` (adds
`pkgs.ghdiff`). The package brings git and gh; Ask Claude needs `claude` on
your PATH. After changing go.mod, update `vendorHash` in package.nix (set it to
`lib.fakeHash` and build; nix prints the right one).
