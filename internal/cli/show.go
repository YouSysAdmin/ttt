package cli

import (
	"time"

	"github.com/spf13/cobra"
)

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
			t := d.Task
			cmd.Printf("Task: %s\n", t.Name)
			if t.Project != "" {
				cmd.Printf("Project: %s\n", t.Project)
			}
			cmd.Printf("Status: %s\n", t.Status)
			if t.Description != "" {
				cmd.Printf("Description: %s\n", t.Description)
			}
			if t.Repo != "" {
				cmd.Printf("Repo: %s\n", t.Repo)
			}
			cmd.Printf("Created: %s\n", formatTime(t.CreatedAt))
			if !t.CompletedAt.IsZero() {
				cmd.Printf("Completed: %s\n", formatTime(t.CompletedAt))
			}
			cmd.Printf("Total: %s across %d entries\n", formatDuration(d.Total), len(d.Entries))

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
