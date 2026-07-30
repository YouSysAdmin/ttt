package client

import (
	"ttt/internal/domain/store"
	"ttt/internal/models/task"
	"ttt/internal/remote/api"
)

// Tasks implements store.TasksStore over the remote API.
type Tasks struct {
	c *Client
}

var _ store.TasksStore = (*Tasks)(nil)

func (s *Tasks) Get(name string) (*task.Task, error) {
	if snap, ok, err := s.c.cached(); err != nil {
		return nil, err
	} else if ok {
		return clonePtr(snap.byName[name]), nil
	}
	var resp api.TaskResp
	if err := s.c.post("/v1/tasks/get", api.NameReq{Name: name}, &resp); err != nil {
		return nil, err
	}
	return resp.Task, nil
}

func (s *Tasks) List() ([]*task.Task, error) {
	if snap, ok, err := s.c.cached(); err != nil {
		return nil, err
	} else if ok {
		return cloneSlice(snap.tasks), nil
	}
	var resp api.TasksResp
	if err := s.c.post("/v1/tasks/list", api.Empty{}, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

func (s *Tasks) Upsert(t *task.Task) error {
	defer s.c.invalidate()
	var resp api.TaskResp
	if err := s.c.post("/v1/tasks/upsert", api.TaskReq{Task: t}, &resp); err != nil {
		return err
	}
	if resp.Task != nil {
		// The server stamped CreatedAt/UpdatedAt - reflect them on the
		// caller's record, as the local store does.
		*t = *resp.Task
	}
	return nil
}

func (s *Tasks) Delete(name string) error {
	defer s.c.invalidate()
	return s.c.post("/v1/tasks/delete", api.NameReq{Name: name}, nil)
}

func (s *Tasks) Rename(oldName, newName string) error {
	defer s.c.invalidate()
	return s.c.post("/v1/tasks/rename", api.RenameReq{OldName: oldName, NewName: newName}, nil)
}
