package tasks

import (
	"cmp"
	"errors"
	"slices"
	"time"

	"ttt/internal/core/env"
	"ttt/internal/core/errs"
	"ttt/internal/core/gitlog"
	"ttt/internal/domain/store"
	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
)

type Handler struct {
	Runtime *env.Runtime
	Store   *store.Store
}

// Add creates a task with status todo. Fails with errs.ErrTaskExists when the
// name is already taken.
func (h *Handler) Add(name, description, project, repo string) (*task.Task, error) {
	if err := task.ValidateName(name); err != nil {
		return nil, err
	}
	existing, err := h.Store.Tasks.Get(name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errs.ErrTaskExists
	}
	if repo != "" {
		if repo, err = gitlog.ResolveRepo(repo); err != nil {
			return nil, err
		}
	}
	t := &task.Task{
		Name:        name,
		Description: description,
		Project:     project,
		Repo:        repo,
		Status:      task.StatusTodo,
	}
	if err := h.Store.Tasks.Upsert(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Delete removes the task and everything keyed by it (entries, notes, the
// tracker pointer). Fails with errs.ErrTaskNotFound.
func (h *Handler) Delete(name string) error {
	t, err := h.Store.Tasks.Get(name)
	if err != nil {
		return err
	}
	if t == nil {
		return errs.ErrTaskNotFound
	}
	return h.Store.Tasks.Delete(name)
}

// Changes are the edits to apply to a task: nil leaves a field untouched, a
// non-nil pointer sets it (an empty string clears the field).
type Changes struct {
	Name        *string
	Description *string
	Project     *string
	Repo        *string
}

// Edit applies ch to the task. Field updates are written first, then a
// rename (if requested) migrates the store keys via Rename. Fails with
// errs.ErrTaskNotFound, and for renames errs.ErrInvalidName /
// errs.ErrTaskExists.
func (h *Handler) Edit(name string, ch Changes) (*task.Task, error) {
	if ch.Name == nil && ch.Description == nil && ch.Project == nil && ch.Repo == nil {
		return nil, errors.New("nothing to edit")
	}
	t, err := h.Store.Tasks.Get(name)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errs.ErrTaskNotFound
	}

	fieldsChanged := false
	if ch.Description != nil {
		t.Description = *ch.Description
		fieldsChanged = true
	}
	if ch.Project != nil {
		t.Project = *ch.Project
		fieldsChanged = true
	}
	if ch.Repo != nil {
		repo := *ch.Repo
		if repo != "" {
			if repo, err = gitlog.ResolveRepo(repo); err != nil {
				return nil, err
			}
		}
		t.Repo = repo
		fieldsChanged = true
	}
	if fieldsChanged {
		if err := h.Store.Tasks.Upsert(t); err != nil {
			return nil, err
		}
	}

	if ch.Name != nil && *ch.Name != name {
		newName := *ch.Name
		if err := task.ValidateName(newName); err != nil {
			return nil, err
		}
		if err := h.Store.Tasks.Rename(name, newName); err != nil {
			return nil, err
		}
		t.Name = newName
	}
	return t, nil
}

// Finish task by name. Fails with errs.ErrTaskNotFound.
func (h *Handler) Finish(name string, at time.Time) (*task.Task, error) {
	t, err := h.Store.Tasks.Get(name)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errs.ErrTaskNotFound
	}
	st, err := h.Store.Tracker.Active()
	if err != nil {
		return nil, err
	}
	if st != nil && st.TaskName == name {
		return nil, errs.ErrAlreadyRunning
	}
	t.Status = task.StatusDone
	t.CompletedAt = at
	if err := h.Store.Tasks.Upsert(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Row is one task with its tracked total for listing.
type Row struct {
	Task    *task.Task
	Total   time.Duration
	Running bool
}

// List returns every task with its total tracked time. A running entry is
// counted live up to now. A non-empty project keeps only tasks in it.
func (h *Handler) List(now time.Time, project string) ([]*Row, error) {
	tl, err := h.Store.Tasks.List()
	if err != nil {
		return nil, err
	}
	rows := make([]*Row, 0, len(tl))
	for _, t := range tl {
		if project != "" && t.Project != project {
			continue
		}
		el, err := h.Store.Entries.ListByTask(t.Name)
		if err != nil {
			return nil, err
		}
		row := &Row{Task: t}
		for _, e := range el {
			row.Total += e.Duration(now)
			row.Running = row.Running || e.Running()
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// StatRow is one task's tracked time within a stats window.
type StatRow struct {
	Task  *task.Task
	Total time.Duration
}

// Stats sums tracked time in [from, to) per task, clipping entries to the
// window so sessions straddling a boundary only count their overlap (a
// running entry counts up to to). Tasks with no time in the window are
// omitted. A non-empty project keeps only its tasks. Rows are sorted by
// time, largest first.
func (h *Handler) Stats(from, to time.Time, project string) ([]*StatRow, error) {
	tl, err := h.Store.Tasks.List()
	if err != nil {
		return nil, err
	}
	var rows []*StatRow
	for _, t := range tl {
		if project != "" && t.Project != project {
			continue
		}
		el, err := h.Store.Entries.ListByTask(t.Name)
		if err != nil {
			return nil, err
		}
		var total time.Duration
		for _, e := range el {
			start, end := e.Start, e.End
			if e.Running() {
				end = to
			}
			if start.Before(from) {
				start = from
			}
			if end.After(to) {
				end = to
			}
			if end.After(start) {
				total += end.Sub(start)
			}
		}
		if total > 0 {
			rows = append(rows, &StatRow{Task: t, Total: total})
		}
	}
	slices.SortStableFunc(rows, func(a, b *StatRow) int {
		return cmp.Compare(b.Total, a.Total)
	})
	return rows, nil
}

// Details is the full picture of one task, for the show command.
type Details struct {
	Task    *task.Task
	Total   time.Duration
	Entries []*entry.Entry
	Notes   []*note.Note
}

// Show returns the task with its entries, notes, and total tracked time (a
// running entry counted live up to now). Fails with errs.ErrTaskNotFound.
func (h *Handler) Show(name string, now time.Time) (*Details, error) {
	t, err := h.Store.Tasks.Get(name)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errs.ErrTaskNotFound
	}
	el, err := h.Store.Entries.ListByTask(name)
	if err != nil {
		return nil, err
	}
	nl, err := h.Store.Notes.ListByTask(name)
	if err != nil {
		return nil, err
	}
	d := &Details{Task: t, Entries: el, Notes: nl}
	for _, e := range el {
		d.Total += e.Duration(now)
	}
	return d, nil
}
