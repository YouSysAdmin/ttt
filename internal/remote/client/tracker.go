package client

import (
	"time"

	"ttt/internal/domain/store"
	"ttt/internal/models/task"
	"ttt/internal/models/tracker"
	"ttt/internal/remote/api"
)

// Tracker implements store.TrackerStore over the remote API.
type Tracker struct {
	c *Client
}

var _ store.TrackerStore = (*Tracker)(nil)

func (s *Tracker) Active() (*tracker.State, error) {
	if snap, ok, err := s.c.cached(); err != nil {
		return nil, err
	} else if ok {
		return clonePtr(snap.state), nil
	}
	var resp api.StateResp
	if err := s.c.post("/v1/tracker/active", api.Empty{}, &resp); err != nil {
		return nil, err
	}
	return resp.State, nil
}

func (s *Tracker) Start(t *task.Task, at time.Time) (*tracker.State, error) {
	defer s.c.invalidate()
	var resp api.StartResp
	if err := s.c.post("/v1/tracker/start", api.StartReq{Task: t, At: at}, &resp); err != nil {
		return nil, err
	}
	if resp.Task != nil {
		// Start upserts the task server-side - reflect the stamps.
		*t = *resp.Task
	}
	return resp.Previous, nil
}

func (s *Tracker) Close(at time.Time, status task.Status) (*tracker.State, error) {
	defer s.c.invalidate()
	var resp api.StateResp
	if err := s.c.post("/v1/tracker/close", api.CloseReq{At: at, Status: status}, &resp); err != nil {
		return nil, err
	}
	return resp.State, nil
}
