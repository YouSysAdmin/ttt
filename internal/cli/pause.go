package cli

import (
	"time"

	"github.com/spf13/cobra"
)

func newPauseCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the running task (resumable with start)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now()
			st, err := app.Tracker.Pause(now)
			if err != nil {
				return err
			}
			if app.JSON {
				return printJSON(cmd, closedSession(app, st, now))
			}
			cmd.Printf("Paused %q (%s this session)\n", st.TaskName, formatDuration(now.Sub(st.Start)))
			reportImport(cmd, app, st, now)
			return nil
		},
	}
}
