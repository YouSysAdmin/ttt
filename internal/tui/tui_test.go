package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"ttt/internal/core/env"
	"ttt/internal/database/boltkv"
	"ttt/internal/domain/notes"
	"ttt/internal/domain/store"
	"ttt/internal/domain/tasks"
	"ttt/internal/domain/tracker"
)

func newTestModel(t *testing.T) *model {
	t.Helper()
	kv, err := boltkv.Open(filepath.Join(t.TempDir(), "ttt.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = kv.Close() })

	rt := &env.Runtime{}
	st := &store.Store{}
	boltkv.BindProvider(rt, st, kv)
	return newModel(Deps{
		Tasks:   &tasks.Handler{Runtime: rt, Store: st},
		Tracker: &tracker.Handler{Runtime: rt, Store: st},
		Notes:   &notes.Handler{Runtime: rt, Store: st},
	})
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// press sends each key and returns the resulting model.
func press(t *testing.T, m *model, keys ...string) *model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(key(k))
		m = next.(*model)
	}
	return m
}

// typeText enters text through the focused input.
func typeText(t *testing.T, m *model, text string) *model {
	t.Helper()
	for _, r := range text {
		m = press(t, m, string(r))
	}
	return m
}

func TestAddStartPauseFlow(t *testing.T) {
	m := newTestModel(t)
	if !strings.Contains(m.View(), "Not tracking") {
		t.Fatalf("expected idle status, got:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "No current tasks") {
		t.Fatalf("expected empty list hint, got:\n%s", m.View())
	}

	// a -> type name -> enter: task appears.
	m = press(t, m, "a")
	if m.mode != modeEdit || !m.editCreate {
		t.Fatal("expected create form after 'a'")
	}
	m = typeText(t, m, "feat")
	m = press(t, m, "enter")
	if m.mode != modeList || !strings.Contains(m.View(), "feat") {
		t.Fatalf("task not added, view:\n%s", m.View())
	}

	// s: start the selected task. The timer panel shows it.
	m = press(t, m, "s")
	if m.status == nil || m.status.State == nil || m.status.State.TaskName != "feat" {
		t.Fatalf("expected tracking feat, got %+v", m.status)
	}
	if strings.Contains(m.View(), "Not tracking") {
		t.Fatalf("timer panel still idle:\n%s", m.View())
	}
	// s again on the same running task: error flash, still tracking.
	m = press(t, m, "s")
	if !m.flashErr || !strings.Contains(m.flash, "already") {
		t.Fatalf("expected already-running flash, got %q (err=%v)", m.flash, m.flashErr)
	}

	// p: pause.
	m = press(t, m, "p")
	if !strings.Contains(m.View(), "Not tracking") || !strings.Contains(m.View(), "paused") {
		t.Fatalf("expected paused state, view:\n%s", m.View())
	}

	// x with nothing running: error flash.
	m = press(t, m, "x")
	if !m.flashErr {
		t.Fatalf("expected error flash for stop-when-idle, got %q", m.flash)
	}
}

func TestInfoAndStatsPanels(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter")

	// n -> type note -> enter: the info panel shows it immediately.
	m = press(t, m, "n")
	m = typeText(t, m, "PR link")
	m = press(t, m, "enter")
	if m.flashErr {
		t.Fatalf("note failed: %q", m.flash)
	}
	v := m.View()
	for _, want := range []string{"Info", "Created:", "PR link", "Stats (this month)", "No tracked time"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}

	// Track some time: the stats panel gets a bar row and a total.
	m = press(t, m, "s", "p")
	v = m.View()
	if !strings.Contains(v, "TOTAL") {
		t.Fatalf("stats panel missing TOTAL after tracking:\n%s", v)
	}
	if !strings.Contains(v, "%") {
		t.Fatalf("stats panel missing percentage column:\n%s", v)
	}

	// Cursor move updates the info panel to the newly selected task.
	m = press(t, m, "a")
	m = typeText(t, m, "another")
	m = press(t, m, "enter", "down")
	if m.detail == nil || m.detail.Task.Name != m.rows[m.cursor].Task.Name {
		t.Fatalf("info panel not following cursor: detail=%v cursor=%d", m.detail, m.cursor)
	}
}

func TestEditModal(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter")

	// e opens the modal prefilled with the task name.
	m = press(t, m, "e")
	if m.mode != modeEdit {
		t.Fatal("expected edit mode")
	}
	if got := m.editInputs[editName].Value(); got != "feat" {
		t.Fatalf("name not prefilled: %q", got)
	}
	if !strings.Contains(m.View(), "Edit task") {
		t.Fatalf("edit modal not rendered:\n%s", m.View())
	}

	// tab to description, type, save.
	m = press(t, m, "tab")
	m = typeText(t, m, "new desc")
	m = press(t, m, "enter")
	if m.mode != modeList {
		t.Fatalf("expected list mode after save, flash=%q", m.flash)
	}
	if m.detail.Task.Description != "new desc" {
		t.Fatalf("description not saved: %+v", m.detail.Task)
	}

	// Rename through the modal.
	m = press(t, m, "e")
	m.editInputs[editName].SetValue("feature-x")
	m = press(t, m, "enter")
	if m.rows[m.cursor].Task.Name != "feature-x" {
		t.Fatalf("rename not applied: %+v", m.rows[m.cursor].Task)
	}

	// esc cancels without saving.
	m = press(t, m, "e", "tab")
	m = typeText(t, m, " EDITED")
	m = press(t, m, "esc")
	if m.detail.Task.Description != "new desc" {
		t.Fatalf("esc must not save, got %q", m.detail.Task.Description)
	}
}

func TestDeleteTaskWithConfirm(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "doomed")
	m = press(t, m, "enter")

	// d -> n: cancelled, task stays.
	m = press(t, m, "d")
	if m.mode != modeConfirm || !strings.Contains(m.View(), "Delete task") {
		t.Fatalf("expected confirm modal, view:\n%s", m.View())
	}
	m = press(t, m, "n")
	if len(m.rows) != 1 {
		t.Fatal("task deleted despite cancel")
	}

	// d -> y: gone.
	m = press(t, m, "d", "y")
	if len(m.rows) != 0 {
		t.Fatalf("task not deleted: %+v", m.rows)
	}
}

func TestNotesModeDelete(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter")
	for _, text := range []string{"first", "second"} {
		m = press(t, m, "n")
		m = typeText(t, m, text)
		m = press(t, m, "enter")
	}

	// v enters notes mode where the newest note ("second") is selected first.
	m = press(t, m, "v")
	if m.mode != modeNotes {
		t.Fatal("expected notes mode")
	}
	m = press(t, m, "d")
	if m.mode != modeConfirm {
		t.Fatal("expected confirm modal for note delete")
	}
	m = press(t, m, "y")
	if got := m.displayNotes(); len(got) != 1 || got[0].Text != "first" {
		t.Fatalf("expected only 'first' left, got %+v", got)
	}
	if m.mode != modeNotes {
		t.Fatal("should stay in notes mode while notes remain")
	}
	m = press(t, m, "esc")
	if m.mode != modeList {
		t.Fatal("esc must return to list")
	}
}

func TestNotesModeEdit(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter", "n")
	m = typeText(t, m, "draft")
	m = press(t, m, "enter")

	// v -> e: input prefilled with the note text.
	m = press(t, m, "v", "e")
	if m.mode != modeInput || m.inputKind != inputEditNote {
		t.Fatalf("expected note-edit input, mode=%v kind=%v", m.mode, m.inputKind)
	}
	if m.input.Value() != "draft" {
		t.Fatalf("input not prefilled: %q", m.input.Value())
	}
	before := m.displayNotes()[0].CreatedAt

	m = typeText(t, m, " v2")
	m = press(t, m, "enter")
	if m.mode != modeNotes {
		t.Fatalf("expected to return to notes mode, got %v (flash %q)", m.mode, m.flash)
	}
	got := m.displayNotes()
	if len(got) != 1 || got[0].Text != "draft v2" {
		t.Fatalf("note not edited: %+v", got)
	}
	if !got[0].CreatedAt.Equal(before) {
		t.Fatalf("edit must keep the timestamp: %v != %v", got[0].CreatedAt, before)
	}

	// esc from note edit returns to notes mode, unchanged.
	m = press(t, m, "e")
	m = typeText(t, m, " DISCARDED")
	m = press(t, m, "esc")
	if m.mode != modeNotes || m.displayNotes()[0].Text != "draft v2" {
		t.Fatalf("esc must discard, got %q", m.displayNotes()[0].Text)
	}
}

func TestModalOverlaysInterface(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter", "e")

	// The modal and the panels behind it are both visible.
	v := m.View()
	for _, want := range []string{"Edit task", "Tasks", "Info"} {
		if !strings.Contains(v, want) {
			t.Fatalf("overlay view missing %q:\n%s", want, v)
		}
	}

	m = press(t, m, "esc", "d")
	v = m.View()
	for _, want := range []string{"Delete task", "Tasks"} {
		if !strings.Contains(v, want) {
			t.Fatalf("confirm overlay missing %q:\n%s", want, v)
		}
	}
}

func TestEnterTogglesTracking(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter") // submits the add-modal

	// enter on the selected task starts tracking.
	m = press(t, m, "enter")
	if m.status == nil || m.status.State == nil || m.status.State.TaskName != "feat" {
		t.Fatalf("enter did not start tracking: %+v (flash %q)", m.status, m.flash)
	}
	// enter again pauses it.
	m = press(t, m, "enter")
	if m.status.State != nil {
		t.Fatalf("enter did not pause: %+v", m.status.State)
	}
	if m.detail.Task.Status != "paused" {
		t.Fatalf("task not paused: %+v", m.detail.Task)
	}
	// The toggle is documented.
	if !strings.Contains(m.View(), "enter start/pause") {
		t.Fatalf("help missing enter toggle:\n%s", m.View())
	}
}

func TestTaskFilter(t *testing.T) {
	m := newTestModel(t)

	// Adding a task switches to the "new" filter and shows it.
	m = press(t, m, "a")
	m = typeText(t, m, "fresh")
	m = press(t, m, "enter")
	if m.filter != filterNew || !strings.Contains(m.View(), "Tasks (new)") {
		t.Fatalf("expected new filter after add, got %v:\n%s", m.filter, m.View())
	}
	if len(m.rows) != 1 || m.rows[0].Task.Name != "fresh" {
		t.Fatalf("added task not visible: %+v", m.rows)
	}

	// Starting it follows it into "current".
	m = press(t, m, "s")
	if m.filter != filterCurrent || !strings.Contains(m.View(), "Tasks (current)") {
		t.Fatalf("expected current filter after start, got %v", m.filter)
	}
	if len(m.rows) != 1 || m.rows[m.cursor].Task.Name != "fresh" {
		t.Fatalf("started task not selected: %+v", m.rows)
	}

	// Stopping moves it to done: current view empties, done view shows it.
	m = press(t, m, "x")
	if len(m.rows) != 0 {
		t.Fatalf("done task still in current view: %+v", m.rows)
	}
	m = press(t, m, "f") // -> new (empty)
	if m.filter != filterNew || len(m.rows) != 0 {
		t.Fatalf("expected empty new view, got %v %+v", m.filter, m.rows)
	}
	m = press(t, m, "f") // -> done
	if m.filter != filterDone || len(m.rows) != 1 {
		t.Fatalf("expected done view with the task, got %v %+v", m.filter, m.rows)
	}
	m = press(t, m, "f") // -> wraps to current
	if m.filter != filterCurrent {
		t.Fatalf("expected wrap to current, got %v", m.filter)
	}
}

func TestInputIsModal(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	v := m.View()
	// The input modal and the panels behind it are both visible.
	for _, want := range []string{"New task", "Tasks (current)", "Info"} {
		if !strings.Contains(v, want) {
			t.Fatalf("input modal view missing %q:\n%s", want, v)
		}
	}
}

func TestStatsPeriodCycle(t *testing.T) {
	m := newTestModel(t)
	if m.period != periodMonth || !strings.Contains(m.View(), "Stats (this month)") {
		t.Fatalf("default period should be this month:\n%s", m.View())
	}
	m = press(t, m, "t")
	if m.period != periodToday || !strings.Contains(m.View(), "Stats (today)") {
		t.Fatal("expected today after first cycle")
	}
	m = press(t, m, "t")
	if m.period != periodWeek || !strings.Contains(m.View(), "Stats (this week)") {
		t.Fatal("expected this week after second cycle")
	}
	m = press(t, m, "t")
	if m.period != periodMonth {
		t.Fatal("expected wrap back to this month")
	}
}

func TestUpdateBanner(t *testing.T) {
	m := newTestModel(t)

	// No check result: no banner.
	if strings.Contains(m.View(), "Update available") {
		t.Fatalf("banner rendered before any check result:\n%s", m.View())
	}

	// A newer release: the banner appears in the flash slot.
	next, _ := m.Update(updateCheckMsg{latestVersion: "9.9.9"})
	m = next.(*model)
	if !strings.Contains(m.View(), "Update available: v9.9.9") || !strings.Contains(m.View(), "ttt update") {
		t.Fatalf("expected update banner, view:\n%s", m.View())
	}

	// Transient flashes win over the banner, then it returns.
	m.setFlash("saved", false)
	if strings.Contains(m.View(), "Update available") {
		t.Fatalf("flash must override the banner:\n%s", m.View())
	}
	m.setFlash("", false)
	if !strings.Contains(m.View(), "Update available") {
		t.Fatalf("banner must return once the flash clears:\n%s", m.View())
	}

	// Up-to-date (empty) result: no banner.
	m2 := newTestModel(t)
	next, _ = m2.Update(updateCheckMsg{})
	m2 = next.(*model)
	if strings.Contains(m2.View(), "Update available") {
		t.Fatalf("banner rendered for empty check result:\n%s", m2.View())
	}

	// --no-update-check: the check resolves empty without touching the
	// network, even for a release build.
	m3 := newTestModel(t)
	m3.deps.Version = "0.0.1"
	m3.deps.NoUpdateCheck = true
	if msg := m3.checkUpdate(); msg.(updateCheckMsg).latestVersion != "" {
		t.Fatalf("disabled check must return an empty result, got %+v", msg)
	}
}

func TestDescriptionWrapsInsteadOfCropping(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(*model)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "tab")
	url := "https://fariaedu.atlassian.net/browse/OA-31337"
	m = typeText(t, m, "Needs to disable rev checking during deploy with maintenance enabled "+url)
	m = press(t, m, "enter")

	// The URL must wrap to a new line unbroken — a split URL defeats terminal
	// link detection — and the fixed fields must still render alongside it.
	v := m.View()
	for _, want := range []string{url, "Created:", "Total:"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}

	// On a short terminal the Description section is capped with an ellipsis
	// instead of overflowing the panel (at height 20 exactly one content line
	// fits, so the first wrapped line gains a "…").
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = next.(*model)
	v = m.View()
	for _, want := range []string{"Description:", "maintenance…", "Created:", "Total:"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q with capped description:\n%s", want, v)
		}
	}

	// When there is no room even for the section title, it is dropped whole
	// rather than half-rendered.
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
	m = next.(*model)
	if strings.Contains(m.View(), "Description:") {
		t.Fatalf("description should be dropped on a too-short panel:\n%s", m.View())
	}
}

func TestNotesScrollFollowsCursor(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = next.(*model)
	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter")
	for i := 0; i < 20; i++ {
		m = press(t, m, "n")
		m = typeText(t, m, fmt.Sprintf("note-%02d", i))
		m = press(t, m, "enter")
	}

	// More notes than fit: a scrollbar thumb appears, newest visible,
	// oldest not.
	v := m.View()
	if !strings.Contains(v, "█") {
		t.Fatalf("expected a scrollbar, got:\n%s", v)
	}
	if !strings.Contains(v, "note-19") || strings.Contains(v, "note-00") {
		t.Fatalf("expected newest visible and oldest hidden:\n%s", v)
	}

	// Scrolling to the bottom of the list brings the oldest into view.
	m = press(t, m, "v")
	for i := 0; i < 19; i++ {
		m = press(t, m, "j")
	}
	v = m.View()
	if !strings.Contains(v, "note-00") {
		t.Fatalf("cursor at oldest note but not visible:\n%s", v)
	}
	if sel := m.displayNotes()[m.noteCursor]; sel.Text != "note-00" {
		t.Fatalf("expected cursor on oldest note, got %q", sel.Text)
	}
}

func TestTaskListScrolls(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 16})
	m = next.(*model)
	for i := 0; i < 30; i++ {
		m = press(t, m, "a")
		m = typeText(t, m, fmt.Sprintf("task-%02d", i))
		m = press(t, m, "enter")
	}
	v := m.View()
	if !strings.Contains(v, "█") {
		t.Fatalf("expected task list scrollbar:\n%s", v)
	}
	// Walk to the last task. It must be visible and selected.
	for i := 0; i < 29; i++ {
		m = press(t, m, "j")
	}
	if !strings.Contains(m.View(), "task-29") {
		t.Fatalf("cursor at last task but not visible:\n%s", m.View())
	}
	if m.rows[m.cursor].Task.Name != "task-29" {
		t.Fatalf("expected cursor on task-29, got %q", m.rows[m.cursor].Task.Name)
	}
}

func TestCustomRangePicker(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(*model)

	m = press(t, m, "T")
	if m.mode != modeRange {
		t.Fatal("expected range mode")
	}
	// Without SelectDate the pickers render no date highlight at all.
	if !m.rangeFrom.Selected || !m.rangeTo.Selected {
		t.Fatal("pickers must have their date selected for the highlight to render")
	}
	v := m.View()
	for _, want := range []string{"Stats period", "From", "To"} {
		if !strings.Contains(v, want) {
			t.Fatalf("range modal missing %q:\n%s", want, v)
		}
	}

	// Move the From date back a week, then apply.
	m = press(t, m, "up") // calendar: previous week
	m = press(t, m, "enter")
	if m.mode != modeList || !m.customRange {
		t.Fatalf("range not applied: mode=%v custom=%v flash=%q", m.mode, m.customRange, m.flash)
	}
	if !m.customFrom.Before(m.customTo) && !m.customFrom.Equal(m.customTo) {
		t.Fatalf("bad range: %v .. %v", m.customFrom, m.customTo)
	}
	if !strings.Contains(m.View(), "Stats ("+m.customFrom.Format("2006-01-02")) {
		t.Fatalf("stats title missing custom range:\n%s", m.View())
	}

	// 't' drops the custom range and returns to presets.
	m = press(t, m, "t")
	if m.customRange || !strings.Contains(m.View(), "Stats (this month)") {
		t.Fatalf("expected preset period back, custom=%v", m.customRange)
	}

	// A reversed range is rejected.
	m = press(t, m, "T", " ") // focus To
	for i := 0; i < 5; i++ {
		m = press(t, m, "up") // move To weeks back, before From
	}
	m = press(t, m, "enter")
	if m.mode != modeRange || !m.flashErr {
		t.Fatalf("reversed range must be rejected, mode=%v flash=%q", m.mode, m.flash)
	}
}

func TestTimerPanel(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
	m = next.(*model)

	if !strings.Contains(m.View(), "Current task time") || !strings.Contains(m.View(), "Not tracking") {
		t.Fatalf("expected idle timer panel:\n%s", m.View())
	}

	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "enter", "s")
	// The seven-segment font renders block cells once tracking.
	if !strings.Contains(m.View(), "██") {
		t.Fatalf("expected big timer digits:\n%s", m.View())
	}
}

func TestAddFormAllFields(t *testing.T) {
	m := newTestModel(t)
	repo := t.TempDir() // repo path must exist

	m = press(t, m, "a")
	m = typeText(t, m, "feat")
	m = press(t, m, "tab")
	m = typeText(t, m, "the description")
	m = press(t, m, "tab")
	m = typeText(t, m, "acme")
	m = press(t, m, "tab")
	m = typeText(t, m, repo)
	m = press(t, m, "enter")
	if m.mode != modeList {
		t.Fatalf("form not submitted, flash=%q", m.flash)
	}

	got := m.detail.Task
	if got.Name != "feat" || got.Description != "the description" || got.Project != "acme" || got.Repo != repo {
		t.Fatalf("task fields wrong: %+v", got)
	}
	if !strings.Contains(m.View(), infoLabel("Project")+"acme") {
		t.Fatalf("info panel missing project:\n%s", m.View())
	}
}

func TestTimerShowsTaskTotalOnResume(t *testing.T) {
	m := newTestModel(t)

	// Seed a task with an hour already tracked, then resume it.
	past := time.Now().Add(-2 * time.Hour)
	if _, _, _, err := m.deps.Tracker.Start("feat", tracker.StartOpts{}, past); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := m.deps.Tracker.Pause(past.Add(time.Hour)); err != nil {
		t.Fatalf("pause: %v", err)
	}
	m.refresh()
	m = press(t, m, "enter") // resume the paused task

	// The timer continues from the accumulated hour, not from zero.
	if m.timerTotal < time.Hour {
		t.Fatalf("timer restarted from zero: %v", m.timerTotal)
	}
}

func TestBigTime(t *testing.T) {
	rows := bigTime("01:23")
	if len(rows) != bigTimeRows {
		t.Fatalf("expected %d rows, got %d", bigTimeRows, len(rows))
	}
	w := bigTimeWidth("01:23")
	for i, r := range rows {
		if got := len([]rune(r)); got != w {
			t.Fatalf("row %d width %d, want %d: %q", i, got, w, r)
		}
	}
}

func TestInputEscCancels(t *testing.T) {
	m := newTestModel(t)
	m = press(t, m, "a")
	m = typeText(t, m, "abandoned")
	m = press(t, m, "esc")
	if m.mode != modeList {
		t.Fatal("expected list mode after esc")
	}
	if strings.Contains(m.View(), "abandoned") {
		t.Fatalf("cancelled task must not exist, view:\n%s", m.View())
	}
}
