// Package entry holds the time-entry model: one contiguous tracked period for
// a task. A task's total time is the sum of its entries' durations.
package entry

import "time"

// Entry is one start..end tracking period. A zero End means the entry is
// still running.
type Entry struct {
	TaskName string    `json:"task_name"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end,omitzero"`
}

// Running reports whether the entry is still open (no End yet).
func (e *Entry) Running() bool { return e.End.IsZero() }

// Duration returns End-Start, substituting now for a running entry.
func (e *Entry) Duration(now time.Time) time.Duration {
	if e.Running() {
		return now.Sub(e.Start)
	}
	return e.End.Sub(e.Start)
}
