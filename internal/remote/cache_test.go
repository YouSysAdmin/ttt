// Tests for the client-side read snapshot cache: request counting proves the
// savings, and dedicated tests pin the invalidation, clone-on-read, and
// old-server fallback behavior. The correctness of cached reads themselves is
// covered by running the full storetest suite with the cache on (remote_test.go).
package remote_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"ttt/internal/domain/store"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	"ttt/internal/remote/client"
)

// pathCounter counts requests per path, wrapped around the real API handler.
type pathCounter struct {
	mu sync.Mutex
	m  map[string]int
}

func (p *pathCounter) get(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[path]
}

func (p *pathCounter) reset() {
	p.mu.Lock()
	p.m = map[string]int{}
	p.mu.Unlock()
}

// newCountedStore builds a remote store whose server counts requests by
// path. notFound paths are answered with a bare 404 instead of being served,
// which is how an old server without that route behaves.
func newCountedStore(t *testing.T, ttl time.Duration, notFound ...string) (*store.Store, *pathCounter) {
	t.Helper()
	api := newHandler(t)
	counter := &pathCounter{m: map[string]int{}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.mu.Lock()
		counter.m[r.URL.Path]++
		counter.mu.Unlock()
		if slices.Contains(notFound, r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	c, err := client.New(ts.URL, client.Options{Token: testToken, CacheTTL: ttl})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	st := &store.Store{}
	client.BindProvider(st, c)
	return st, counter
}

// readBurst performs the read mix one TUI refresh produces.
func readBurst(t *testing.T, st *store.Store, name string) {
	t.Helper()
	if _, err := st.Tasks.List(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := st.Tasks.Get(name); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := st.Entries.ListByTask(name); err != nil {
		t.Fatalf("entries: %v", err)
	}
	if _, err := st.Notes.ListByTask(name); err != nil {
		t.Fatalf("notes: %v", err)
	}
	if _, err := st.Tracker.Active(); err != nil {
		t.Fatalf("active: %v", err)
	}
}

func TestSnapshotSingleFetch(t *testing.T) {
	st, counter := newCountedStore(t, time.Minute)
	now := time.Now()
	if _, err := st.Tracker.Start(&task.Task{Name: "a", Status: task.StatusActive}, now); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := st.Notes.Add(&note.Note{TaskName: "a", CreatedAt: now, Text: "n"}); err != nil {
		t.Fatalf("note: %v", err)
	}

	counter.reset()
	readBurst(t, st, "a")
	readBurst(t, st, "a")

	if got := counter.get("/v1/state"); got != 1 {
		t.Fatalf("expected exactly 1 snapshot fetch, got %d", got)
	}
	for _, p := range []string{"/v1/tasks/list", "/v1/tasks/get", "/v1/entries/list", "/v1/notes/list", "/v1/tracker/active"} {
		if got := counter.get(p); got != 0 {
			t.Fatalf("read route %s hit %d times, want 0", p, got)
		}
	}
}

func TestSnapshotTTLExpiry(t *testing.T) {
	st, counter := newCountedStore(t, 20*time.Millisecond)
	if _, err := st.Tasks.List(); err != nil {
		t.Fatalf("list: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := st.Tasks.List(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := counter.get("/v1/state"); got != 2 {
		t.Fatalf("expected refetch after TTL, got %d snapshot fetches", got)
	}
}

func TestSnapshotInvalidatedOnWrite(t *testing.T) {
	st, counter := newCountedStore(t, time.Minute)
	if _, err := st.Tasks.List(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := st.Tasks.Upsert(&task.Task{Name: "a", Status: task.StatusTodo}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := st.Tasks.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("write not visible after invalidation")
	}
	if fetches := counter.get("/v1/state"); fetches != 2 {
		t.Fatalf("expected refetch after write, got %d snapshot fetches", fetches)
	}
}

func TestSnapshotCloneOnRead(t *testing.T) {
	st, _ := newCountedStore(t, time.Minute)
	now := time.Now()
	if _, err := st.Tracker.Start(&task.Task{Name: "a", Status: task.StatusActive}, now); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Mutating returned records must never leak into the cache.
	got, _ := st.Tasks.Get("a")
	got.Status = task.StatusDone
	again, _ := st.Tasks.Get("a")
	if again.Status != task.StatusActive {
		t.Fatalf("cache polluted through Get: %+v", again)
	}

	el, _ := st.Entries.ListByTask("a")
	el[0].TaskName = "mutated"
	el2, _ := st.Entries.ListByTask("a")
	if el2[0].TaskName != "a" {
		t.Fatalf("cache polluted through ListByTask: %+v", el2[0])
	}

	state, _ := st.Tracker.Active()
	state.TaskName = "mutated"
	state2, _ := st.Tracker.Active()
	if state2.TaskName != "a" {
		t.Fatalf("cache polluted through Active: %+v", state2)
	}
}

// An old server binary has no /v1/state route and answers a bare 404. The
// client must fall back to per-method reads instead of failing every command.
func TestSnapshotUnsupportedServerFallsBack(t *testing.T) {
	st, counter := newCountedStore(t, time.Minute, "/v1/state")
	if err := st.Tasks.Upsert(&task.Task{Name: "a", Status: task.StatusTodo}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	readBurst(t, st, "a")
	readBurst(t, st, "a")

	ts, err := st.Tasks.List()
	if err != nil || len(ts) != 1 {
		t.Fatalf("fallback list: %v, %+v", err, ts)
	}
	if got := counter.get("/v1/state"); got != 1 {
		t.Fatalf("expected a single probe of /v1/state, got %d", got)
	}
	if got := counter.get("/v1/tasks/list"); got < 2 {
		t.Fatalf("per-method fallback not used, /v1/tasks/list hit %d times", got)
	}
}
