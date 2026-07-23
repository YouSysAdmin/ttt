// Package gitlog shells out to git to list commits in a time window, for
// importing commit messages into a task's notes when a tracking session ends.
package gitlog

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ResolveRepo normalizes a repo path to absolute, so the value stored on a
// task keeps working no matter which directory later commands run from. The
// path must exist and be a directory.
func ResolveRepo(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("repo path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repo path %s is not a directory", abs)
	}
	return abs, nil
}

// Commit is one imported commit: its commit time and "abbrev-hash subject".
type Commit struct {
	Time time.Time
	Text string
}

// Commits returns the repo's commits (current branch, oldest first) committed
// between since and until, limited to the repo's configured user when
// user.email is set - merges and pulls land teammates' commits in the window,
// and those aren't this session's work.
func Commits(repo string, since, until time.Time) ([]Commit, error) {
	args := []string{"-C", repo, "log", "--reverse",
		"--since=" + since.Format(time.RFC3339),
		"--until=" + until.Format(time.RFC3339),
		"--pretty=format:%ct %h %s"}
	if email, err := exec.Command("git", "-C", repo, "config", "user.email").Output(); err == nil {
		if e := strings.TrimSpace(string(email)); e != "" {
			args = append(args, "--author="+e)
		}
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("git log in %s: %s", repo, msg)
	}

	var commits []Commit
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		epoch, rest, _ := strings.Cut(line, " ")
		sec, err := strconv.ParseInt(epoch, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse git log line %q: %w", line, err)
		}
		commits = append(commits, Commit{Time: time.Unix(sec, 0), Text: rest})
	}
	return commits, nil
}
