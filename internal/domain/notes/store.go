// Package notes persists task notes: timestamped free-form text (PR links,
// imported git commits, ...), keyed so a per-task prefix scan yields
// chronological order.
package notes

import (
	"bytes"
	"encoding/json"
	"time"

	"go.etcd.io/bbolt"

	"ttt/internal/domain/store"
	"ttt/internal/models/note"
)

// keyTimeFormat is RFC3339 with fixed-width nanoseconds, same scheme as entry
// keys: UTC keeps the suffix "Z" so lexical order equals chronological order.
const keyTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// Key returns the bucket key for a note: "<task name>/<UTC created-at>".
func Key(taskName string, at time.Time) []byte {
	return []byte(taskName + "/" + at.UTC().Format(keyTimeFormat))
}

// Store persists notes in one bbolt bucket. It implements store.NotesStore.
type Store struct {
	store.Context
}

// compile-time check.
var _ store.NotesStore = (*Store)(nil)

func (s *Store) bucket() []byte { return []byte(s.GetNotesBucketName()) }

// ListByTask returns the task's notes in chronological order.
func (s *Store) ListByTask(name string) ([]*note.Note, error) {
	prefix := []byte(name + "/")
	var out []*note.Note
	err := s.Runtime.DB.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(s.bucket()).Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			n := &note.Note{}
			if err := json.Unmarshal(v, n); err != nil {
				return err
			}
			out = append(out, n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// All returns every note grouped by task name, chronological within each
// task (keys share one lexically ordered bucket and each "<name>/" prefix is
// a contiguous range). The remote server uses it to build complete state
// snapshots in one bucket scan.
func (s *Store) All() (map[string][]*note.Note, error) {
	out := map[string][]*note.Note{}
	err := s.Runtime.DB.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(s.bucket()).ForEach(func(k, v []byte) error {
			n := &note.Note{}
			if err := json.Unmarshal(v, n); err != nil {
				return err
			}
			out[n.TaskName] = append(out[n.TaskName], n)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Add persists n, bumping its CreatedAt by 1ns until the key is unique so
// same-second notes (e.g. a batch of imported commits) never overwrite each
// other.
func (s *Store) Add(n *note.Note) error {
	return s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket())
		for b.Get(Key(n.TaskName, n.CreatedAt)) != nil {
			n.CreatedAt = n.CreatedAt.Add(time.Nanosecond)
		}
		raw, err := json.Marshal(n)
		if err != nil {
			return err
		}
		return b.Put(Key(n.TaskName, n.CreatedAt), raw)
	})
}

// Update overwrites the note at its key, replacing the stored record.
func (s *Store) Update(n *note.Note) error {
	return s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		raw, err := json.Marshal(n)
		if err != nil {
			return err
		}
		return tx.Bucket(s.bucket()).Put(Key(n.TaskName, n.CreatedAt), raw)
	})
}

// Delete removes the note keyed by n's TaskName and CreatedAt.
func (s *Store) Delete(n *note.Note) error {
	return s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(s.bucket()).Delete(Key(n.TaskName, n.CreatedAt))
	})
}
