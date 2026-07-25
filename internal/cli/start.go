package cli

import (
	"time"

	"github.com/spf13/cobra"

	"ttt/internal/domain/tracker"
	"ttt/internal/models/task"
)

func newStartCmd(app *App) *cobra.Command {
	var opts tracker.StartOpts

	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start tracking a task (pauses any running task, creates the task if missing)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now()
			t, created, prev, err := app.Tracker.Start(args[0], opts, now)
			if err != nil {
				return err
			}
			if app.JSON {
				payload := struct {
					Task     *task.Task   `json:"task"`
					Created  bool         `json:"created"`
					Previous *sessionJSON `json:"previous"` // preempted task, if any
				}{t, created, nil}
				if prev != nil {
					s := closedSession(app, prev, now)
					payload.Previous = &s
				}
				return printJSON(cmd, payload)
			}
			if created {
				cmd.Printf("Created task %q\n", t.Name)
			}
			if prev != nil {
				cmd.Printf("Paused %q\n", prev.TaskName)
				reportImport(cmd, app, prev, now)
			}
			cmd.Printf("Tracking on %s\n", t.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.Project, "project", "p", "", "set the task's project (e.g. an org or app)")
	cmd.Flags().StringVarP(&opts.Repo, "git", "g", "", "link a git repository; your commits made while tracking are imported as notes")
	return cmd
}
