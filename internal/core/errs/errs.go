// Package errs holds the sentinel errors shared across handlers and stores,
// matched with errors.Is at the CLI boundary.
package errs

import "errors"

var (
	ErrTaskExists     = errors.New("task already exists")
	ErrTaskNotFound   = errors.New("task not found")
	ErrAlreadyRunning = errors.New("task is already being tracked")
	ErrNothingRunning = errors.New("no task is currently being tracked")
	ErrInvalidName    = errors.New(`task name must be non-empty and must not contain "/"`)
)
