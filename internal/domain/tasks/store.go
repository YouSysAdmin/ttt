package tasks

import (
	"bytes"
	"encoding/json"
	"time"

	"go.etcd.io/bbolt"

	"ttt/internal/core/errs"
	"ttt/internal/domain/store"
	"ttt/internal/models/task"
	"ttt/internal/models/tracker"
)

// Store persists tasks in one bbolt bucket, keyed by name. It implements
// store.TasksStore.
type Store struct {
	store.Context
}

// compile-time check.
var _ store.TasksStore = (*Store)(nil)

func (s *Store) bucket() []byte { return []byte(s.GetTasksBucketName()) }

// Get returns the task for name, or (nil, nil) when none exists - absence is
// not an error so callers branch on the nil.
func (s *Store) Get(name string) (*task.Task, error) {
	var t *task.Task
	err := s.Runtime.DB.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(s.bucket()).Get([]byte(name))
		if raw == nil {
			return nil
		}
		t = &task.Task{}
		return json.Unmarshal(raw, t)
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// List returns every task, in key (name) order.
func (s *Store) List() ([]*task.Task, error) {
	var out []*task.Task
	err := s.Runtime.DB.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(s.bucket()).ForEach(func(_, v []byte) error {
			t := &task.Task{}
			if err := json.Unmarshal(v, t); err != nil {
				return err
			}
			out = append(out, t)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Upsert creates or replaces the task keyed by its name, stamping CreatedAt on
// first write and UpdatedAt on every write.
func (s *Store) Upsert(t *task.Task) error {
	return s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(s.bucket())
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
	})
}

// Delete atomically removes the task record, its entries and notes, and
// clears the tracker pointer when it targets the task, so nothing keyed by
// the name can dangle. Deleting an absent task is not an error.
func (s *Store) Delete(name string) error {
	return s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		if err := tx.Bucket(s.bucket()).Delete([]byte(name)); err != nil {
			return err
		}
		prefix := []byte(name + "/")
		for _, bucketName := range []string{s.GetEntriesBucketName(), s.GetNotesBucketName()} {
			b := tx.Bucket([]byte(bucketName))
			var keys [][]byte
			c := b.Cursor()
			for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
				keys = append(keys, append([]byte(nil), k...))
			}
			for _, k := range keys {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
		}
		trk := tx.Bucket([]byte(s.GetTrackerBucketName()))
		if raw := trk.Get([]byte("active")); raw != nil {
			st := &tracker.State{}
			if err := json.Unmarshal(raw, st); err != nil {
				return err
			}
			if st.TaskName == name {
				return trk.Delete([]byte("active"))
			}
		}
		return nil
	})
}

// Rename atomically moves the task record, its entry and note keys (the name
// is their key prefix), and the tracker pointer to newName, in one
// transaction so a crash can't leave the buckets disagreeing on the name.
func (s *Store) Rename(oldName, newName string) error {
	return s.Runtime.DB.Update(func(tx *bbolt.Tx) error {
		tb := tx.Bucket(s.bucket())
		raw := tb.Get([]byte(oldName))
		if raw == nil {
			return errs.ErrTaskNotFound
		}
		if tb.Get([]byte(newName)) != nil {
			return errs.ErrTaskExists
		}

		t := &task.Task{}
		if err := json.Unmarshal(raw, t); err != nil {
			return err
		}
		t.Name = newName
		t.UpdatedAt = time.Now()
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		if err := tb.Put([]byte(newName), raw); err != nil {
			return err
		}
		if err := tb.Delete([]byte(oldName)); err != nil {
			return err
		}

		// Entries and notes both use "<name>/<timestamp>" keys and carry a
		// task_name field, so rewrite the prefix and the field for each.
		for _, bucketName := range []string{s.GetEntriesBucketName(), s.GetNotesBucketName()} {
			if err := renamePrefixed(tx.Bucket([]byte(bucketName)), oldName, newName); err != nil {
				return err
			}
		}

		trk := tx.Bucket([]byte(s.GetTrackerBucketName()))
		if raw := trk.Get([]byte("active")); raw != nil {
			st := &tracker.State{}
			if err := json.Unmarshal(raw, st); err != nil {
				return err
			}
			if st.TaskName == oldName {
				st.TaskName = newName
				raw, err := json.Marshal(st)
				if err != nil {
					return err
				}
				if err := trk.Put([]byte("active"), raw); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// renamePrefixed moves every "<oldName>/<suffix>" key to "<newName>/<suffix>"
// and rewrites the record's task_name field. Pairs are collected before
// writing - putting keys while a cursor iterates would disturb it.
func renamePrefixed(b *bbolt.Bucket, oldName, newName string) error {
	prefix := []byte(oldName + "/")
	type kv struct{ k, v []byte }
	var rows []kv
	c := b.Cursor()
	for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
		rows = append(rows, kv{append([]byte(nil), k...), append([]byte(nil), v...)})
	}
	for _, row := range rows {
		var rec map[string]any
		if err := json.Unmarshal(row.v, &rec); err != nil {
			return err
		}
		rec["task_name"] = newName
		raw, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		newKey := append([]byte(newName+"/"), row.k[len(prefix):]...)
		if err := b.Put(newKey, raw); err != nil {
			return err
		}
		if err := b.Delete(row.k); err != nil {
			return err
		}
	}
	return nil
}
