// Package task holds the task model: a named unit of work whose tracked time
// is the sum of its entries (see internal/models/entry).
package task

import (
	"strings"
	"time"

	"ttt/internal/core/errs"
)

// Status is the task lifecycle state, driven by the tracker commands:
// add -> todo, start -> active, pause -> paused, stop -> done.
type Status string

const (
	StatusTodo   Status = "todo"
	StatusActive Status = "active"
	StatusPaused Status = "paused"
	StatusDone   Status = "done"
)

// ValidateName rejects names that can't serve as store keys: "/" is the
// separator in entry keys ("<name>/<start>"), so it can't appear in a name.
func ValidateName(name string) error {
	if name == "" || strings.Contains(name, "/") {
		return errs.ErrInvalidName
	}
	return nil
}

// Task is keyed by its unique Name across the CLI and the store. Project is
// a free-form grouping label (an org, an app, ...) for filtering and
// per-project stats.
type Task struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Project     string `json:"project,omitempty"`
	// Repo is a linked git repository path. The user's commits made there
	// during a tracking session are imported as notes when the session ends.
	Repo      string    `json:"repo,omitempty"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// CompletedAt is set when stop marks the task done and cleared when the
	// task is reopened by start.
	CompletedAt time.Time `json:"completed_at,omitzero"`
}
