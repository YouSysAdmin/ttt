package tasks_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ttt/internal/core/env"
	"ttt/internal/core/errs"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/store"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	kv, err := boltkv.Open(filepath.Join(t.TempDir(), "ttt.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = kv.Close() })

	rt := &env.Runtime{}
	st := &store.Store{}
	boltkv.BindProvider(rt, st, kv)
	return st
}

func TestRenameMigratesEverything(t *testing.T) {
	st := newTestStore(t)
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
}

func TestDeleteRemovesEverything(t *testing.T) {
	st := newTestStore(t)
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
}

func TestRenameErrors(t *testing.T) {
	st := newTestStore(t)
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
}
