// Round-trip tests for the remote backend: a real boltkv store behind the
// HTTP server, driven through the remote client. Running the shared
// storetest suite proves sentinel identity and the in-place mutation
// contracts survive the wire.
package remote_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ttt/internal/core/env"
	"ttt/internal/core/errs"
	ttls "ttt/internal/core/tls"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/store"
	"ttt/internal/domain/store/storetest"
	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	"ttt/internal/remote/client"
	"ttt/internal/remote/server"
)

const testToken = "test-token"

// newBoltStore opens a fresh local store, the same helper the domain tests use.
func newBoltStore(t *testing.T) *store.Store {
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

// newHandler builds the API handler over a fresh local store.
func newHandler(t *testing.T) http.Handler {
	t.Helper()
	return server.New(newBoltStore(t), testToken, "test")
}

// newServer serves a fresh local store over HTTP and returns its URL.
func newServer(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(newHandler(t))
	t.Cleanup(ts.Close)
	return ts.URL
}

// newRemoteStoreTTL builds a store whose calls round-trip through the HTTP
// server to a real boltkv store, with the given read-cache TTL.
func newRemoteStoreTTL(t *testing.T, ttl time.Duration) *store.Store {
	t.Helper()
	c, err := client.New(newServer(t), client.Options{Token: testToken, CacheTTL: ttl})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	st := &store.Store{}
	client.BindProvider(st, c)
	return st
}

// newRemoteStore is the storetest factory, cache on - Options are built
// directly here, so the config default doesn't apply and the TTL must be
// passed explicitly for the suite to exercise the cached path.
func newRemoteStore(t *testing.T) *store.Store {
	return newRemoteStoreTTL(t, time.Second)
}

func TestRemoteStore(t *testing.T) {
	storetest.All(t, newRemoteStore)
}

func TestRemoteStoreUncached(t *testing.T) {
	storetest.All(t, func(t *testing.T) *store.Store {
		return newRemoteStoreTTL(t, 0)
	})
}

func TestAuthRejected(t *testing.T) {
	url := newServer(t)
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"wrong token", "not-the-token"},
		{"missing token", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := client.New(url, client.Options{Token: tc.token})
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			if _, err := c.Ping(); err == nil || !strings.Contains(err.Error(), "invalid or missing token") {
				t.Fatalf("expected unauthorized error, got %v", err)
			}
		})
	}
}

func TestPing(t *testing.T) {
	c, err := client.New(newServer(t), client.Options{Token: testToken})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	v, err := c.Ping()
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if v != "test" {
		t.Fatalf("version = %q, want %q", v, "test")
	}
}

// TestSentinelIdentity spot-checks that a sentinel raised inside the server's
// store comes back as the same error value, not just the same text.
func TestSentinelIdentity(t *testing.T) {
	st := newRemoteStore(t)
	if err := st.Tasks.Rename("missing", "x"); !errors.Is(err, errs.ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound identity across the wire, got %#v", err)
	}
}

// TestServerRejectsInvalidNames proves the server re-validates task names on
// every write route: handlers run client-side in remote mode, so a
// misbehaving client could otherwise smuggle "/" into the key space.
func TestServerRejectsInvalidNames(t *testing.T) {
	st := newRemoteStore(t)
	now := time.Now()

	for _, bad := range []string{"", "a/b"} {
		if err := st.Tasks.Upsert(&task.Task{Name: bad, Status: task.StatusTodo}); !errors.Is(err, errs.ErrInvalidName) {
			t.Fatalf("upsert %q: expected ErrInvalidName, got %v", bad, err)
		}
		if _, err := st.Tracker.Start(&task.Task{Name: bad, Status: task.StatusActive}, now); !errors.Is(err, errs.ErrInvalidName) {
			t.Fatalf("start %q: expected ErrInvalidName, got %v", bad, err)
		}
		if err := st.Entries.Put(&entry.Entry{TaskName: bad, Start: now}); !errors.Is(err, errs.ErrInvalidName) {
			t.Fatalf("put entry %q: expected ErrInvalidName, got %v", bad, err)
		}
		if err := st.Notes.Add(&note.Note{TaskName: bad, CreatedAt: now, Text: "x"}); !errors.Is(err, errs.ErrInvalidName) {
			t.Fatalf("add note %q: expected ErrInvalidName, got %v", bad, err)
		}
	}

	if err := st.Tasks.Upsert(&task.Task{Name: "ok", Status: task.StatusTodo}); err != nil {
		t.Fatalf("upsert ok: %v", err)
	}
	if err := st.Tasks.Rename("ok", "bad/name"); !errors.Is(err, errs.ErrInvalidName) {
		t.Fatalf("rename: expected ErrInvalidName, got %v", err)
	}
}

func TestSelfSignedTLS(t *testing.T) {
	cfg, err := ttls.SelfSignedTLS("localhost", "ed25519")
	if err != nil {
		t.Fatalf("self-signed config: %v", err)
	}
	ts := httptest.NewUnstartedServer(server.New(newBoltStore(t), testToken, "test"))
	ts.TLS = cfg
	ts.StartTLS()
	t.Cleanup(ts.Close)

	// Without Insecure the self-signed cert must be rejected...
	strict, err := client.New(ts.URL, client.Options{Token: testToken})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := strict.Ping(); err == nil {
		t.Fatal("expected certificate verification failure")
	}

	// ...and Insecure lets the connection through.
	insecure, err := client.New(ts.URL, client.Options{Token: testToken, Insecure: true})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := insecure.Ping(); err != nil {
		t.Fatalf("ping over self-signed TLS: %v", err)
	}
}
