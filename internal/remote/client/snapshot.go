package client

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	"ttt/internal/models/tracker"
	"ttt/internal/remote/api"
)

// snapshot is one decoded /v1/state response. It is built once and never
// mutated afterwards - readers hand out clones, never pointers into it.
type snapshot struct {
	fetchedAt time.Time
	tasks     []*task.Task
	byName    map[string]*task.Task
	entries   map[string][]*entry.Entry
	notes     map[string][]*note.Note
	state     *tracker.State
}

// cached returns the fresh snapshot when the read cache is usable. ok=false
// means the caller must use its per-method route instead (cache disabled, or
// the server predates /v1/state). Holding the mutex across the fetch is fine
// because all store calls run in one goroutine - it just guarantees a single
// fetch per expiry by construction.
func (c *Client) cached() (*snapshot, bool, error) {
	if c.ttl <= 0 {
		return nil, false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapUnsupported {
		return nil, false, nil
	}
	if c.snap != nil && time.Since(c.snap.fetchedAt) < c.ttl {
		return c.snap, true, nil
	}

	var resp api.StateSnapshotResp
	if err := c.post("/v1/state", api.Empty{}, &resp); err != nil {
		// An old server has no /v1/state and answers with a bare 404. Fall
		// back to the per-method routes for the life of this client so
		// version skew degrades instead of failing every read - but say so,
		// because the fallback is one request per read and feels sluggish.
		var se *statusError
		if errors.As(err, &se) && se.status == http.StatusNotFound {
			c.snapUnsupported = true
			fmt.Fprintln(os.Stderr, "warning: remote server predates the read cache (no /v1/state) - every read is a separate request, upgrade the server")
			return nil, false, nil
		}
		return nil, false, err
	}

	snap := &snapshot{
		fetchedAt: time.Now(),
		tasks:     resp.Tasks,
		byName:    make(map[string]*task.Task, len(resp.Tasks)),
		entries:   resp.Entries,
		notes:     resp.Notes,
		state:     resp.State,
	}
	for _, t := range resp.Tasks {
		snap.byName[t.Name] = t
	}
	c.snap = snap
	return snap, true, nil
}

// NextSync reports when the read cache will fetch a fresh snapshot. ok is
// false when reads go straight to the server: cache disabled, server without
// /v1/state, or nothing fetched yet.
func (c *Client) NextSync() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 || c.snapUnsupported || c.snap == nil {
		return time.Time{}, false
	}
	return c.snap.fetchedAt.Add(c.ttl), true
}

// invalidate drops the snapshot. Every write calls it unconditionally (even
// on error - a timed-out write may still have committed server-side), and
// composite transitions touch several buckets, so the whole snapshot goes.
func (c *Client) invalidate() {
	c.mu.Lock()
	c.snap = nil
	c.mu.Unlock()
}

// clonePtr returns a shallow copy of the record. The models hold only value
// fields (strings, time.Time), so a shallow copy is a full copy. nil stays
// nil, preserving the (nil, nil) absence contract.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// cloneSlice clones every element. A nil slice stays nil, matching what the
// boltkv stores return for tasks with no records.
func cloneSlice[T any](s []*T) []*T {
	if s == nil {
		return nil
	}
	out := make([]*T, len(s))
	for i, p := range s {
		out[i] = clonePtr(p)
	}
	return out
}
