package tracker

import (
	"time"

	"ttt/internal/core/env"
	"ttt/internal/core/errs"
	"ttt/internal/core/gitlog"
	"ttt/internal/domain/store"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	"ttt/internal/models/tracker"
)

type Handler struct {
	Runtime *env.Runtime
	Store   *store.Store
}

// Status is a point-in-time view of what's being tracked. State and Task are
// nil when idle. Elapsed is the current session, and Total is the task's
// all-time tracked total including the running session.
type Status struct {
	State   *tracker.State
	Task    *task.Task
	Elapsed time.Duration
	Total   time.Duration
}

// Status reports the current tracking state, with times up to now.
func (h *Handler) Status(now time.Time) (*Status, error) {
	st, err := h.Store.Tracker.Active()
	if err != nil {
		return nil, err
	}
	if st == nil {
		return &Status{}, nil
	}
	t, err := h.Store.Tasks.Get(st.TaskName)
	if err != nil {
		return nil, err
	}
	el, err := h.Store.Entries.ListByTask(st.TaskName)
	if err != nil {
		return nil, err
	}
	s := &Status{State: st, Task: t, Elapsed: now.Sub(st.Start)}
	for _, e := range el {
		s.Total += e.Duration(now)
	}
	return s, nil
}

// StartOpts are optional task attributes applied on Start. Empty fields leave
// the task's current values untouched.
type StartOpts struct {
	Project string
	Repo    string
}

// Start begins tracking name at at, auto-creating the task when it doesn't
// exist (created=true). Non-empty opts fields are set on the task, whether
// new or existing. Any previously running task is paused (its state is
// returned as prev). Starting the task that's already running fails with
// errs.ErrAlreadyRunning. Starting a done task reopens it.
func (h *Handler) Start(name string, opts StartOpts, at time.Time) (t *task.Task, created bool, prev *tracker.State, err error) {
	if err := task.ValidateName(name); err != nil {
		return nil, false, nil, err
	}
	active, err := h.Store.Tracker.Active()
	if err != nil {
		return nil, false, nil, err
	}
	if active != nil && active.TaskName == name {
		return nil, false, nil, errs.ErrAlreadyRunning
	}
	t, err = h.Store.Tasks.Get(name)
	if err != nil {
		return nil, false, nil, err
	}
	if t == nil {
		t = &task.Task{Name: name}
		created = true
	}
	if opts.Project != "" {
		t.Project = opts.Project
	}
	if opts.Repo != "" {
		repo, err := gitlog.ResolveRepo(opts.Repo)
		if err != nil {
			return nil, false, nil, err
		}
		t.Repo = repo
	}
	t.Status = task.StatusActive
	t.CompletedAt = time.Time{} // reopening a done task un-completes it
	prev, err = h.Store.Tracker.Start(t, at)
	if err != nil {
		return nil, false, nil, err
	}
	return t, created, prev, nil
}

// Pause closes the running entry and leaves its task resumable.
func (h *Handler) Pause(at time.Time) (*tracker.State, error) {
	return h.Store.Tracker.Close(at, task.StatusPaused)
}

// Stop closes the running entry and marks its task done.
func (h *Handler) Stop(at time.Time) (*tracker.State, error) {
	return h.Store.Tracker.Close(at, task.StatusDone)
}

// ImportCommits imports the user's commits made in the task's linked repo
// during the closed session st..end as notes (CreatedAt = commit time).
// Returns the count and the repo, or (0, "", nil) when the task has no repo.
// Commits already noted on the task are skipped - git's --since/--until are
// second-granular, so adjacent session windows can re-match boundary commits.
func (h *Handler) ImportCommits(st *tracker.State, end time.Time) (int, string, error) {
	t, err := h.Store.Tasks.Get(st.TaskName)
	if err != nil {
		return 0, "", err
	}
	if t == nil || t.Repo == "" {
		return 0, "", nil
	}
	commits, err := gitlog.Commits(t.Repo, st.Start, end)
	if err != nil {
		return 0, t.Repo, err
	}

	existing, err := h.Store.Notes.ListByTask(t.Name)
	if err != nil {
		return 0, t.Repo, err
	}
	seen := make(map[string]bool, len(existing))
	for _, n := range existing {
		seen[n.Text] = true
	}

	imported := 0
	for _, c := range commits {
		if seen[c.Text] {
			continue
		}
		n := &note.Note{TaskName: t.Name, CreatedAt: c.Time, Text: c.Text}
		if err := h.Store.Notes.Add(n); err != nil {
			return imported, t.Repo, err
		}
		imported++
	}
	return imported, t.Repo, nil
}
