package store

import "ttt/internal/core/env"

// Context is embedded by concrete domain stores: it carries the Runtime and
// proxies bucket-name lookups through the backend's StoreProvider so stores never hardcode the layout.
type Context struct {
	Runtime *env.Runtime
}

// GetTasksBucketName returns the bucket where tasks live, via the StoreProvider.
func (c *Context) GetTasksBucketName() string {
	return c.Runtime.StoreProvider.GetTasksBucketName()
}

// GetEntriesBucketName returns the bucket where time entries live, via the StoreProvider.
func (c *Context) GetEntriesBucketName() string {
	return c.Runtime.StoreProvider.GetEntriesBucketName()
}

// GetTrackerBucketName returns the bucket holding the active-tracking pointer, via the StoreProvider.
func (c *Context) GetTrackerBucketName() string {
	return c.Runtime.StoreProvider.GetTrackerBucketName()
}

// GetNotesBucketName returns the bucket where task notes live, via the StoreProvider.
func (c *Context) GetNotesBucketName() string {
	return c.Runtime.StoreProvider.GetNotesBucketName()
}
