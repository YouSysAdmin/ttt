package tracker_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ttt/internal/core/env"
	"ttt/internal/core/errs"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/store"
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

func TestStartOpensEntryAndPointer(t *testing.T) {
	st := newTestStore(t)
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
}

func TestStartPreemptsRunningTask(t *testing.T) {
	st := newTestStore(t)
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
}

func TestCloseSetsStatusAndClearsPointer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status task.Status
	}{
		{"pause", task.StatusPaused},
		{"stop", task.StatusDone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newTestStore(t)
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
}

func TestCloseWhenIdle(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Tracker.Close(time.Now(), task.StatusDone); !errors.Is(err, errs.ErrNothingRunning) {
		t.Fatalf("expected ErrNothingRunning, got %v", err)
	}
}

func TestResumeAccumulatesEntries(t *testing.T) {
	st := newTestStore(t)
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
}
