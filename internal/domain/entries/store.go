// Package entries persists time entries: one record per start..end tracking
// period, keyed so a per-task prefix scan yields chronological order.
package entries

import (
	"bytes"
	"encoding/json"
	"time"

	"go.etcd.io/bbolt"

	"ttt/internal/domain/store"
	"ttt/internal/models/entry"
)

// keyTimeFormat is RFC3339 with fixed-width nanoseconds. Keys are built from
// UTC times so the suffix is always "Z" and lexical order equals chronological
// order (RFC3339Nano trims trailing zeros, which would break sorting).
const keyTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// Key returns the bucket key for an entry: "<task name>/<UTC start>". "/" is
// safe as a separator because handlers reject it in task names.
func Key(taskName string, start time.Time) []byte {
	return []byte(taskName + "/" + start.UTC().Format(keyTimeFormat))
}

// Store persists entries in one bbolt bucket. It implements
// store.EntriesStore.
type Store struct {
	store.Context
}

// compile-time check.
var _ store.EntriesStore = (*Store)(nil)

func (s *Store) bucket() []byte { return []byte(s.GetEntriesBucketName()) }

// ListByTask returns the task's entries in chronological order.
func (s *Store) ListByTask(name string) ([]*entry.Entry, error) {
	prefix := []byte(name + "/")
	var out []*entry.Entry
	err := s.Runtime.DB.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(s.bucket()).Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			e := &entry.Entry{}
			if err := json.Unmarshal(v, e); err != nil {
				return err
			}
			out = append(out, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// All returns every entry grouped by task name, chronological within each
// task (keys share one lexically ordered bucket and each "<name>/" prefix is
// a contiguous range). The remote server uses it to build complete state
// snapshots in one bucket scan.
func (s *Store) All() (map[string][]*entry.Entry, error) {
	out := map[string][]*entry.Entry{}
	err := s.Runtime.DB.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(s.bucket()).ForEach(func(k, v []byte) error {
			e := &entry.Entry{}
			if err := json.Unmarshal(v, e); err != nil {
				return err
			}
			out[e.TaskName] = append(out[e.TaskName], e)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Put creates or replaces the entry keyed by its task name and start time.
func (s *Store) Put(e *entry.Entry) error {
	return s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return tx.Bucket(s.bucket()).Put(Key(e.TaskName, e.Start), raw)
	})
}
