package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/reflow/truncate"
)

var (
	styleTracking = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	styleIdle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleHeader   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	styleStatus   = map[string]lipgloss.Style{
		"todo":   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		"active": lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		"paused": lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		"done":   lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	}
	styleFlashOk  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleFlashErr = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleBorder   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	// Top padding keeps content from touching the border/title row.
	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 1, 0, 1)
)

// titledBox renders content in a rounded-border box of outer width w, with
// the title spliced into the top border line. Lipgloss v1 has no border
// titles, so the top line is rebuilt by hand.
func titledBox(title, content string, w int, h int) string {
	style := stylePanel.Width(w - 2)
	if h > 0 {
		style = style.Height(h - 2)
	}
	box := style.Render(content)
	i := strings.Index(box, "\n")
	if i < 0 {
		return box
	}
	title = clip(title, w-6)
	fill := max(0, w-5-ansi.StringWidth(title))
	top := styleBorder.Render("╭─ ") + styleTitle.Render(title) +
		styleBorder.Render(" "+strings.Repeat("─", fill)+"╮")
	return top + box[i:]
}

// Layout: the tasks panel (left) beside the info panel (right top) and the
// timer/stats row (right bottom), with help and flash lines at the bottom.
func (m *model) View() string {
	// Fallback size before the first WindowSizeMsg (and in tests).
	w, h := m.width, m.height
	if w < 40 {
		w = 100
	}
	if h < 12 {
		h = 30
	}

	bottom := m.bottomBar(w)
	bodyH := h - lipgloss.Height(bottom)

	leftW := w * 2 / 5
	rightW := w - leftW
	// Info gets the larger share: task fields plus the notes list live there.
	// The bottom row splits into the big timer (left) and stats (right).
	infoH := bodyH * 3 / 5
	bottomRowH := bodyH - infoH
	timerW := rightW * 2 / 5
	statsW := rightW - timerW

	// -3 per panel: two border rows plus the top padding row.
	left := titledBox("Tasks ("+m.filter.label()+")", m.listContent(leftW-4, bodyH-3), leftW, bodyH)
	right := lipgloss.JoinVertical(lipgloss.Left,
		titledBox("Info", m.infoContent(rightW-4, infoH-3), rightW, infoH),
		lipgloss.JoinHorizontal(lipgloss.Top,
			titledBox("Current task time", m.timerContent(timerW-4, bottomRowH-3), timerW, bottomRowH),
			titledBox(m.statsTitle(), m.statsContent(statsW-4, bottomRowH-3), statsW, bottomRowH),
		),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	// Modals float centered over the panels instead of replacing them.
	switch m.mode {
	case modeEdit:
		body = overlay(body, m.editModal())
	case modeConfirm:
		body = overlay(body, m.confirmModal())
	case modeInput:
		body = overlay(body, m.inputModal())
	case modeRange:
		body = overlay(body, m.rangeModal())
	}

	return body + "\n" + bottom
}

func (m *model) statsTitle() string {
	if m.customRange {
		return fmt.Sprintf("Stats (%s → %s)",
			m.customFrom.Format("2006-01-02"), m.customTo.Format("2006-01-02"))
	}
	return "Stats (" + m.period.label() + ")"
}

// timerContent renders the running task's total tracked time (all entries,
// so a resumed task continues where it left off) as a seven-segment clock,
// degrading to shorter or plain text when narrow.
func (m *model) timerContent(w, h int) string {
	name := "Not tracking"
	style := styleIdle
	var elapsed time.Duration
	if m.status != nil && m.status.State != nil {
		name = m.status.State.TaskName
		style = styleTracking
		elapsed = m.timerTotal
	}

	full := formatDuration(elapsed)
	short := full[:len(full)-3] // drop seconds
	var body string
	switch {
	case h >= bigTimeRows+1 && w >= bigTimeWidth(full):
		body = strings.Join(bigTime(full), "\n")
	case h >= bigTimeRows+1 && w >= bigTimeWidth(short):
		body = strings.Join(bigTime(short), "\n")
	default:
		body = full
	}
	content := style.Render(body) + "\n\n" + clip(name, w)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}

// rangeModal renders the from/to date pickers side by side.
func (m *model) rangeModal() string {
	focus := func(active bool, title string) string {
		if active {
			return "▶ " + title
		}
		return title
	}
	pickerW := max(24, (m.modalWidth()-6)/2)
	boxes := lipgloss.JoinHorizontal(lipgloss.Top,
		titledBox(focus(!m.rangeFocusTo, "From"), m.rangeFrom.View(), pickerW, 0),
		" ",
		titledBox(focus(m.rangeFocusTo, "To"), m.rangeTo.View(), pickerW, 0),
	)
	help := styleHelp.Render("space switch from/to · arrows move · tab month/year · enter apply · esc cancel")
	return titledBox("Stats period", boxes+"\n"+help, m.modalWidth(), 0)
}

// modalWidth is the outer width for every modal: 60% of the screen, but
// never cramped and never wider than the screen allows.
func (m *model) modalWidth() int {
	w := m.width
	if w < 40 {
		w = 100
	}
	mw := w * 3 / 5
	if mw < 50 {
		mw = 50
	}
	if mw > w-4 {
		mw = w - 4
	}
	return mw
}

// overlay composites fg centered over bg, splicing fg's cells into bg's
// lines ANSI-aware so the panels stay visible around the modal.
func overlay(bg, fg string) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	var bgW, fgW int
	for _, l := range bgLines {
		bgW = max(bgW, ansi.StringWidth(l))
	}
	for _, l := range fgLines {
		fgW = max(fgW, ansi.StringWidth(l))
	}
	x := max(0, (bgW-fgW)/2)
	y := max(0, (len(bgLines)-len(fgLines))/2)

	for i, fgLine := range fgLines {
		bi := y + i
		if bi >= len(bgLines) {
			break
		}
		bgLine := bgLines[bi]
		left := ansi.Truncate(bgLine, x, "")
		if pad := x - ansi.StringWidth(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		if pad := fgW - ansi.StringWidth(fgLine); pad > 0 {
			fgLine += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(bgLine, x+fgW, "")
		bgLines[bi] = left + fgLine + right
	}
	return strings.Join(bgLines, "\n")
}

// editModal renders the edit-task form.
func (m *model) editModal() string {
	labelW := 0
	for _, l := range editLabels {
		labelW = max(labelW, len(l))
	}
	lines := make([]string, 0, editFieldCount+2)
	for i, l := range editLabels {
		marker := "  "
		if i == m.editFocus {
			marker = styleTracking.Render("> ")
		}
		lines = append(lines, fmt.Sprintf("%s%-*s  %s", marker, labelW, l+":", m.editInputs[i].View()))
	}
	lines = append(lines, "", styleHelp.Render("tab/↑/↓ field · enter save · esc cancel"))
	title := "Edit task"
	if m.editCreate {
		title = "New task"
	}
	return titledBox(title, strings.Join(lines, "\n"), m.modalWidth(), 0)
}

// confirmModal renders the yes/no prompt.
func (m *model) confirmModal() string {
	return titledBox("Confirm",
		m.confirmPrompt+"\n\n"+styleHelp.Render("y confirm · n/esc cancel"),
		m.modalWidth(), 0)
}

// inputModal renders the single-line note input.
func (m *model) inputModal() string {
	title := "New note"
	if m.inputKind == inputEditNote {
		title = "Edit note"
	}
	return titledBox(title,
		m.input.View()+"\n\n"+styleHelp.Render("enter save · esc cancel"),
		m.modalWidth(), 0)
}

// bottomBar renders the input line (in input mode) or the key help, plus the
// flash line. Always two lines so the layout doesn't jump.
func (m *model) bottomBar(w int) string {
	var top string
	switch m.mode {
	case modeInput:
		top = styleHelp.Render(" enter save · esc cancel")
	case modeEdit:
		top = styleHelp.Render(" tab/↑/↓ field · enter save · esc cancel")
	case modeConfirm:
		top = styleHelp.Render(" y confirm · n/esc cancel")
	case modeNotes:
		top = styleHelp.Render(" ↑/↓ note · e edit · d delete · esc back")
	case modeRange:
		top = styleHelp.Render(" space switch from/to · arrows move · tab month/year · enter apply · esc cancel")
	default:
		top = styleHelp.Render(clip(" ↑/↓ · enter start/pause · x stop · a add · e edit · d del · n add note · v notes · f filter · t period · T range · q quit", w))
	}

	flash := " "
	if m.flash != "" {
		style := styleFlashOk
		if m.flashErr {
			style = styleFlashErr
		}
		flash = style.Render(truncate.StringWithTail(" "+m.flash, uint(w), "…"))
	}
	return top + "\n" + flash
}

// listContent renders the task table, windowed vertically around the cursor.
func (m *model) listContent(w, maxLines int) string {
	if len(m.rows) == 0 {
		return styleIdle.Render(fmt.Sprintf("No %s tasks - 'a' adds one, 'f' switches the filter", m.filter.label()))
	}

	// The name column stretches so the table spans the full panel width:
	// cursor(2) + name + 2 + status(6) + 2 + total(8) = w.
	nameW := max(len("NAME"), w-20)

	header := styleHeader.Render(fmt.Sprintf("  %-*s  %-6s  %-8s", nameW, "NAME", "STATUS", "TOTAL"))
	rowLines := make([]string, 0, len(m.rows))
	for i, r := range m.rows {
		line := fmt.Sprintf("%-*s  %-6s  %-8s",
			nameW, clip(r.Task.Name, nameW), r.Task.Status, formatDuration(r.Total))
		if i == m.cursor {
			rowLines = append(rowLines, styleSelected.Render("> "+line))
			continue
		}
		st, ok := styleStatus[string(r.Task.Status)]
		if !ok {
			st = styleIdle
		}
		rowLines = append(rowLines, "  "+st.Render(line))
	}
	lines := append([]string{header}, window(rowLines, maxLines-1, m.cursor, w)...)
	return strings.Join(lines, "\n")
}

// infoContent renders the selected task's details and notes.
func (m *model) infoContent(w, maxLines int) string {
	d := m.detail
	if d == nil {
		return styleIdle.Render("No task selected")
	}
	lines := []string{styleTitle.Render(clip(d.Task.Name, w))}
	if d.Task.Project != "" {
		lines = append(lines, clip("Project: "+d.Task.Project, w))
	}
	lines = append(lines, clip("Status: "+string(d.Task.Status), w))
	if d.Task.Description != "" {
		lines = append(lines, clip("Description: "+d.Task.Description, w))
	}
	if d.Task.Repo != "" {
		lines = append(lines, clip("Repo: "+d.Task.Repo, w))
	}
	lines = append(lines, clip("Created: "+formatTime(d.Task.CreatedAt), w))
	if !d.Task.CompletedAt.IsZero() {
		lines = append(lines, clip("Completed: "+formatTime(d.Task.CompletedAt), w))
	}
	lines = append(lines, clip(fmt.Sprintf("Total: %s across %d entries", formatDuration(d.Total), len(d.Entries)), w))
	if len(d.Notes) > 0 && len(lines)+1 < maxLines {
		lines = append(lines, styleTitle.Render("Notes:"))
		// Newest first. The window scrolls with the note cursor in modeNotes.
		noteLines := make([]string, 0, len(d.Notes))
		for i, n := range m.displayNotes() {
			line := clip(fmt.Sprintf(" %s  %s", formatTime(n.CreatedAt), n.Text), w)
			if m.mode == modeNotes && i == m.noteCursor {
				line = styleSelected.Render(line)
			}
			noteLines = append(noteLines, line)
		}
		cursor := -1
		if m.mode == modeNotes {
			cursor = m.noteCursor
		}
		lines = append(lines, window(noteLines, maxLines-len(lines), cursor, w)...)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}

// barPalette colors project/task bars, cycling by row.
var barPalette = []lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("42")),  // green
	lipgloss.NewStyle().Foreground(lipgloss.Color("39")),  // blue
	lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // orange
	lipgloss.NewStyle().Foreground(lipgloss.Color("205")), // pink
	lipgloss.NewStyle().Foreground(lipgloss.Color("81")),  // cyan
	lipgloss.NewStyle().Foreground(lipgloss.Color("141")), // purple
}

// bar renders a w-cell bar: frac filled, the rest a dim track.
func bar(frac float64, w int, style lipgloss.Style) string {
	if w <= 0 {
		return ""
	}
	fill := int(frac*float64(w) + 0.5)
	if frac > 0 && fill == 0 {
		fill = 1 // any tracked time deserves a visible sliver
	}
	if fill > w {
		fill = w
	}
	return style.Render(strings.Repeat("█", fill)) +
		styleBorder.Render(strings.Repeat("░", w-fill))
}

// statsContent renders per-project totals as proportional bars (scaled to
// the largest project, with percentages of the grand total), followed by a
// per-task breakdown while space remains. Falls back to plain rows when the
// panel is too narrow for bars.
func (m *model) statsContent(w, maxLines int) string {
	if len(m.stats) == 0 {
		return styleIdle.Render("No tracked time " + m.period.label())
	}

	byProject := map[string]time.Duration{}
	var order []string
	var total time.Duration
	for _, r := range m.stats {
		c := r.Task.Project
		if c == "" {
			c = "(none)"
		}
		if _, seen := byProject[c]; !seen {
			order = append(order, c)
		}
		byProject[c] += r.Total
		total += r.Total
	}

	nameW := len("TOTAL")
	for _, c := range order {
		nameW = max(nameW, len(c))
	}
	for _, r := range m.stats {
		nameW = max(nameW, len(r.Task.Name))
	}
	nameW = min(nameW, 16)

	// name + 2 + bar + 2 + HH:MM:SS + 2 + "99%": the bar gets the rest.
	barW := w - nameW - 8 - 4 - 6
	row := func(i int, name string, d, scale time.Duration) string {
		name = clip(name, nameW)
		if barW < 5 {
			return clip(fmt.Sprintf("%-*s  %s", nameW, name, formatDuration(d)), w)
		}
		frac := 0.0
		if scale > 0 {
			frac = float64(d) / float64(scale)
		}
		pct := 0.0
		if total > 0 {
			pct = 100 * float64(d) / float64(total)
		}
		return fmt.Sprintf("%-*s  %s  %s %3.0f%%",
			nameW, name, bar(frac, barW, barPalette[i%len(barPalette)]), formatDuration(d), pct)
	}

	var lines []string
	maxProject := byProject[order[0]]
	for i, c := range order {
		lines = append(lines, row(i, c, byProject[c], maxProject))
	}

	// Per-task breakdown, when more than one task shares the period.
	if len(m.stats) > 1 {
		lines = append(lines, "", styleHeader.Render("TASKS"))
		maxTask := m.stats[0].Total
		for i, r := range m.stats {
			lines = append(lines, row(i, r.Task.Name, r.Total, maxTask))
		}
	}

	// TOTAL stays pinned below the (scrollable) breakdown.
	out := window(lines, maxLines-1, -1, w)
	out = append(out, styleTitle.Render(clip(fmt.Sprintf("%-*s  %s", nameW, "TOTAL", formatDuration(total)), w)))
	return strings.Join(out, "\n")
}

// window clips lines to maxLines, scrolled so line `cursor` stays visible
// (cursor < 0 pins the top), and adds a right-edge scrollbar at column w
// when there is more than fits.
func window(lines []string, maxLines, cursor, w int) []string {
	if maxLines <= 0 {
		return nil
	}
	if len(lines) <= maxLines {
		return lines
	}
	offset := 0
	if cursor >= maxLines {
		offset = cursor - maxLines + 1
	}
	if offset > len(lines)-maxLines {
		offset = len(lines) - maxLines
	}

	// Proportional thumb over a dim track.
	thumb := max(1, maxLines*maxLines/len(lines))
	thumbStart := 0
	if denom := len(lines) - maxLines; denom > 0 {
		thumbStart = (maxLines - thumb) * offset / denom
	}

	out := make([]string, maxLines)
	for i := range out {
		line := clip(lines[offset+i], w-1)
		if pad := w - 1 - ansi.StringWidth(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		glyph := "│"
		if i >= thumbStart && i < thumbStart+thumb {
			glyph = "█"
		}
		out[i] = line + styleHelp.Render(glyph)
	}
	return out
}

// clip truncates s to width cells, ANSI-aware, with an ellipsis. Only
// actually-too-wide strings are touched: StringWithTail reserves room for
// the tail even when s fits exactly, which would mangle exact-width lines.
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "…")
}

func formatTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%02d:%02d:%02d",
		d/time.Hour, (d%time.Hour)/time.Minute, (d%time.Minute)/time.Second)
}
