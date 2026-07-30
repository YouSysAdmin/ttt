// Package api holds the wire types shared by the remote server and client:
// one request/response pair per store method, carrying the model structs
// with their storage json tags, plus the error envelope that keeps sentinel
// errors matchable with errors.Is across the wire.
package api

import (
	"time"

	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	"ttt/internal/models/tracker"
)

// Empty is the body for requests and responses that carry no data.
type Empty struct{}

// PingResp reports the server is up and which version it runs.
type PingResp struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

// NameReq addresses a task by name (tasks/get, tasks/delete, entries/list, notes/list).
type NameReq struct {
	Name string `json:"name"`
}

// TaskReq carries a task record (tasks/upsert).
type TaskReq struct {
	Task *task.Task `json:"task"`
}

// TaskResp carries a task record. A null task round-trips the (nil, nil) absence contract of TasksStore.Get.
type TaskResp struct {
	Task *task.Task `json:"task"`
}

// TasksResp is the tasks/list response.
type TasksResp struct {
	Tasks []*task.Task `json:"tasks"`
}

// RenameReq is the tasks/rename request.
type RenameReq struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

// EntryReq carries an entry record (entries/put).
type EntryReq struct {
	Entry *entry.Entry `json:"entry"`
}

// EntriesResp is the entries/list response.
type EntriesResp struct {
	Entries []*entry.Entry `json:"entries"`
}

// NoteReq carries a note record (notes/add, notes/update, notes/delete).
type NoteReq struct {
	Note *note.Note `json:"note"`
}

// NoteResp returns the stored note. notes/add may have bumped CreatedAt to
// keep the key unique, so the client copies it back into the caller's note.
type NoteResp struct {
	Note *note.Note `json:"note"`
}

// NotesResp is the notes/list response.
type NotesResp struct {
	Notes []*note.Note `json:"notes"`
}

// StateResp carries a tracker state. A null state round-trips the (nil, nil)
// idle contract of TrackerStore.Active.
type StateResp struct {
	State *tracker.State `json:"state"`
}

// StartReq is the tracker/start request: the task to track (the caller has
// already set its status) and the transition time.
type StartReq struct {
	Task *task.Task `json:"task"`
	At   time.Time  `json:"at"`
}

// StartResp returns the upserted task (server-stamped timestamps, copied
// back by the client) and the preempted state, if any.
type StartResp struct {
	Task     *task.Task     `json:"task"`
	Previous *tracker.State `json:"previous"`
}

// CloseReq is the tracker/close request: the transition time and the status
// the running entry's task moves to (paused or done).
type CloseReq struct {
	At     time.Time   `json:"at"`
	Status task.Status `json:"status"`
}

// StateSnapshotResp is the /v1/state response: the whole store in one round
// trip, feeding the client-side read cache. Entries and Notes are keyed by
// task name and keep their per-task chronological order. Tasks keep the
// TasksStore.List order.
type StateSnapshotResp struct {
	Tasks   []*task.Task              `json:"tasks"`
	Entries map[string][]*entry.Entry `json:"entries"`
	Notes   map[string][]*note.Note   `json:"notes"`
	State   *tracker.State            `json:"state"`
}
