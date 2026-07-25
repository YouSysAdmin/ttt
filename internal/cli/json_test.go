package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

// runJSON executes the real command tree with --json against a temp store
// and returns stdout. The App is closed after each run, so consecutive calls
// in one test share the database file like real invocations do.
func runJSON(t *testing.T, db string, args ...string) string {
	t.Helper()
	app := &App{}
	root := newRootCmd(app, "0.0.1")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--db", db, "--json"}, args...))
	err := root.Execute()
	if cerr := app.Close(); cerr != nil {
		t.Fatalf("close store: %v", cerr)
	}
	if err != nil {
		t.Fatalf("ttt %v: %v", args, err)
	}
	return out.String()
}

func decode[T any](t *testing.T, raw string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, raw)
	}
	return v
}

func TestJSONOutputs(t *testing.T) {
	db := filepath.Join(t.TempDir(), "ttt.db")

	// add: the task object.
	added := decode[map[string]any](t, runJSON(t, db, "add", "feat", "-d", "the desc", "-p", "acme"))
	if added["name"] != "feat" || added["status"] != "todo" || added["project"] != "acme" {
		t.Fatalf("add payload wrong: %v", added)
	}

	// bare ttt: not tracking yet.
	status := decode[map[string]any](t, runJSON(t, db))
	if status["tracking"] != false {
		t.Fatalf("status payload wrong: %v", status)
	}

	// start: task + created + previous (null here).
	started := decode[map[string]any](t, runJSON(t, db, "start", "feat"))
	if started["created"] != false || started["previous"] != nil {
		t.Fatalf("start payload wrong: %v", started)
	}
	if task, ok := started["task"].(map[string]any); !ok || task["status"] != "active" {
		t.Fatalf("start task wrong: %v", started["task"])
	}

	// bare ttt while tracking.
	status = decode[map[string]any](t, runJSON(t, db))
	if status["tracking"] != true {
		t.Fatalf("tracking status wrong: %v", status)
	}
	if _, ok := status["session_seconds"].(float64); !ok {
		t.Fatalf("session_seconds missing: %v", status)
	}

	// note: the note object.
	noted := decode[map[string]any](t, runJSON(t, db, "note", "PR", "link"))
	if noted["task_name"] != "feat" || noted["text"] != "PR link" {
		t.Fatalf("note payload wrong: %v", noted)
	}

	// starting another task reports the preempted one under "previous".
	started = decode[map[string]any](t, runJSON(t, db, "start", "other"))
	if started["created"] != true {
		t.Fatalf("auto-create not reported: %v", started)
	}
	prev, ok := started["previous"].(map[string]any)
	if !ok || prev["task_name"] != "feat" {
		t.Fatalf("previous session wrong: %v", started["previous"])
	}

	// stop: the closed session.
	stopped := decode[map[string]any](t, runJSON(t, db, "stop"))
	if stopped["task_name"] != "other" {
		t.Fatalf("stop payload wrong: %v", stopped)
	}
	if _, ok := stopped["session_seconds"].(float64); !ok {
		t.Fatalf("stop session_seconds missing: %v", stopped)
	}

	// list: an array of rows.
	rows := decode[[]map[string]any](t, runJSON(t, db, "list"))
	if len(rows) != 2 {
		t.Fatalf("list rows = %d, want 2\n%v", len(rows), rows)
	}

	// show: task + entries + notes, never null collections.
	shown := decode[map[string]any](t, runJSON(t, db, "show", "feat"))
	if _, ok := shown["entries"].([]any); !ok {
		t.Fatalf("show entries not an array: %v", shown["entries"])
	}
	if notes, ok := shown["notes"].([]any); !ok || len(notes) != 1 {
		t.Fatalf("show notes wrong: %v", shown["notes"])
	}

	// edit: the updated task object.
	edited := decode[map[string]any](t, runJSON(t, db, "edit", "feat", "-d", "new desc"))
	if edited["description"] != "new desc" {
		t.Fatalf("edit payload wrong: %v", edited)
	}

	// stats: one document with tasks, projects, and the total.
	stats := decode[map[string]any](t, runJSON(t, db, "stats"))
	for _, key := range []string{"from", "to", "tasks", "projects", "total_seconds", "total"} {
		if _, ok := stats[key]; !ok {
			t.Fatalf("stats missing %q: %v", key, stats)
		}
	}
}

func TestJSONErrorEnvelope(t *testing.T) {
	// Pausing with nothing running errors; Execute (not tested here) wraps
	// it in {"error": ...}. Here we pin that the command itself emits no
	// partial output before failing.
	db := filepath.Join(t.TempDir(), "ttt.db")
	app := &App{}
	root := newRootCmd(app, "0.0.1")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--db", db, "--json", "pause"})
	err := root.Execute()
	_ = app.Close()
	if err == nil {
		t.Fatal("expected error for pause with nothing running")
	}
	if out.Len() != 0 {
		t.Fatalf("failed command must not print partial output, got %q", out.String())
	}
	if !app.JSON {
		t.Fatal("app.JSON must be set from the persistent flag")
	}
}
