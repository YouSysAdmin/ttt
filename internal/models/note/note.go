// Package note holds the note model: timestamped free-form text attached to a
// task - PR links, imported git commits, or any other context worth keeping.
package note

import "time"

// Note is one text attachment. CreatedAt is part of the store key, and for
// imported git commits it is the commit time.
type Note struct {
	TaskName  string    `json:"task_name"`
	CreatedAt time.Time `json:"created_at"`
	Text      string    `json:"text"`
}
