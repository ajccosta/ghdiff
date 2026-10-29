package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorktreeIsNeverTheClone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	m := &model{repo: repo}
	c := &convo{created: time.Now()}
	dir, err := accessFor("work").dir(m, c)
	if err != nil {
		t.Fatal(err)
	}
	if dir == repo || !strings.HasPrefix(dir, filepath.Join(stateDir(), "worktrees")) {
		t.Fatalf("worktree at %s (clone %s)", dir, repo)
	}
	head := func(d string) string {
		out, _ := exec.Command("git", "-C", d, "rev-parse", "HEAD").Output()
		return strings.TrimSpace(string(out))
	}
	if head(dir) == "" || head(dir) != head(repo) {
		t.Fatalf("worktree HEAD %q, clone %q", head(dir), head(repo))
	}
}

func TestEveryAccessIsOffered(t *testing.T) {
	h := consentHints()
	for _, a := range accesses {
		if !strings.Contains(h, a.key+" "+a.hint) {
			t.Errorf("%q missing from %q", a.id, h)
		}
		if accessFor(a.key).id != a.id || accessFor(a.id).id != a.id {
			t.Errorf("accessFor(%q) is not %q", a.key, a.id)
		}
	}
}
