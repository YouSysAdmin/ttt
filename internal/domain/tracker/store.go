// Package tracker owns the active-tracking pointer and the composite
// start/close transitions. Each transition spans the tasks, entries, and
// tracker buckets inside a single bbolt Update transaction so the store can
// never be left half-switched.
package tracker

import (
	"encoding/json"
	"time"

	"go.etcd.io/bbolt"

	"ttt/internal/core/errs"
	"ttt/internal/domain/entries"
	"ttt/internal/domain/store"
	"ttt/internal/models/entry"
	"ttt/internal/models/task"
	"ttt/internal/models/tracker"
)

// activeKey is the fixed key of the pointer singleton. Absent when idle.
var activeKey = []byte("active")

// Store implements store.TrackerStore on bbolt.
type Store struct {
	store.Context
}

// compile-time check.
var _ store.TrackerStore = (*Store)(nil)

func (s *Store) bucket() []byte        { return []byte(s.GetTrackerBucketName()) }
func (s *Store) tasksBucket() []byte   { return []byte(s.GetTasksBucketName()) }
func (s *Store) entriesBucket() []byte { return []byte(s.GetEntriesBucketName()) }

// Active returns the running state, or (nil, nil) when not tracking.
func (s *Store) Active() (*tracker.State, error) {
	var st *tracker.State
	err := s.Runtime.DB.View(func(tx *bbolt.Tx) error {
		var err error
		st, err = readActive(tx.Bucket(s.bucket()))
		return err
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// Start atomically closes any running entry (End=at, its task paused),
// upserts t, opens a new entry at at, and points the tracker at it. Returns
// the previous state, or nil if nothing was running.
func (s *Store) Start(t *task.Task, at time.Time) (*tracker.State, error) {
	var prev *tracker.State
	err := s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		trk := tx.Bucket(s.bucket())
		var err error
		if prev, err = readActive(trk); err != nil {
			return err
		}
		if prev != nil {
			if err := s.closeLocked(tx, prev, at, task.StatusPaused); err != nil {
				return err
			}
		}

		if err := s.upsertTaskLocked(tx, t); err != nil {
			return err
		}
		e := &entry.Entry{TaskName: t.Name, Start: at}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := tx.Bucket(s.entriesBucket()).Put(entries.Key(e.TaskName, e.Start), raw); err != nil {
			return err
		}

		state, err := json.Marshal(&tracker.State{TaskName: t.Name, Start: at})
		if err != nil {
			return err
		}
		return trk.Put(activeKey, state)
	})
	if err != nil {
		return nil, err
	}
	return prev, nil
}

// Close atomically sets End=at on the running entry, moves its task to status,
// and clears the pointer. Returns the closed state, or errs.ErrNothingRunning.
func (s *Store) Close(at time.Time, status task.Status) (*tracker.State, error) {
	var st *tracker.State
	err := s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		trk := tx.Bucket(s.bucket())
		var err error
		if st, err = readActive(trk); err != nil {
			return err
		}
		if st == nil {
			return errs.ErrNothingRunning
		}
		if err := s.closeLocked(tx, st, at, status); err != nil {
			return err
		}
		return trk.Delete(activeKey)
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// closeLocked ends the entry addressed by st and moves its task to status,
// inside the caller's transaction. A missing entry or task record (e.g. after
// a partial import) is skipped rather than treated as an error, so a stale
// pointer never wedges the tracker.
func (s *Store) closeLocked(tx *bbolt.Tx, st *tracker.State, at time.Time, status task.Status) error {
	eb := tx.Bucket(s.entriesBucket())
	key := entries.Key(st.TaskName, st.Start)
	if raw := eb.Get(key); raw != nil {
		e := &entry.Entry{}
		if err := json.Unmarshal(raw, e); err != nil {
			return err
		}
		e.End = at
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := eb.Put(key, raw); err != nil {
			return err
		}
	}

	tb := tx.Bucket(s.tasksBucket())
	if raw := tb.Get([]byte(st.TaskName)); raw != nil {
		t := &task.Task{}
		if err := json.Unmarshal(raw, t); err != nil {
			return err
		}
		t.Status = status
		if status == task.StatusDone {
			t.CompletedAt = at
		}
		t.UpdatedAt = time.Now()
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		return tb.Put([]byte(t.Name), raw)
	}
	return nil
}

// upsertTaskLocked mirrors tasks.Store.Upsert (CreatedAt on first write,
// UpdatedAt always) inside the caller's transaction.
func (s *Store) upsertTaskLocked(tx *bbolt.Tx, t *task.Task) error {
	b := tx.Bucket(s.tasksBucket())
	now := time.Now()
	if b.Get([]byte(t.Name)) == nil {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return b.Put([]byte(t.Name), raw)
}

// readActive decodes the pointer singleton, or returns nil when idle.
func readActive(b *bbolt.Bucket) (*tracker.State, error) {
	raw := b.Get(activeKey)
	if raw == nil {
		return nil, nil
	}
	st := &tracker.State{}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, err
	}
	return st, nil
}
