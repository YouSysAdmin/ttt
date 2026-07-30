// Package storetest holds the backend-agnostic conformance suite for the
// store interfaces. The boltkv-backed domain tests and the remote
// client/server round-trip tests run the same suite, so any backend must
// honor the same contracts: atomic composite transitions, (nil, nil)
// absence, sentinel error identity, and in-place mutation of passed records.
package storetest

import (
	"errors"
	"testing"
	"time"

	"ttt/internal/core/errs"
	"ttt/internal/domain/store"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
)

// Factory returns a fresh, empty store for one test.
type Factory func(t *testing.T) *store.Store

// All runs every conformance test.
func All(t *testing.T, newStore Factory) {
	Tasks(t, newStore)
	Tracker(t, newStore)
	Notes(t, newStore)
}

// Tasks covers TasksStore: the composite Rename/Delete transactions and
// their sentinel errors.
func Tasks(t *testing.T, newStore Factory) {
	t.Run("RenameMigratesEverything", func(t *testing.T) {
		st := newStore(t)
		t0 := time.Now()

		// A task with a closed entry, a running entry (task is active), and a note.
		if _, err := st.Tracker.Start(&task.Task{Name: "old", Status: task.StatusActive}, t0); err != nil {
			t.Fatalf("start: %v", err)
		}
		if _, err := st.Tracker.Close(t0.Add(time.Second), task.StatusPaused); err != nil {
			t.Fatalf("pause: %v", err)
		}
		if _, err := st.Tracker.Start(&task.Task{Name: "old", Status: task.StatusActive}, t0.Add(2*time.Second)); err != nil {
			t.Fatalf("restart: %v", err)
		}
		if err := st.Notes.Add(&note.Note{TaskName: "old", CreatedAt: t0, Text: "a note"}); err != nil {
			t.Fatalf("note: %v", err)
		}

		if err := st.Tasks.Rename("old", "new"); err != nil {
			t.Fatalf("rename: %v", err)
		}

		// Task record moved.
		if got, _ := st.Tasks.Get("old"); got != nil {
			t.Fatalf("old task still present: %+v", got)
		}
		renamed, _ := st.Tasks.Get("new")
		if renamed == nil || renamed.Name != "new" || renamed.Status != task.StatusActive {
			t.Fatalf("renamed task wrong: %+v", renamed)
		}

		// Entries moved, chronological, TaskName rewritten, running entry intact.
		el, _ := st.Entries.ListByTask("new")
		if len(el) != 2 {
			t.Fatalf("expected 2 entries under new name, got %d", len(el))
		}
		for _, e := range el {
			if e.TaskName != "new" {
				t.Fatalf("entry task_name not rewritten: %+v", e)
			}
		}
		if el[0].Running() || !el[1].Running() {
			t.Fatalf("entry states lost in rename: %+v, %+v", el[0], el[1])
		}
		if old, _ := st.Entries.ListByTask("old"); len(old) != 0 {
			t.Fatalf("old entries still present: %+v", old)
		}

		// Notes moved.
		nl, _ := st.Notes.ListByTask("new")
		if len(nl) != 1 || nl[0].TaskName != "new" || nl[0].Text != "a note" {
			t.Fatalf("notes not migrated: %+v", nl)
		}

		// Tracker pointer follows the rename.
		active, _ := st.Tracker.Active()
		if active == nil || active.TaskName != "new" {
			t.Fatalf("tracker pointer not updated: %+v", active)
		}
		// And closing still works against the migrated entry key.
		if _, err := st.Tracker.Close(t0.Add(3*time.Second), task.StatusDone); err != nil {
			t.Fatalf("close after rename: %v", err)
		}
		el, _ = st.Entries.ListByTask("new")
		if el[1].Running() {
			t.Fatalf("running entry not closed after rename: %+v", el[1])
		}
	})

	t.Run("DeleteRemovesEverything", func(t *testing.T) {
		st := newStore(t)
		t0 := time.Now()

		// A running task (entry + tracker pointer) with a note.
		if _, err := st.Tracker.Start(&task.Task{Name: "doomed", Status: task.StatusActive}, t0); err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := st.Notes.Add(&note.Note{TaskName: "doomed", CreatedAt: t0, Text: "n"}); err != nil {
			t.Fatalf("note: %v", err)
		}

		if err := st.Tasks.Delete("doomed"); err != nil {
			t.Fatalf("delete: %v", err)
		}

		if got, _ := st.Tasks.Get("doomed"); got != nil {
			t.Fatalf("task still present: %+v", got)
		}
		if el, _ := st.Entries.ListByTask("doomed"); len(el) != 0 {
			t.Fatalf("entries still present: %+v", el)
		}
		if nl, _ := st.Notes.ListByTask("doomed"); len(nl) != 0 {
			t.Fatalf("notes still present: %+v", nl)
		}
		if active, _ := st.Tracker.Active(); active != nil {
			t.Fatalf("tracker pointer still present: %+v", active)
		}
	})

	t.Run("RenameErrors", func(t *testing.T) {
		st := newStore(t)
		for _, name := range []string{"a", "b"} {
			if err := st.Tasks.Upsert(&task.Task{Name: name, Status: task.StatusTodo}); err != nil {
				t.Fatalf("upsert %s: %v", name, err)
			}
		}
		if err := st.Tasks.Rename("missing", "x"); !errors.Is(err, errs.ErrTaskNotFound) {
			t.Fatalf("expected ErrTaskNotFound, got %v", err)
		}
		if err := st.Tasks.Rename("a", "b"); !errors.Is(err, errs.ErrTaskExists) {
			t.Fatalf("expected ErrTaskExists, got %v", err)
		}
	})

	t.Run("UpsertStampsCallerRecord", func(t *testing.T) {
		st := newStore(t)
		rec := &task.Task{Name: "stamped", Status: task.StatusTodo}
		if err := st.Tasks.Upsert(rec); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		// Upsert's contract: the caller's record reflects the stamps.
		if rec.CreatedAt.IsZero() || rec.UpdatedAt.IsZero() {
			t.Fatalf("caller record not stamped: %+v", rec)
		}
	})
}

// Tracker covers TrackerStore: the composite Start/Close transactions,
// preemption, and the idle sentinel.
func Tracker(t *testing.T, newStore Factory) {
	t.Run("StartOpensEntryAndPointer", func(t *testing.T) {
		st := newStore(t)
		at := time.Now()

		prev, err := st.Tracker.Start(&task.Task{Name: "foo", Status: task.StatusActive}, at)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		if prev != nil {
			t.Fatalf("expected no previous state, got %+v", prev)
		}

		active, err := st.Tracker.Active()
		if err != nil {
			t.Fatalf("active: %v", err)
		}
		if active == nil || active.TaskName != "foo" || !active.Start.Equal(at) {
			t.Fatalf("unexpected active state: %+v", active)
		}

		el, err := st.Entries.ListByTask("foo")
		if err != nil {
			t.Fatalf("list entries: %v", err)
		}
		if len(el) != 1 || !el[0].Running() {
			t.Fatalf("expected one running entry, got %+v", el)
		}

		got, err := st.Tasks.Get("foo")
		if err != nil || got == nil {
			t.Fatalf("get task: %v, %+v", err, got)
		}
		if got.Status != task.StatusActive || got.CreatedAt.IsZero() {
			t.Fatalf("task not upserted properly: %+v", got)
		}
	})

	t.Run("StartPreemptsRunningTask", func(t *testing.T) {
		st := newStore(t)
		t0 := time.Now()
		t1 := t0.Add(2 * time.Second)

		if _, err := st.Tracker.Start(&task.Task{Name: "foo", Status: task.StatusActive}, t0); err != nil {
			t.Fatalf("start foo: %v", err)
		}
		prev, err := st.Tracker.Start(&task.Task{Name: "bar", Status: task.StatusActive}, t1)
		if err != nil {
			t.Fatalf("start bar: %v", err)
		}
		if prev == nil || prev.TaskName != "foo" {
			t.Fatalf("expected foo as previous state, got %+v", prev)
		}

		// foo's entry must be closed exactly at bar's start, and foo paused.
		el, err := st.Entries.ListByTask("foo")
		if err != nil || len(el) != 1 {
			t.Fatalf("list foo entries: %v, %+v", err, el)
		}
		if el[0].Running() || !el[0].End.Equal(t1) {
			t.Fatalf("foo entry not closed at t1: %+v", el[0])
		}
		foo, _ := st.Tasks.Get("foo")
		if foo.Status != task.StatusPaused {
			t.Fatalf("foo status = %s, want paused", foo.Status)
		}

		active, _ := st.Tracker.Active()
		if active == nil || active.TaskName != "bar" {
			t.Fatalf("expected bar active, got %+v", active)
		}
	})

	t.Run("CloseSetsStatusAndClearsPointer", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			status task.Status
		}{
			{"pause", task.StatusPaused},
			{"stop", task.StatusDone},
		} {
			t.Run(tc.name, func(t *testing.T) {
				st := newStore(t)
				t0 := time.Now()
				t1 := t0.Add(time.Second)

				if _, err := st.Tracker.Start(&task.Task{Name: "foo", Status: task.StatusActive}, t0); err != nil {
					t.Fatalf("start: %v", err)
				}
				closed, err := st.Tracker.Close(t1, tc.status)
				if err != nil {
					t.Fatalf("close: %v", err)
				}
				if closed == nil || closed.TaskName != "foo" || !closed.Start.Equal(t0) {
					t.Fatalf("unexpected closed state: %+v", closed)
				}

				active, _ := st.Tracker.Active()
				if active != nil {
					t.Fatalf("pointer not cleared: %+v", active)
				}
				el, _ := st.Entries.ListByTask("foo")
				if len(el) != 1 || !el[0].End.Equal(t1) {
					t.Fatalf("entry not closed at t1: %+v", el)
				}
				foo, _ := st.Tasks.Get("foo")
				if foo.Status != tc.status {
					t.Fatalf("foo status = %s, want %s", foo.Status, tc.status)
				}
				if tc.status == task.StatusDone && !foo.CompletedAt.Equal(t1) {
					t.Fatalf("CompletedAt = %v, want %v", foo.CompletedAt, t1)
				}
				if tc.status == task.StatusPaused && !foo.CompletedAt.IsZero() {
					t.Fatalf("pause must not set CompletedAt, got %v", foo.CompletedAt)
				}
			})
		}
	})

	t.Run("CloseWhenIdle", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.Tracker.Close(time.Now(), task.StatusDone); !errors.Is(err, errs.ErrNothingRunning) {
			t.Fatalf("expected ErrNothingRunning, got %v", err)
		}
	})

	t.Run("ResumeAccumulatesEntries", func(t *testing.T) {
		st := newStore(t)
		t0 := time.Now()

		if _, err := st.Tracker.Start(&task.Task{Name: "foo", Status: task.StatusActive}, t0); err != nil {
			t.Fatalf("start: %v", err)
		}
		if _, err := st.Tracker.Close(t0.Add(time.Second), task.StatusPaused); err != nil {
			t.Fatalf("pause: %v", err)
		}
		if _, err := st.Tracker.Start(&task.Task{Name: "foo", Status: task.StatusActive}, t0.Add(5*time.Second)); err != nil {
			t.Fatalf("restart: %v", err)
		}

		el, err := st.Entries.ListByTask("foo")
		if err != nil {
			t.Fatalf("list entries: %v", err)
		}
		if len(el) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(el))
		}
		if el[0].Running() || !el[1].Running() {
			t.Fatalf("expected first closed and second running: %+v, %+v", el[0], el[1])
		}
		if !el[0].Start.Before(el[1].Start) {
			t.Fatalf("entries not chronological: %+v", el)
		}
	})
}

// Notes covers NotesStore: chronological keys, deletes, and the CreatedAt
// collision bump reflected on the caller's record.
func Notes(t *testing.T, newStore Factory) {
	t.Run("AddAndListChronological", func(t *testing.T) {
		st := newStore(t)
		t0 := time.Now()

		// Insert out of order. The key scheme must sort them chronologically.
		for _, n := range []*note.Note{
			{TaskName: "foo", CreatedAt: t0.Add(2 * time.Second), Text: "second"},
			{TaskName: "foo", CreatedAt: t0, Text: "first"},
			{TaskName: "bar", CreatedAt: t0.Add(time.Second), Text: "other task"},
		} {
			if err := st.Notes.Add(n); err != nil {
				t.Fatalf("add: %v", err)
			}
		}

		nl, err := st.Notes.ListByTask("foo")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(nl) != 2 || nl[0].Text != "first" || nl[1].Text != "second" {
			t.Fatalf("unexpected notes: %+v", nl)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		st := newStore(t)
		at := time.Now()
		keep := &note.Note{TaskName: "foo", CreatedAt: at, Text: "keep"}
		drop := &note.Note{TaskName: "foo", CreatedAt: at.Add(time.Second), Text: "drop"}
		for _, n := range []*note.Note{keep, drop} {
			if err := st.Notes.Add(n); err != nil {
				t.Fatalf("add: %v", err)
			}
		}
		if err := st.Notes.Delete(drop); err != nil {
			t.Fatalf("delete: %v", err)
		}
		nl, err := st.Notes.ListByTask("foo")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(nl) != 1 || nl[0].Text != "keep" {
			t.Fatalf("unexpected notes after delete: %+v", nl)
		}
		// Deleting an absent note is not an error.
		if err := st.Notes.Delete(drop); err != nil {
			t.Fatalf("double delete: %v", err)
		}
	})

	t.Run("AddBumpsCreatedAtOnCollision", func(t *testing.T) {
		st := newStore(t)
		at := time.Now()

		// Same-second commits imported in a batch share CreatedAt. The bump
		// must land on the caller's record too (Add's in-place contract).
		batch := []*note.Note{
			{TaskName: "foo", CreatedAt: at, Text: "a"},
			{TaskName: "foo", CreatedAt: at, Text: "b"},
			{TaskName: "foo", CreatedAt: at, Text: "c"},
		}
		for _, n := range batch {
			if err := st.Notes.Add(n); err != nil {
				t.Fatalf("add %q: %v", n.Text, err)
			}
		}
		if !batch[1].CreatedAt.Equal(at.Add(time.Nanosecond)) || !batch[2].CreatedAt.Equal(at.Add(2*time.Nanosecond)) {
			t.Fatalf("bump not reflected on caller records: %v / %v", batch[1].CreatedAt, batch[2].CreatedAt)
		}

		nl, err := st.Notes.ListByTask("foo")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(nl) != 3 {
			t.Fatalf("expected 3 notes, got %d: %+v", len(nl), nl)
		}
		if nl[0].Text != "a" || nl[1].Text != "b" || nl[2].Text != "c" {
			t.Fatalf("order lost on collision: %+v", nl)
		}
		if !nl[1].CreatedAt.Equal(at.Add(time.Nanosecond)) || !nl[2].CreatedAt.Equal(at.Add(2*time.Nanosecond)) {
			t.Fatalf("expected 1ns bumps, got %v / %v", nl[1].CreatedAt, nl[2].CreatedAt)
		}
	})
}
