package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	datepicker "github.com/ethanefung/bubble-datepicker"

	"ttt/internal/domain/tasks"
	"ttt/internal/models/task"
)

// Task-form field order. Keep editLabels in sync.
const (
	editName = iota
	editDescription
	editProject
	editRepo
	editFieldCount
)

var editLabels = [editFieldCount]string{"Name", "Description", "Project", "Repo"}

// openAddForm opens the task form empty, for creating a new task.
func (m *model) openAddForm() {
	m.openForm(nil)
}

// openEdit opens the task form prefilled with the selected task.
func (m *model) openEdit() {
	sel := m.selected()
	if sel == nil {
		return
	}
	m.openForm(sel.Task)
}

// openForm shows the task form. A nil task means create mode.
func (m *model) openForm(t *task.Task) {
	var values [editFieldCount]string
	if t != nil {
		values = [editFieldCount]string{t.Name, t.Description, t.Project, t.Repo}
	}
	m.editCreate = t == nil
	labelW := 0
	for _, l := range editLabels {
		labelW = max(labelW, len(l))
	}
	m.editInputs = make([]textinput.Model, editFieldCount)
	for i := range m.editInputs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 256
		ti.Width = m.modalWidth() - labelW - 12
		ti.SetValue(values[i])
		m.editInputs[i] = ti
	}
	m.editFocus = editName
	m.editInputs[editName].Focus()
	m.mode = modeEdit
}

func (m *model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		return m, nil
	case "enter":
		m.submitEdit()
		return m, nil
	case "tab", "down":
		m.focusEditField((m.editFocus + 1) % editFieldCount)
		return m, nil
	case "shift+tab", "up":
		m.focusEditField((m.editFocus + editFieldCount - 1) % editFieldCount)
		return m, nil
	}
	var cmd tea.Cmd
	m.editInputs[m.editFocus], cmd = m.editInputs[m.editFocus].Update(msg)
	return m, cmd
}

func (m *model) focusEditField(i int) {
	m.editInputs[m.editFocus].Blur()
	m.editFocus = i
	m.editInputs[i].Focus()
}

// submitEdit saves the form. Create mode adds a task. Edit mode applies
// every field (the form was prefilled, so unchanged fields write back their
// current value and cleared fields clear). Errors flash and keep the form
// open.
func (m *model) submitEdit() {
	values := make([]string, editFieldCount)
	for i := range m.editInputs {
		values[i] = m.editInputs[i].Value()
	}

	if m.editCreate {
		t, err := m.deps.Tasks.Add(values[editName], values[editDescription], values[editProject], values[editRepo])
		if err != nil {
			m.setFlash(err.Error(), true)
			return
		}
		m.mode = modeList
		m.setFlash(fmt.Sprintf("added task %q", t.Name), false)
		// New tasks are todo: switch to the filter that shows them.
		m.filter = filterNew
		m.refresh()
		m.selectTask(t.Name)
		return
	}

	sel := m.selected()
	if sel == nil {
		m.mode = modeList
		return
	}
	ch := tasks.Changes{
		Name:        &values[editName],
		Description: &values[editDescription],
		Project:     &values[editProject],
		Repo:        &values[editRepo],
	}
	t, err := m.deps.Tasks.Edit(sel.Task.Name, ch)
	if err != nil {
		m.setFlash(err.Error(), true)
		return
	}
	m.mode = modeList
	m.setFlash(fmt.Sprintf("updated task %q", t.Name), false)
	m.refresh()
}

// openRange opens the from/to date pickers, seeded with the current window.
func (m *model) openRange() {
	now := time.Now()
	from, to := m.statsWindow(now)
	if !m.customRange {
		to = now
	} else {
		to = m.customTo
	}
	// SelectDate marks the date as selected - without it the picker renders
	// no highlight at all. The focused day gets the app's cursor colors, and
	// the blurred picker keeps its selection visible in orange.
	newPicker := func(t time.Time) datepicker.Model {
		dp := datepicker.New(t)
		dp.SelectDate()
		dp.Styles.FocusedText = styleSelected
		dp.Styles.SelectedText = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
		return dp
	}
	m.rangeFrom = newPicker(from)
	m.rangeFrom.SetFocus(datepicker.FocusCalendar)
	m.rangeTo = newPicker(to)
	m.rangeTo.SetFocus(datepicker.FocusNone)
	m.rangeFocusTo = false
	m.mode = modeRange
}

// updateRange drives the two pickers: space switches from/to, everything
// else (arrows, tab for month/year) goes to the focused picker.
func (m *model) updateRange(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		return m, nil
	case " ":
		m.rangeFocusTo = !m.rangeFocusTo
		if m.rangeFocusTo {
			m.rangeFrom.SetFocus(datepicker.FocusNone)
			m.rangeTo.SetFocus(datepicker.FocusCalendar)
		} else {
			m.rangeFrom.SetFocus(datepicker.FocusCalendar)
			m.rangeTo.SetFocus(datepicker.FocusNone)
		}
		return m, nil
	case "enter":
		from := midnight(m.rangeFrom.Time)
		to := midnight(m.rangeTo.Time)
		if to.Before(from) {
			m.setFlash("period end is before its start", true)
			return m, nil
		}
		m.customRange = true
		m.customFrom, m.customTo = from, to
		m.mode = modeList
		m.refresh()
		return m, nil
	}
	var cmd tea.Cmd
	if m.rangeFocusTo {
		m.rangeTo, cmd = m.rangeTo.Update(msg)
	} else {
		m.rangeFrom, cmd = m.rangeFrom.Update(msg)
	}
	return m, cmd
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// openConfirm shows a yes/no modal. The action runs on "y".
func (m *model) openConfirm(prompt string, action func() error) {
	m.confirmPrompt = prompt
	m.confirmAction = action
	m.mode = modeConfirm
}

func (m *model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "y", "Y":
		m.mode = modeList
		if err := m.confirmAction(); err != nil {
			m.setFlash(err.Error(), true)
		}
		m.refresh()
	case "n", "N", "esc", "q":
		m.mode = modeList
	}
	return m, nil
}
