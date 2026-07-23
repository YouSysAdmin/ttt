package boltkv

import (
	"ttt/internal/core/env"
	"ttt/internal/domain/entries"
	"ttt/internal/domain/notes"
	"ttt/internal/domain/store"
	"ttt/internal/domain/tasks"
	"ttt/internal/domain/tracker"
)

// BindProvider wires an already-open boltkv store into the Runtime and the
// aggregate store.Store: Runtime gains the raw DB handle and StoreProvider
// (engine-agnostic bucket surface), while the aggregate Store gets the
// concrete boltkv implementation of each per-domain interface. The backend owns which
// implementation fills each slot, so callers stay backend-agnostic. The caller
// still owns kv and Close()s it at shutdown.
func BindProvider(rt *env.Runtime, st *store.Store, kv *Store) {
	rt.DB = kv.DB()
	rt.StoreProvider = kv

	ctx := store.Context{Runtime: rt}
	st.Tasks = &tasks.Store{Context: ctx}
	st.Entries = &entries.Store{Context: ctx}
	st.Tracker = &tracker.Store{Context: ctx}
	st.Notes = &notes.Store{Context: ctx}
}
