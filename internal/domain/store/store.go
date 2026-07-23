package store

import (
	"time"

	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	"ttt/internal/models/tracker"
)

// Store is the aggregate persistence handle passed to handlers alongside the
// Runtime (Handler{Runtime, Store}), one interface per persistence domain.
// The backend fills the slots (see boltkv.BindProvider). It lives outside
// env.Runtime on purpose: per-domain store packages embed Context (which
// references *env.Runtime), so env must not import this package.
type Store struct {
	Tasks   TasksStore
	Entries EntriesStore
	Tracker TrackerStore
	Notes   NotesStore
}

// TasksStore persists tasks keyed by their unique name.
type TasksStore interface {
	// Get returns the task by name, or (nil, nil) when none exists - absence
	// is not an error so callers branch on the nil.
	Get(name string) (*task.Task, error)
	List() ([]*task.Task, error)
	// Upsert creates or replaces the task keyed by its name, stamping
	// CreatedAt on first write and UpdatedAt on every write.
	Upsert(t *task.Task) error
	// Delete atomically removes the task record, its entries and notes, and
	// clears the tracker pointer when it targets the task. Deleting an
	// absent task is not an error.
	Delete(name string) error
	// Rename atomically moves the task record, its entry and note keys, and
	// the tracker pointer (when it targets oldName) to newName. Fails with
	// errs.ErrTaskNotFound / errs.ErrTaskExists.
	Rename(oldName, newName string) error
}

// EntriesStore persists tracked time entries, chronologically iterable per
// task.
type EntriesStore interface {
	ListByTask(name string) ([]*entry.Entry, error)
	Put(e *entry.Entry) error
}

// NotesStore persists task notes, chronologically iterable per task.
type NotesStore interface {
	ListByTask(name string) ([]*note.Note, error)
	// Add persists n, bumping its CreatedAt by 1ns until the key is unique
	// (imported commits can share a second).
	Add(n *note.Note) error
	// Update overwrites the note at its key (TaskName + CreatedAt) - unlike
	// Add, an existing record is replaced, not key-bumped.
	Update(n *note.Note) error
	// Delete removes the note keyed by n's TaskName and CreatedAt. Deleting
	// an absent note is not an error.
	Delete(n *note.Note) error
}

// TrackerStore owns the active-tracking pointer and the composite transitions
// that must atomically span tasks, entries, and the pointer.
type TrackerStore interface {
	// Active returns the running state, or (nil, nil) when not tracking.
	Active() (*tracker.State, error)
	// Start atomically: if an entry is running, sets its End=at and its
	// task's status to paused, upserts t (the caller has already set its
	// status), opens a new entry {t.Name, at}, and points the tracker at it.
	// Returns the previous state, or nil if nothing was running.
	Start(t *task.Task, at time.Time) (*tracker.State, error)
	// Close atomically sets End=at on the running entry, sets its task's
	// status to status, and clears the pointer. Returns the closed state, or
	// errs.ErrNothingRunning when idle.
	Close(at time.Time, status task.Status) (*tracker.State, error)
}
