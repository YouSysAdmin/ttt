// Package boltkv is the bbolt backend for ttt: engine init, bucket
// management, and the database.Database interface. Domain-specific persistence
// lives under internal/domain/<thing>/store.go and reaches the bbolt handle via
// the *Store returned by Open (DB()).
package boltkv

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"ttt/internal/database"
)

// compile-time check: *Store must satisfy database.Database.
var _ database.Database = (*Store)(nil)

// Bucket names. Domain stores reach these through the database.Database
// interface rather than the vars directly, so a new backend can choose its own
// layout.
var (
	bucketTasks   = []byte("tasks")
	bucketEntries = []byte("entries")
	bucketTracker = []byte("tracker")
	bucketNotes   = []byte("notes")
)

// Store is the boltkv backend handle: wraps the bbolt DB, records the file
// path, and implements database.Database. Domain persistence methods live in
// internal/domain/<thing>/store.go.
type Store struct {
	db   *bbolt.DB
	path string
}

// Open opens (or creates) the bbolt file - making its parent dir if needed -
// and ensures every built-in bucket exists. The 5s timeout turns a stale flock
// into an error instead of a hang. The caller owns Close.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create store dir %q: %w", dir, err)
		}
	}
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bolt db %s: %w", path, err)
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, b := range [][]byte{bucketTasks, bucketEntries, bucketTracker, bucketNotes} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}
	return &Store{db: db, path: path}, nil
}

// DB returns the underlying bbolt handle, for domain stores that need
// transactions.
func (s *Store) DB() *bbolt.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// Close releases the bbolt lock. Safe to call twice.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the on-disk location of the bbolt file.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Backup writes a consistent snapshot to w via bbolt's Tx.WriteTo (a hot backup
// inside a read transaction) and returns the bytes written.
func (s *Store) Backup(w io.Writer) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store not open")
	}
	var n int64
	err := s.db.View(func(tx *bbolt.Tx) error {
		var werr error
		n, werr = tx.WriteTo(w)
		return werr
	})
	return n, err
}

// CreateBucket creates a top-level bucket by name if it doesn't exist.
func (s *Store) CreateBucket(name string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store not open")
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(name))
		return err
	})
}

// ListBuckets returns every top-level bucket name.
func (s *Store) ListBuckets() ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var out []string
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
			out = append(out, string(name))
			return nil
		})
	})
	return out, err
}

// GetTasksBucketName satisfies the database.Database interface.
func (s *Store) GetTasksBucketName() string { return string(bucketTasks) }

// GetEntriesBucketName satisfies the database.Database interface.
func (s *Store) GetEntriesBucketName() string { return string(bucketEntries) }

// GetTrackerBucketName satisfies the database.Database interface.
func (s *Store) GetTrackerBucketName() string { return string(bucketTracker) }

// GetNotesBucketName satisfies the database.Database interface.
func (s *Store) GetNotesBucketName() string { return string(bucketNotes) }
