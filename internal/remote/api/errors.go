package api

import (
	"errors"
	"fmt"
	"net/http"

	"ttt/internal/core/errs"
)

// WireError is the payload of the error envelope every non-2xx response carries.
type WireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorEnvelope is the JSON body of every error response.
type ErrorEnvelope struct {
	Error WireError `json:"error"`
}

// Codes without a sentinel counterpart, raised by the transport itself.
const (
	CodeUnauthorized = "unauthorized"
	CodeBadRequest   = "bad_request"
	CodeInternal     = "internal"
)

// codeTable maps the sentinel errors from internal/core/errs to stable wire
// codes and HTTP statuses. Both EncodeError and DecodeError walk this one
// table so the mapping can never diverge between server and client.
var codeTable = []struct {
	code     string
	sentinel error
	status   int
}{
	{"task_exists", errs.ErrTaskExists, http.StatusConflict},
	{"task_not_found", errs.ErrTaskNotFound, http.StatusNotFound},
	{"already_running", errs.ErrAlreadyRunning, http.StatusConflict},
	{"nothing_running", errs.ErrNothingRunning, http.StatusConflict},
	{"invalid_name", errs.ErrInvalidName, http.StatusBadRequest},
}

// EncodeError classifies a store error for the wire: a known sentinel gets
// its stable code and status, anything else is an opaque internal error.
func EncodeError(err error) (code string, status int) {
	for _, e := range codeTable {
		if errors.Is(err, e.sentinel) {
			return e.code, e.status
		}
	}
	return CodeInternal, http.StatusInternalServerError
}

// DecodeError turns a wire error back into a Go error. Known codes return
// the sentinel identity so errors.Is keeps working in handlers and the CLI.
// Unknown codes surface the server's message.
func DecodeError(code, message string) error {
	for _, e := range codeTable {
		if code == e.code {
			return e.sentinel
		}
	}
	if message == "" {
		message = code
	}
	return fmt.Errorf("remote: %s", message)
}
