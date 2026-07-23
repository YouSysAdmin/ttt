package gitlog_test

import (
	"os/exec"
	"testing"
	"time"

	"ttt/internal/core/gitlog"
)

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commit(t *testing.T, dir, msg string, env ...string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", msg)
	cmd.Env = append(cmd.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit %q: %v\n%s", msg, err, out)
	}
}

func TestCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "me@example.com")
	git(t, dir, "config", "user.name", "me")

	before := time.Now().Add(-time.Minute)
	commit(t, dir, "mine one")
	commit(t, dir, "mine two")
	// A teammate's commit in the window must be filtered out by author.
	commit(t, dir, "not mine",
		"GIT_AUTHOR_NAME=other", "GIT_AUTHOR_EMAIL=other@example.com")
	after := time.Now().Add(time.Minute)

	commits, err := gitlog.Commits(dir, before, after)
	if err != nil {
		t.Fatalf("commits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d: %+v", len(commits), commits)
	}
	// Oldest first, "hash subject" text.
	if got := commits[0].Text; len(got) < 8 || got[8:] != "mine one" {
		t.Fatalf("unexpected first commit text: %q", got)
	}
	if got := commits[1].Text; got[8:] != "mine two" {
		t.Fatalf("unexpected second commit text: %q", got)
	}

	// A window before any commit yields nothing.
	empty, err := gitlog.Commits(dir, before.Add(-time.Hour), before)
	if err != nil {
		t.Fatalf("empty window: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no commits, got %+v", empty)
	}

	// A non-repo directory errors.
	if _, err := gitlog.Commits(t.TempDir(), before, after); err == nil {
		t.Fatal("expected error for non-repo dir")
	}
}
