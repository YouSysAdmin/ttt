// Package server exposes a store.Store over HTTP/JSON: one POST route per
// store method (see internal/remote/api for the wire types), guarded by a
// static bearer token. Business logic stays in the client's handlers - the
// server only executes store methods, each of which is a single bbolt
// transaction, so concurrent clients serialize naturally.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"ttt/internal/domain/store"
	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	"ttt/internal/remote/api"
)

// New returns the API handler serving st. token must be non-empty (the CLI
// refuses to start otherwise) and version is reported by /v1/ping.
func New(st *store.Store, token, version string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/ping", handle(func(_ *api.Empty) (*api.PingResp, error) {
		return &api.PingResp{OK: true, Version: version}, nil
	}))

	mux.HandleFunc("POST /v1/tasks/get", handle(func(req *api.NameReq) (*api.TaskResp, error) {
		t, err := st.Tasks.Get(req.Name)
		return &api.TaskResp{Task: t}, err
	}))
	mux.HandleFunc("POST /v1/tasks/list", handle(func(_ *api.Empty) (*api.TasksResp, error) {
		ts, err := st.Tasks.List()
		return &api.TasksResp{Tasks: ts}, err
	}))
	mux.HandleFunc("POST /v1/tasks/upsert", handle(func(req *api.TaskReq) (*api.TaskResp, error) {
		if req.Task == nil {
			return nil, errBadRequest
		}
		// Name validation lives in the handlers, which run client-side in
		// remote mode - re-check on every write so a misbehaving client
		// can't smuggle "/" into the <name>/<timestamp> key space.
		if err := task.ValidateName(req.Task.Name); err != nil {
			return nil, err
		}
		if err := st.Tasks.Upsert(req.Task); err != nil {
			return nil, err
		}
		// Return the record so the client can copy the stamped timestamps
		// back into the caller's task, preserving Upsert's in-place contract.
		return &api.TaskResp{Task: req.Task}, nil
	}))
	mux.HandleFunc("POST /v1/tasks/delete", handle(func(req *api.NameReq) (*api.Empty, error) {
		return &api.Empty{}, st.Tasks.Delete(req.Name)
	}))
	mux.HandleFunc("POST /v1/tasks/rename", handle(func(req *api.RenameReq) (*api.Empty, error) {
		if err := task.ValidateName(req.NewName); err != nil {
			return nil, err
		}
		return &api.Empty{}, st.Tasks.Rename(req.OldName, req.NewName)
	}))

	mux.HandleFunc("POST /v1/entries/list", handle(func(req *api.NameReq) (*api.EntriesResp, error) {
		es, err := st.Entries.ListByTask(req.Name)
		return &api.EntriesResp{Entries: es}, err
	}))
	mux.HandleFunc("POST /v1/entries/put", handle(func(req *api.EntryReq) (*api.Empty, error) {
		if req.Entry == nil {
			return nil, errBadRequest
		}
		if err := task.ValidateName(req.Entry.TaskName); err != nil {
			return nil, err
		}
		return &api.Empty{}, st.Entries.Put(req.Entry)
	}))

	mux.HandleFunc("POST /v1/notes/list", handle(func(req *api.NameReq) (*api.NotesResp, error) {
		ns, err := st.Notes.ListByTask(req.Name)
		return &api.NotesResp{Notes: ns}, err
	}))
	mux.HandleFunc("POST /v1/notes/add", handle(func(req *api.NoteReq) (*api.NoteResp, error) {
		if req.Note == nil {
			return nil, errBadRequest
		}
		if err := task.ValidateName(req.Note.TaskName); err != nil {
			return nil, err
		}
		if err := st.Notes.Add(req.Note); err != nil {
			return nil, err
		}
		// CreatedAt may have been bumped for key uniqueness - return it.
		return &api.NoteResp{Note: req.Note}, nil
	}))
	mux.HandleFunc("POST /v1/notes/update", handle(func(req *api.NoteReq) (*api.Empty, error) {
		if req.Note == nil {
			return nil, errBadRequest
		}
		return &api.Empty{}, st.Notes.Update(req.Note)
	}))
	mux.HandleFunc("POST /v1/notes/delete", handle(func(req *api.NoteReq) (*api.Empty, error) {
		if req.Note == nil {
			return nil, errBadRequest
		}
		return &api.Empty{}, st.Notes.Delete(req.Note)
	}))

	// One-shot snapshot of the whole store for the client-side read cache.
	// Built from a few separate read transactions, not one atomic view - a
	// concurrent write can tear it, which self-heals on the next fetch.
	mux.HandleFunc("POST /v1/state", handle(func(_ *api.Empty) (*api.StateSnapshotResp, error) {
		ts, err := st.Tasks.List()
		if err != nil {
			return nil, err
		}
		resp := &api.StateSnapshotResp{Tasks: ts}
		if resp.Entries, err = allEntries(st, ts); err != nil {
			return nil, err
		}
		if resp.Notes, err = allNotes(st, ts); err != nil {
			return nil, err
		}
		if resp.State, err = st.Tracker.Active(); err != nil {
			return nil, err
		}
		return resp, nil
	}))

	mux.HandleFunc("POST /v1/tracker/active", handle(func(_ *api.Empty) (*api.StateResp, error) {
		s, err := st.Tracker.Active()
		return &api.StateResp{State: s}, err
	}))
	mux.HandleFunc("POST /v1/tracker/start", handle(func(req *api.StartReq) (*api.StartResp, error) {
		if req.Task == nil {
			return nil, errBadRequest
		}
		if err := task.ValidateName(req.Task.Name); err != nil {
			return nil, err
		}
		prev, err := st.Tracker.Start(req.Task, req.At)
		if err != nil {
			return nil, err
		}
		return &api.StartResp{Task: req.Task, Previous: prev}, nil
	}))
	mux.HandleFunc("POST /v1/tracker/close", handle(func(req *api.CloseReq) (*api.StateResp, error) {
		s, err := st.Tracker.Close(req.At, req.Status)
		return &api.StateResp{State: s}, err
	}))

	return withAuth(token, mux)
}

// errBadRequest marks a structurally invalid request (missing record).
var errBadRequest = errors.New("missing record in request body")

// allEntries collects every entry grouped by task. The boltkv store exposes
// a one-scan All() that also catches records whose task is absent - the
// per-task fallback keeps other backends working.
func allEntries(st *store.Store, ts []*task.Task) (map[string][]*entry.Entry, error) {
	if b, ok := st.Entries.(interface {
		All() (map[string][]*entry.Entry, error)
	}); ok {
		return b.All()
	}
	out := map[string][]*entry.Entry{}
	for _, t := range ts {
		es, err := st.Entries.ListByTask(t.Name)
		if err != nil {
			return nil, err
		}
		if len(es) > 0 {
			out[t.Name] = es
		}
	}
	return out, nil
}

// allNotes is allEntries for notes.
func allNotes(st *store.Store, ts []*task.Task) (map[string][]*note.Note, error) {
	if b, ok := st.Notes.(interface {
		All() (map[string][]*note.Note, error)
	}); ok {
		return b.All()
	}
	out := map[string][]*note.Note{}
	for _, t := range ts {
		ns, err := st.Notes.ListByTask(t.Name)
		if err != nil {
			return nil, err
		}
		if len(ns) > 0 {
			out[t.Name] = ns
		}
	}
	return out, nil
}

// handle adapts one store call to HTTP: decode the JSON body (capped at
// 1 MB, an empty body decodes to the zero request), run fn, and write either
// the response or the error envelope from api.EncodeError.
func handle[Req, Resp any](fn func(*Req) (*Resp, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req Req
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, api.CodeBadRequest, "decode request: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := fn(&req)
		if err != nil {
			if errors.Is(err, errBadRequest) {
				writeError(w, api.CodeBadRequest, err.Error(), http.StatusBadRequest)
				return
			}
			code, status := api.EncodeError(err)
			writeError(w, code, err.Error(), status)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// withAuth rejects requests whose bearer token doesn't match, in constant
// time. All routes, including ping, are guarded - the server never reveals
// anything without the token.
func withAuth(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			writeError(w, api.CodeUnauthorized, "invalid or missing token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code, message string, status int) {
	writeJSON(w, status, api.ErrorEnvelope{Error: api.WireError{Code: code, Message: message}})
}
