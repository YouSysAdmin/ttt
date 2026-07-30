package client

import (
	"ttt/internal/domain/store"
	"ttt/internal/models/note"
	"ttt/internal/remote/api"
)

// Notes implements store.NotesStore over the remote API.
type Notes struct {
	c *Client
}

var _ store.NotesStore = (*Notes)(nil)

func (s *Notes) ListByTask(name string) ([]*note.Note, error) {
	if snap, ok, err := s.c.cached(); err != nil {
		return nil, err
	} else if ok {
		return cloneSlice(snap.notes[name]), nil
	}
	var resp api.NotesResp
	if err := s.c.post("/v1/notes/list", api.NameReq{Name: name}, &resp); err != nil {
		return nil, err
	}
	return resp.Notes, nil
}

func (s *Notes) Add(n *note.Note) error {
	defer s.c.invalidate()
	var resp api.NoteResp
	if err := s.c.post("/v1/notes/add", api.NoteReq{Note: n}, &resp); err != nil {
		return err
	}
	if resp.Note != nil {
		// CreatedAt may have been bumped server-side for key uniqueness.
		*n = *resp.Note
	}
	return nil
}

func (s *Notes) Update(n *note.Note) error {
	defer s.c.invalidate()
	return s.c.post("/v1/notes/update", api.NoteReq{Note: n}, nil)
}

func (s *Notes) Delete(n *note.Note) error {
	defer s.c.invalidate()
	return s.c.post("/v1/notes/delete", api.NoteReq{Note: n}, nil)
}
