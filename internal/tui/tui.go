// Package tui is the Bubble Tea interface over the domain handlers: a live
// task list with start/pause/stop, add/note inputs, and a detail view. It
// holds no domain logic - every action delegates to the same handlers the
// CLI commands use.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	datepicker "github.com/ethanefung/bubble-datepicker"

	"ttt/internal/core/update"
	"ttt/internal/domain/notes"
	"ttt/internal/domain/tasks"
	"ttt/internal/domain/tracker"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
	trackermodel "ttt/internal/models/tracker"
)

// Deps are the handlers the TUI drives.
type Deps struct {
	Tasks         *tasks.Handler
	Tracker       *tracker.Handler
	Notes         *notes.Handler
	Version       string // running build, for the startup update check
	NoUpdateCheck bool   // --no-update-check: skip the startup check entirely
}

// Run starts the TUI and blocks until the user quits.
func Run(d Deps) error {
	_, err := tea.NewProgram(newModel(d), tea.WithAltScreen()).Run()
	return err
}

type mode int

const (
	modeList mode = iota
	modeInput
	modeEdit    // edit-task modal
	modeConfirm // yes/no modal guarding deletions
	modeNotes   // note selection inside the info panel
	modeRange   // from/to date pickers for a custom stats period
)

type inputKind int

const (
	inputNote     inputKind = iota
	inputEditNote           // edits editNote's text and returns to modeNotes
)

// taskFilter selects which tasks the list shows. Cycled with 'f'.
type taskFilter int

const (
	filterCurrent taskFilter = iota // started or paused (default)
	filterNew                       // not started yet
	filterDone                      // closed
)

func (f taskFilter) label() string {
	switch f {
	case filterNew:
		return "new"
	case filterDone:
		return "done"
	default:
		return "current"
	}
}

func (f taskFilter) matches(s task.Status) bool {
	switch f {
	case filterNew:
		return s == task.StatusTodo
	case filterDone:
		return s == task.StatusDone
	default:
		return s == task.StatusActive || s == task.StatusPaused
	}
}

// statPeriod selects the stats panel window. Cycled with 't'.
type statPeriod int

const (
	periodMonth statPeriod = iota // this month (default)
	periodToday
	periodWeek
)

func (p statPeriod) label() string {
	switch p {
	case periodToday:
		return "today"
	case periodWeek:
		return "this week"
	default:
		return "this month"
	}
}

// start returns the period's calendar start: midnight today, Monday 00:00,
// or the 1st of the month 00:00.
func (p statPeriod) start(now time.Time) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch p {
	case periodToday:
		return midnight
	case periodWeek:
		return midnight.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
	default:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type model struct {
	deps Deps

	mode      mode
	rows      []*tasks.Row // tasks matching `filter`
	status    *tracker.Status
	cursor    int
	detail    *tasks.Details   // info panel: the selected task
	stats     []*tasks.StatRow // stats panel, over `period` or the custom range
	period    statPeriod
	filter    taskFilter
	input     textinput.Model
	inputKind inputKind

	// custom stats range, set through the date-picker modal ('T').
	customRange          bool
	customFrom, customTo time.Time // customTo is the inclusive last day
	rangeFrom, rangeTo   datepicker.Model
	rangeFocusTo         bool

	// task-form modal: name, description, project, repo inputs. editCreate
	// distinguishes add (true) from edit.
	editInputs []textinput.Model
	editFocus  int
	editCreate bool

	timerTotal time.Duration // running task's all-time total, for the Timer panel

	// confirm modal: what to ask and what to run on "y".
	confirmPrompt string
	confirmAction func() error

	noteCursor int        // modeNotes: index into displayNotes()
	editNote   *note.Note // target of inputEditNote

	flash    string // one-line feedback: last action result or error
	flashErr bool
	width    int
	height   int

	updateVersion string // newer release found by the startup check
}

func newModel(d Deps) *model {
	ti := textinput.New()
	ti.CharLimit = 256
	m := &model{deps: d, input: ti}
	m.refresh()
	return m
}

// refresh reloads everything the panels render: task rows, tracking status,
// the selected task's details, and the one-month stats.
func (m *model) refresh() {
	now := time.Now()
	if rows, err := m.deps.Tasks.List(now, ""); err == nil {
		m.rows = m.rows[:0]
		for _, r := range rows {
			if m.filter.matches(r.Task.Status) {
				m.rows = append(m.rows, r)
			}
		}
	} else {
		m.setFlash(err.Error(), true)
	}
	if st, err := m.deps.Tracker.Status(now); err == nil {
		m.status = st
	} else {
		m.setFlash(err.Error(), true)
	}
	// The Timer panel shows the running task's all-time total, so resuming a
	// task continues from its accumulated time instead of restarting at zero.
	m.timerTotal = 0
	if m.status != nil {
		m.timerTotal = m.status.Total
	}
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
	m.loadDetail()
	from, to := m.statsWindow(now)
	if stats, err := m.deps.Tasks.Stats(from, to, ""); err == nil {
		m.stats = stats
	}
}

// statsWindow is the stats period: the preset's calendar start..now, or the
// custom range (whole days, capped at now so running entries don't count
// future time).
func (m *model) statsWindow(now time.Time) (time.Time, time.Time) {
	if !m.customRange {
		return m.period.start(now), now
	}
	to := m.customTo.AddDate(0, 0, 1)
	if to.After(now) {
		to = now
	}
	return m.customFrom, to
}

// displayNotes returns the selected task's notes newest-first - the order
// the info panel renders them in and modeNotes' cursor indexes.
func (m *model) displayNotes() []*note.Note {
	if m.detail == nil {
		return nil
	}
	out := make([]*note.Note, len(m.detail.Notes))
	for i, n := range m.detail.Notes {
		out[len(out)-1-i] = n
	}
	return out
}

// loadDetail fills the info panel with the selected task.
func (m *model) loadDetail() {
	sel := m.selected()
	if sel == nil {
		m.detail = nil
		return
	}
	if d, err := m.deps.Tasks.Show(sel.Task.Name, time.Now()); err == nil {
		m.detail = d
	}
}

func (m *model) setFlash(text string, isErr bool) {
	m.flash, m.flashErr = text, isErr
}

func (m *model) selected() *tasks.Row {
	if len(m.rows) == 0 {
		return nil
	}
	return m.rows[m.cursor]
}

func (m *model) Init() tea.Cmd {
	if m.deps.NoUpdateCheck {
		return tick()
	}
	return tea.Batch(tick(), m.checkUpdate)
}

// checkUpdate asks GitHub for a newer release once at startup. It runs as a
// tea command (own goroutine), so the UI never waits on the network; failures
// and dev builds resolve to an empty message.
func (m *model) checkUpdate() tea.Msg {
	if m.deps.NoUpdateCheck {
		return updateCheckMsg{}
	}
	res := update.CheckLatestVersion(m.deps.Version)
	if res.Err != nil {
		return updateCheckMsg{}
	}
	return updateCheckMsg{latestVersion: res.LatestVersion}
}

type updateCheckMsg struct{ latestVersion string }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case updateCheckMsg:
		m.updateVersion = msg.latestVersion
		return m, nil
	case tickMsg:
		if m.mode == modeList {
			m.refresh()
		}
		return m, tick()
	case tea.KeyMsg:
		switch m.mode {
		case modeInput:
			return m.updateInput(msg)
		case modeEdit:
			return m.updateEdit(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeNotes:
			return m.updateNotes(msg)
		case modeRange:
			return m.updateRange(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m *model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.setFlash("", false)
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.loadDetail()
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.loadDetail()
		}
	case "s":
		m.startSelected()
	case "enter":
		// Toggle: start the selected task, or pause it when it's the one
		// already running.
		if sel := m.selected(); sel != nil {
			if m.status != nil && m.status.State != nil && m.status.State.TaskName == sel.Task.Name {
				m.closeRunning(false)
			} else {
				m.startSelected()
			}
		}
	case "p":
		m.closeRunning(false)
	case "x":
		m.closeRunning(true)
	case "a":
		m.openAddForm()
	case "n":
		if m.selected() != nil {
			m.openInput(inputNote, fmt.Sprintf("note for %q", m.selected().Task.Name))
		}
	case "e":
		m.openEdit()
	case "d":
		if sel := m.selected(); sel != nil {
			name := sel.Task.Name
			m.openConfirm(fmt.Sprintf("Delete task %q with all its entries and notes?", name), func() error {
				if err := m.deps.Tasks.Delete(name); err != nil {
					return err
				}
				m.setFlash(fmt.Sprintf("deleted task %q", name), false)
				return nil
			})
		}
	case "v":
		if m.detail != nil && len(m.detail.Notes) > 0 {
			m.noteCursor = 0
			m.mode = modeNotes
		}
	case "t":
		// Leaving a custom range resumes the preset cycle where it was.
		if m.customRange {
			m.customRange = false
		} else {
			m.period = (m.period + 1) % 3
		}
		m.refresh()
	case "T":
		m.openRange()
	case "f":
		m.filter = (m.filter + 1) % 3
		m.cursor = 0
		m.refresh()
	}
	return m, nil
}

// selectTask points the cursor at name, if visible under the current filter.
func (m *model) selectTask(name string) {
	for i, r := range m.rows {
		if r.Task.Name == name {
			m.cursor = i
			m.loadDetail()
			return
		}
	}
}

// updateNotes handles the note-selection mode inside the info panel.
func (m *model) updateNotes(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	notes := m.displayNotes()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc":
		m.mode = modeList
	case "up", "k":
		if m.noteCursor > 0 {
			m.noteCursor--
		}
	case "down", "j":
		if m.noteCursor < len(notes)-1 {
			m.noteCursor++
		}
	case "e":
		if m.noteCursor < len(notes) {
			m.editNote = notes[m.noteCursor]
			m.openInput(inputEditNote, "note text")
			m.input.SetValue(m.editNote.Text)
			m.input.CursorEnd()
		}
	case "d", "x":
		if m.noteCursor < len(notes) {
			n := notes[m.noteCursor]
			m.openConfirm(fmt.Sprintf("Delete note %q?", clip(n.Text, 40)), func() error {
				if err := m.deps.Notes.Delete(n); err != nil {
					return err
				}
				m.setFlash("note deleted", false)
				// Stay in notes mode while there is something to select.
				m.loadDetail()
				if len(m.displayNotes()) > 0 {
					m.mode = modeNotes
					m.noteCursor = min(m.noteCursor, len(m.displayNotes())-1)
				}
				return nil
			})
		}
	}
	return m, nil
}

func (m *model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = m.inputReturnMode()
		return m, nil
	case "enter":
		m.submitInput()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// inputReturnMode is where closing the input line leads back to.
func (m *model) inputReturnMode() mode {
	if m.inputKind == inputEditNote {
		return modeNotes
	}
	return modeList
}

func (m *model) openInput(kind inputKind, placeholder string) {
	m.inputKind = kind
	m.input.Placeholder = placeholder
	m.input.SetValue("")
	m.input.Width = m.modalWidth() - 8
	m.input.Focus()
	m.mode = modeInput
}

func (m *model) submitInput() {
	value := strings.TrimSpace(m.input.Value())
	m.mode = m.inputReturnMode()
	if value == "" {
		return
	}
	switch m.inputKind {
	case inputEditNote:
		if m.editNote == nil {
			return
		}
		if err := m.deps.Notes.Edit(m.editNote, value); err != nil {
			m.setFlash(err.Error(), true)
			return
		}
		m.setFlash("note updated", false)
	case inputNote:
		sel := m.selected()
		if sel == nil {
			return
		}
		if _, err := m.deps.Notes.Add(sel.Task.Name, value, time.Now()); err != nil {
			m.setFlash(err.Error(), true)
			return
		}
		m.setFlash(fmt.Sprintf("noted on %q", sel.Task.Name), false)
	}
	m.refresh()
}

func (m *model) startSelected() {
	sel := m.selected()
	if sel == nil {
		return
	}
	now := time.Now()
	t, _, prev, err := m.deps.Tracker.Start(sel.Task.Name, tracker.StartOpts{}, now)
	if err != nil {
		m.setFlash(err.Error(), true)
		return
	}
	flash := fmt.Sprintf("tracking on %s", t.Name)
	if prev != nil {
		flash += m.importFlash(prev, now)
	}
	m.setFlash(flash, false)
	// The task is active now: follow it into the current view.
	m.filter = filterCurrent
	m.refresh()
	m.selectTask(t.Name)
}

// closeRunning pauses or (done=true) stops the running task.
func (m *model) closeRunning(done bool) {
	now := time.Now()
	var st *trackermodel.State
	var err error
	if done {
		st, err = m.deps.Tracker.Stop(now)
	} else {
		st, err = m.deps.Tracker.Pause(now)
	}
	if err != nil {
		m.setFlash(err.Error(), true)
		return
	}
	verb := "paused"
	if done {
		verb = "stopped"
	}
	m.setFlash(fmt.Sprintf("%s %q%s", verb, st.TaskName, m.importFlash(st, now)), false)
	m.refresh()
}

// importFlash runs the git commit import for a closed session and renders
// its outcome as a flash suffix. Failures degrade to a warning suffix.
func (m *model) importFlash(st *trackermodel.State, now time.Time) string {
	n, repo, err := m.deps.Tracker.ImportCommits(st, now)
	if err != nil {
		return fmt.Sprintf(" (import warning: %v)", err)
	}
	if n > 0 {
		return fmt.Sprintf(" (imported %d commit(s) from %s)", n, repo)
	}
	return ""
}
