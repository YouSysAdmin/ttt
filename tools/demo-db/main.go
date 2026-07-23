// Command demo generates a populated demo database for trying out ttt:
//
//	go run ./tools/demo [path]
//	ttt --db demo.db tui
package main

import (
	"fmt"
	"os"
	"time"

	"ttt/internal/core/env"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/store"
	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
)

func main() {
	path := "demo.db"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	_ = os.Remove(path)

	kv, err := boltkv.Open(path)
	if err != nil {
		fatal(err)
	}
	defer kv.Close()

	rt := &env.Runtime{}
	st := &store.Store{}
	boltkv.BindProvider(rt, st, kv)

	now := time.Now()
	day := func(daysAgo int, hour, min int) time.Time {
		d := now.AddDate(0, 0, -daysAgo)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, min, 0, 0, d.Location())
	}

	type session struct {
		start time.Time
		dur   time.Duration
	}
	seed := []struct {
		task     task.Task
		sessions []session
		notes    []note.Note
	}{
		{
			task: task.Task{Name: "api-gateway", Project: "work", Status: task.StatusDone,
				Description: "New API gateway rollout"},
			sessions: []session{
				{day(21, 9, 30), 2 * time.Hour},
				{day(20, 10, 0), 3*time.Hour + 15*time.Minute},
				{day(14, 14, 0), 90 * time.Minute},
				{day(13, 9, 15), 2*time.Hour + 45*time.Minute},
			},
			notes: []note.Note{
				{CreatedAt: day(21, 11, 10), Text: "3fa2b1c Add gateway skeleton"},
				{CreatedAt: day(14, 15, 20), Text: "PR: https://github.com/work/gateway/pull/128"},
				{CreatedAt: day(13, 11, 55), Text: "b7e9d02 Handle retries with backoff"},
			},
		},
		{
			task: task.Task{Name: "billing-service", Project: "work", Status: task.StatusPaused,
				Description: "Usage-based billing"},
			sessions: []session{
				{day(3, 11, 0), 80 * time.Minute},
				{day(2, 9, 40), 2*time.Hour + 5*time.Minute},
				{day(1, 16, 20), 45 * time.Minute},
			},
			notes: []note.Note{
				{CreatedAt: day(2, 11, 30), Text: "PR: https://github.com/work/billing/pull/47"},
				{CreatedAt: day(1, 17, 0), Text: "double-check proration edge cases"},
			},
		},
		{
			task: task.Task{Name: "code-review", Project: "work", Status: task.StatusPaused,
				Description: "Daily review rotation"},
			sessions: []session{
				{day(6, 9, 0), 30 * time.Minute},
				{day(5, 9, 0), 25 * time.Minute},
				{day(4, 9, 0), 40 * time.Minute},
				{day(1, 9, 0), 35 * time.Minute},
			},
		},
		{
			task: task.Task{Name: "ttt", Project: "opensource", Status: task.StatusActive,
				Description: "This tool :)"},
			sessions: []session{
				{day(5, 20, 0), time.Hour},
				{day(2, 21, 15), 2 * time.Hour},
			},
			notes: []note.Note{
				{CreatedAt: day(2, 22, 0), Text: "0c1f3aa Add stats bars"},
				{CreatedAt: day(0, 0, 1), Text: "idea: export to CSV"},
			},
		},
		{
			task: task.Task{Name: "blog-post", Project: "personal", Status: task.StatusPaused,
				Description: "Write about terminal UIs"},
			sessions: []session{
				{day(8, 19, 0), 50 * time.Minute},
				{day(1, 20, 30), 35 * time.Minute},
			},
			notes: []note.Note{
				{CreatedAt: day(1, 21, 10), Text: "draft outline done"},
			},
		},
		{
			task: task.Task{Name: "house-chores", Project: "personal", Status: task.StatusDone},
			sessions: []session{
				{day(10, 12, 0), 40 * time.Minute},
			},
		},
		{
			task: task.Task{Name: "learn-rust", Project: "personal", Status: task.StatusTodo,
				Description: "Evenings project"},
			notes: []note.Note{
				{CreatedAt: day(4, 22, 0), Text: "start with the book, ch. 4"},
			},
		},
	}

	for _, s := range seed {
		t := s.task
		if len(s.sessions) > 0 && t.Status == task.StatusDone {
			last := s.sessions[len(s.sessions)-1]
			t.CompletedAt = last.start.Add(last.dur)
		}
		if err := st.Tasks.Upsert(&t); err != nil {
			fatal(err)
		}
		// Backdate CreatedAt to the first session (the store stamps it on
		// first write, and a second upsert keeps the caller's value).
		if len(s.sessions) > 0 {
			t.CreatedAt = s.sessions[0].start.Add(-10 * time.Minute)
			if err := st.Tasks.Upsert(&t); err != nil {
				fatal(err)
			}
		}
		for _, ses := range s.sessions {
			e := &entry.Entry{TaskName: t.Name, Start: ses.start, End: ses.start.Add(ses.dur)}
			if err := st.Entries.Put(e); err != nil {
				fatal(err)
			}
		}
		for _, n := range s.notes {
			n.TaskName = t.Name
			if err := st.Notes.Add(&n); err != nil {
				fatal(err)
			}
		}
	}

	// Leave "ttt" actively tracking, started 24 minutes ago, so the TUI timer ticks live.
	running := task.Task{Name: "ttt", Project: "opensource", Status: task.StatusActive,
		Description: "This tool :)"}
	if _, err := st.Tracker.Start(&running, now.Add(-24*time.Minute)); err != nil {
		fatal(err)
	}

	fmt.Printf("demo database written to %s\n", path)
	fmt.Printf("try it:  ttt --db %s tui\n", path)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
