package tasks_test

import (
	"testing"
	"time"

	"ttt/internal/domain/tasks"
	"ttt/internal/models/entry"
	"ttt/internal/models/task"
)

func TestStatsClipsToWindow(t *testing.T) {
	st := newTestStore(t)
	h := &tasks.Handler{Store: st}

	now := time.Now()
	from := now.Add(-1 * time.Hour)

	for _, tk := range []*task.Task{
		{Name: "inside", Project: "work", Status: task.StatusDone},
		{Name: "straddle", Project: "work", Status: task.StatusDone},
		{Name: "before", Status: task.StatusDone},
		{Name: "running", Status: task.StatusActive},
	} {
		if err := st.Tasks.Upsert(tk); err != nil {
			t.Fatalf("upsert %s: %v", tk.Name, err)
		}
	}
	for _, e := range []*entry.Entry{
		// Fully inside the window: counts whole (10 min).
		{TaskName: "inside", Start: from.Add(10 * time.Minute), End: from.Add(20 * time.Minute)},
		// Started 30 min before the window, ends 15 min in: clipped to 15 min.
		{TaskName: "straddle", Start: from.Add(-30 * time.Minute), End: from.Add(15 * time.Minute)},
		// Entirely before the window: excluded.
		{TaskName: "before", Start: from.Add(-2 * time.Hour), End: from.Add(-1 * time.Hour)},
		// Running since 5 min before `to`: counts 5 min.
		{TaskName: "running", Start: now.Add(-5 * time.Minute)},
	} {
		if err := st.Entries.Put(e); err != nil {
			t.Fatalf("put entry: %v", err)
		}
	}

	rows, err := h.Stats(from, now, "")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	got := map[string]time.Duration{}
	for _, r := range rows {
		got[r.Task.Name] = r.Total
	}
	want := map[string]time.Duration{
		"inside":   10 * time.Minute,
		"straddle": 15 * time.Minute,
		"running":  5 * time.Minute,
	}
	if len(got) != len(want) {
		t.Fatalf("got rows %v, want %v", got, want)
	}
	for name, d := range want {
		if got[name] != d {
			t.Errorf("%s = %s, want %s", name, got[name], d)
		}
	}

	// Sorted largest first.
	if rows[0].Task.Name != "straddle" || rows[2].Task.Name != "running" {
		t.Errorf("rows not sorted by time desc: %v, %v, %v",
			rows[0].Task.Name, rows[1].Task.Name, rows[2].Task.Name)
	}

	// Project filter.
	workRows, err := h.Stats(from, now, "work")
	if err != nil {
		t.Fatalf("stats work: %v", err)
	}
	if len(workRows) != 2 {
		t.Fatalf("expected 2 work rows, got %v", workRows)
	}
}
