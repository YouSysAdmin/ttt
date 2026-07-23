// Package database declares the engine-agnostic persistence abstraction that
// storage backends implement. app keeps its state here: tasks, their time
// entries, and the active-tracking pointer.
//
// The interface stays narrow - backends layer richer typed methods on top, and
// domain stores reach the right bucket through the Get*BucketName accessors so
// the layout is owned by the backend, not the caller. The only backend today is boltkv (bbolt).
// The interface is the seam that lets another drop in later.
package database

import "io"

// Database is the minimum surface every backend exposes. Keep it small,
// per-domain richness lives in the concrete stores.
type Database interface {
	// Close releases backend resources. A second call may be a no-op.
	Close() error

	// Path returns the on-disk location (a file path for boltkv, or a
	// DSN/URL for a future network backend).
	Path() string

	// Backup writes a consistent snapshot of the whole store to w and returns
	// the bytes written. For boltkv this is bbolt's Tx.WriteTo - a hot backup
	// safe to take while the store is in use.
	Backup(w io.Writer) (int64, error)

	// CreateBucket creates a named bucket/namespace if absent, for ad-hoc
	// provisioning beyond the built-in buckets below.
	CreateBucket(name string) error

	// ListBuckets returns every bucket name known to the backend.
	ListBuckets() ([]string, error)

	// GetTasksBucketName returns the bucket where tasks live, keyed by name.
	// Read through here so the layout stays backend-owned.
	GetTasksBucketName() string

	// GetEntriesBucketName returns the bucket where time entries live, keyed
	// by "<task name>/<start timestamp>".
	GetEntriesBucketName() string

	// GetTrackerBucketName returns the bucket holding the active-tracking
	// pointer singleton.
	GetTrackerBucketName() string

	// GetNotesBucketName returns the bucket where task notes live, keyed by
	// "<task name>/<created-at timestamp>".
	GetNotesBucketName() string
}
