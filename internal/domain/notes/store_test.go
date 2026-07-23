package notes_test

import (
	"path/filepath"
	"testing"
	"time"

	"ttt/internal/core/env"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/store"
	"ttt/internal/models/note"
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

func TestAddAndListChronological(t *testing.T) {
	st := newTestStore(t)
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
}

func TestDelete(t *testing.T) {
	st := newTestStore(t)
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
}

func TestAddBumpsCreatedAtOnCollision(t *testing.T) {
	st := newTestStore(t)
	at := time.Now()

	// Same-second commits imported in a batch share CreatedAt.
	for _, text := range []string{"a", "b", "c"} {
		if err := st.Notes.Add(&note.Note{TaskName: "foo", CreatedAt: at, Text: text}); err != nil {
			t.Fatalf("add %q: %v", text, err)
		}
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
}
