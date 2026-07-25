package cli

import (
	"time"

	"github.com/spf13/cobra"
)

func newStopCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running task and mark it done",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now()
			st, err := app.Tracker.Stop(now)
			if err != nil {
				return err
			}
			if app.JSON {
				return printJSON(cmd, closedSession(app, st, now))
			}
			cmd.Printf("Stopped tracking %q (%s this session)\n", st.TaskName, formatDuration(now.Sub(st.Start)))
			reportImport(cmd, app, st, now)
			return nil
		},
	}
}
