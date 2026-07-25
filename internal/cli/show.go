package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ttt/internal/models/entry"
	"ttt/internal/models/note"
	"ttt/internal/models/task"
)

// showLabelWidth pads every inline field label to the widest one
// ("Completed: ") so all values start at the same column, matching the TUI
// info panel. Description and Notes render as sections, not inline fields.
const showLabelWidth = len("Completed: ")

func showLabel(name string) string {
	return fmt.Sprintf("%-*s", showLabelWidth, name+":")
}

func newShowCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a task's details, tracked time, and notes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := app.Tasks.Show(args[0], time.Now())
			if err != nil {
				return err
			}
			if app.JSON {
				// Empty collections marshal as [], not null, for consumers.
				entries := d.Entries
				if entries == nil {
					entries = []*entry.Entry{}
				}
				ns := d.Notes
				if ns == nil {
					ns = []*note.Note{}
				}
				return printJSON(cmd, struct {
					Task         *task.Task     `json:"task"`
					TotalSeconds int64          `json:"total_seconds"`
					Total        string         `json:"total"`
					Entries      []*entry.Entry `json:"entries"`
					Notes        []*note.Note   `json:"notes"`
				}{d.Task, secs(d.Total), formatDuration(d.Total), entries, ns})
			}
			t := d.Task
			cmd.Printf("%s%s\n", showLabel("Name"), t.Name)
			if t.Project != "" {
				cmd.Printf("%s%s\n", showLabel("Project"), t.Project)
			}
			cmd.Printf("%s%s\n", showLabel("Status"), t.Status)
			if t.Repo != "" {
				cmd.Printf("%s%s\n", showLabel("Repo"), t.Repo)
			}
			cmd.Printf("%s%s\n", showLabel("Created"), formatTime(t.CreatedAt))
			if !t.CompletedAt.IsZero() {
				cmd.Printf("%s%s\n", showLabel("Completed"), formatTime(t.CompletedAt))
			}
			cmd.Printf("%s%s across %d entries\n", showLabel("Total"), formatDuration(d.Total), len(d.Entries))

			if t.Description != "" {
				cmd.Println("\nDescription:")
				for _, line := range strings.Split(t.Description, "\n") {
					cmd.Printf("  %s\n", line)
				}
			}

			if len(d.Notes) > 0 {
				cmd.Println("\nNotes:")
				for _, n := range d.Notes {
					cmd.Printf("  %s  %s\n", formatTime(n.CreatedAt), n.Text)
				}
			}
			return nil
		},
	}
}
