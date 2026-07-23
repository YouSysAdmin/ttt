package notes

import (
	"errors"
	"strings"
	"time"

	"ttt/internal/core/env"
	"ttt/internal/core/errs"
	"ttt/internal/domain/store"
	"ttt/internal/models/note"
)

type Handler struct {
	Runtime *env.Runtime
	Store   *store.Store
}

// Add attaches text to taskName, or to the currently running task when
// taskName is empty (errs.ErrNothingRunning when idle). An explicit taskName
// must exist (errs.ErrTaskNotFound).
func (h *Handler) Add(taskName, text string, at time.Time) (*note.Note, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("note text must be non-empty")
	}

	if taskName == "" {
		active, err := h.Store.Tracker.Active()
		if err != nil {
			return nil, err
		}
		if active == nil {
			return nil, errs.ErrNothingRunning
		}
		taskName = active.TaskName
	} else {
		t, err := h.Store.Tasks.Get(taskName)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, errs.ErrTaskNotFound
		}
	}

	n := &note.Note{TaskName: taskName, CreatedAt: at, Text: text}
	if err := h.Store.Notes.Add(n); err != nil {
		return nil, err
	}
	return n, nil
}

// Edit replaces the note's text, keeping its timestamp (and thus its place
// in the task's note history).
func (h *Handler) Edit(n *note.Note, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("note text must be non-empty")
	}
	n.Text = text
	return h.Store.Notes.Update(n)
}

// Delete removes the note.
func (h *Handler) Delete(n *note.Note) error {
	return h.Store.Notes.Delete(n)
}
