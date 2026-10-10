// ghdiff is a terminal diff viewer that looks like GitHub's "Files changed"
// tab: a file tree, a unified diff with both line numbers, and the changed
// words inside an edited line highlighted.
//
//	ghdiff [file.diff]               read a diff from a file, or stdin
//	ghdiff --git [git diff args...]  view git diff in the current repo
//	ghdiff --pr N [-R owner/repo]    view a pull request (gh pr diff)
//	ghdiff --commit SHA [-R owner/repo]
//
// --title sets the title bar (default: $GHDIFF_TITLE).
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage: ghdiff [--title T] [file.diff]
       ghdiff [--title T] --git [git diff args...]
       ghdiff [--title T] --pr N [-R owner/repo]
       ghdiff [--title T] --commit SHA [-R owner/repo]`)
	os.Exit(2)
}

func run(name string, args ...string) []byte {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		fmt.Fprintf(os.Stderr, "ghdiff: %s %s: %s\n", name, strings.Join(args, " "), msg)
		os.Exit(1)
	}
	return out
}

func main() {
	args := os.Args[1:]
	title := os.Getenv("GHDIFF_TITLE")
	repo := ""
	var input []byte

	// Flags that may appear anywhere before the mode.
	for len(args) >= 2 && (args[0] == "--title" || args[0] == "-R") {
		if args[0] == "--title" {
			title = args[1]
		} else {
			repo = args[1]
		}
		args = args[2:]
	}
	takeRepo := func(rest []string) []string {
		for i := 0; i+1 < len(rest); i++ {
			if rest[i] == "-R" || rest[i] == "--repo" {
				repo = rest[i+1]
				return append(rest[:i:i], rest[i+2:]...)
			}
		}
		return rest
	}

	switch {
	case len(args) > 0 && (args[0] == "-h" || args[0] == "--help"):
		usage()
	case len(args) > 0 && args[0] == "--git":
		input = run("git", append([]string{"diff", "--no-color", "--no-ext-diff"}, args[1:]...)...)
		if title == "" {
			title = strings.TrimSpace("git diff " + strings.Join(args[1:], " "))
		}
	case len(args) >= 2 && args[0] == "--pr":
		rest := takeRepo(args[2:])
		ghArgs := []string{"pr", "diff", args[1], "--color", "never"}
		if repo != "" {
			ghArgs = append(ghArgs, "-R", repo)
		}
		input = run("gh", append(ghArgs, rest...)...)
		if title == "" {
			title = strings.TrimSpace(repo + "#" + args[1])
		}
	case len(args) >= 2 && args[0] == "--commit":
		takeRepo(args[2:])
		sha := args[1]
		if repo != "" {
			input = run("gh", "api", "-H", "Accept: application/vnd.github.diff",
				fmt.Sprintf("repos/%s/commits/%s", repo, sha))
		} else {
			input = run("git", "show", "--no-color", "--no-ext-diff", "--format=", sha)
		}
		if title == "" {
			title = strings.TrimSpace(repo + " @ " + sha[:min(7, len(sha))])
		}
	case len(args) == 1:
		b, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "ghdiff:", err)
			os.Exit(1)
		}
		input = b
	case len(args) == 0:
		if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			usage()
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ghdiff:", err)
			os.Exit(1)
		}
		input = b
	default:
		usage()
	}

	if title == "" {
		if wd, err := os.Getwd(); err == nil {
			title = filepath.Base(wd) // e.g. as git's pager: the repo's folder
		}
	}

	files := Parse(bytes.NewReader(input))
	// As git's pager: nothing to show for an empty diff, and output that is
	// not a diff (git diff --stat, --name-only) is printed as it is.
	if len(files) == 0 {
		os.Stdout.Write(input)
		return
	}
	// With stdin used for the diff, bubbletea reads keys from the terminal.
	p := tea.NewProgram(newModel(files, title, input))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "ghdiff:", err)
		os.Exit(1)
	}
}
