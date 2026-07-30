package cli

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ttt/internal/core/env"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/store"
	remoteserver "ttt/internal/remote/server"
)

// newTestServer serves a temp boltkv store over HTTP, like `ttt server`.
func newTestServer(t *testing.T, token string) string {
	t.Helper()
	kv, err := boltkv.Open(filepath.Join(t.TempDir(), "ttt.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	rt := &env.Runtime{}
	st := &store.Store{}
	boltkv.BindProvider(rt, st, kv)

	ts := httptest.NewServer(remoteserver.New(st, token, "test"))
	t.Cleanup(ts.Close)
	return ts.URL
}

// runRemote executes the real command tree in client mode against url and
// returns stdout+stderr, asserting no local store was opened.
func runRemote(t *testing.T, url, token string, args ...string) string {
	t.Helper()
	app := &App{}
	root := newRootCmd(app, "0.0.1")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--remote-url", url, "--remote-token", token, "--json"}, args...))
	err := root.Execute()
	if app.kv != nil {
		t.Fatalf("client mode opened a local store")
	}
	if cerr := app.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	if err != nil {
		t.Fatalf("ttt %v: %v", args, err)
	}
	return out.String()
}

// TestRemoteCLI drives the same add/start/status/stop flow as the local JSON
// tests, but through a real HTTP server - the payload shapes must match
// local mode exactly, since the commands and handlers are shared.
func TestRemoteCLI(t *testing.T) {
	url := newTestServer(t, "secret")

	added := decode[map[string]any](t, runRemote(t, url, "secret", "add", "feat", "-d", "the desc", "-p", "acme"))
	if added["name"] != "feat" || added["status"] != "todo" || added["project"] != "acme" {
		t.Fatalf("add payload wrong: %v", added)
	}

	status := decode[map[string]any](t, runRemote(t, url, "secret"))
	if status["tracking"] != false {
		t.Fatalf("status payload wrong: %v", status)
	}

	started := decode[map[string]any](t, runRemote(t, url, "secret", "start", "feat"))
	if started["created"] != false || started["previous"] != nil {
		t.Fatalf("start payload wrong: %v", started)
	}
	if task, ok := started["task"].(map[string]any); !ok || task["status"] != "active" {
		t.Fatalf("start task wrong: %v", started["task"])
	}

	status = decode[map[string]any](t, runRemote(t, url, "secret"))
	if status["tracking"] != true {
		t.Fatalf("tracking status wrong: %v", status)
	}

	stopped := decode[map[string]any](t, runRemote(t, url, "secret", "stop"))
	if stopped["task_name"] != "feat" {
		t.Fatalf("stop payload wrong: %v", stopped)
	}
	if _, ok := stopped["session_seconds"].(float64); !ok {
		t.Fatalf("session_seconds missing: %v", stopped)
	}
}

func TestRemoteCLIBadToken(t *testing.T) {
	url := newTestServer(t, "secret")

	app := &App{}
	root := newRootCmd(app, "0.0.1")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--remote-url", url, "--remote-token", "wrong", "list"})
	err := root.Execute()
	_ = app.Close()
	if err == nil || !strings.Contains(err.Error(), "invalid or missing token") {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestRemoteAndDBFlagsConflict(t *testing.T) {
	app := &App{}
	root := newRootCmd(app, "0.0.1")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--remote-url", "http://localhost:1", "--db", filepath.Join(t.TempDir(), "ttt.db"), "list"})
	err := root.Execute()
	_ = app.Close()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error, got %v", err)
	}
}
