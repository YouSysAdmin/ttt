package tasks_test

import (
	"path/filepath"
	"testing"

	"ttt/internal/core/env"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/store"
	"ttt/internal/domain/store/storetest"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	kv, err := boltkv.Open(filepath.Join(t.TempDir(), "ttt.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = kv.Close() })

	rt := &env.Runtime{}
	st := &store.Store{}
	boltkv.BindProvider(rt, st, kv)
	return st
}

// The store contract tests live in storetest so the remote backend runs the
// same suite (see internal/remote).
func TestStore(t *testing.T) {
	storetest.Tasks(t, newTestStore)
}
