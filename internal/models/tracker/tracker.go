// Package tracker holds the active-tracking model: a singleton pointer to the
// currently running entry, so status/stop/pause know what is being tracked.
package tracker

import "time"

// State identifies the running entry: the task name plus the entry's Start,
// which together address the entry record in the store.
type State struct {
	TaskName string    `json:"task_name"`
	Start    time.Time `json:"start"`
}
