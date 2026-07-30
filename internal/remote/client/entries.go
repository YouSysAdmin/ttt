package client

import (
	"ttt/internal/domain/store"
	"ttt/internal/models/entry"
	"ttt/internal/remote/api"
)

// Entries implements store.EntriesStore over the remote API.
type Entries struct {
	c *Client
}

var _ store.EntriesStore = (*Entries)(nil)

func (s *Entries) ListByTask(name string) ([]*entry.Entry, error) {
	if snap, ok, err := s.c.cached(); err != nil {
		return nil, err
	} else if ok {
		return cloneSlice(snap.entries[name]), nil
	}
	var resp api.EntriesResp
	if err := s.c.post("/v1/entries/list", api.NameReq{Name: name}, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

func (s *Entries) Put(e *entry.Entry) error {
	defer s.c.invalidate()
	return s.c.post("/v1/entries/put", api.EntryReq{Entry: e}, nil)
}
