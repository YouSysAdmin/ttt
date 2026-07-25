package update

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// withCacheDir redirects the notice cache into a temp dir for the test.
func withCacheDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := userCacheDir
	userCacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDir = orig })
}

func TestNotifyIfOutdated(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		json.NewEncoder(w).Encode(Release{TagName: "v2.0.0"})
	}))
	defer srv.Close()
	withReleaseAPI(t, srv.URL)
	withCacheDir(t)

	t.Run("outdated prints once per run and caches", func(t *testing.T) {
		var buf bytes.Buffer
		NotifyIfOutdated("1.0.0", &buf)
		out := buf.String()
		if !strings.Contains(out, "Update available: v1.0.0 -> v2.0.0") {
			t.Fatalf("output = %q, want update notice", out)
		}
		if !strings.Contains(out, "run:") {
			t.Fatalf("output = %q, want an upgrade hint", out)
		}
		if got := requests.Load(); got != 1 {
			t.Fatalf("requests = %d, want 1", got)
		}

		// Second invocation inside the TTL: still notifies, from cache only.
		buf.Reset()
		NotifyIfOutdated("1.0.0", &buf)
		if !strings.Contains(buf.String(), "v2.0.0") {
			t.Fatalf("cached notice missing: %q", buf.String())
		}
		if got := requests.Load(); got != 1 {
			t.Fatalf("requests = %d, want 1 (cache must prevent refetch)", got)
		}
	})

	t.Run("up to date is silent but still cached", func(t *testing.T) {
		var buf bytes.Buffer
		NotifyIfOutdated("2.0.0", &buf)
		if buf.Len() != 0 {
			t.Fatalf("expected silence when up to date, got %q", buf.String())
		}
	})

	t.Run("stale cache triggers a refetch", func(t *testing.T) {
		writeStale := func() {
			path, err := cachePath()
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(checkCache{CheckedAt: time.Now().Add(-2 * notifyTTL), Latest: "1.5.0"})
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		writeStale()
		before := requests.Load()
		var buf bytes.Buffer
		NotifyIfOutdated("1.0.0", &buf)
		if got := requests.Load(); got != before+1 {
			t.Fatalf("requests = %d, want %d (stale cache must refetch)", got, before+1)
		}
		if !strings.Contains(buf.String(), "v2.0.0") {
			t.Fatalf("notice should use the refetched version, got %q", buf.String())
		}
	})
}

func TestNotifyIfOutdated_Silent(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withReleaseAPI(t, srv.URL)
	withCacheDir(t)

	t.Run("dev build makes no request", func(t *testing.T) {
		for _, v := range []string{"", "dev"} {
			var buf bytes.Buffer
			NotifyIfOutdated(v, &buf)
			if buf.Len() != 0 || requests.Load() != 0 {
				t.Fatalf("dev build %q must be silent with no requests", v)
			}
		}
	})

	t.Run("API errors are swallowed", func(t *testing.T) {
		var buf bytes.Buffer
		NotifyIfOutdated("1.0.0", &buf)
		if buf.Len() != 0 {
			t.Fatalf("expected silence on API error, got %q", buf.String())
		}
	})
}
