package cli

import (
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newNoteCmd(app *App) *cobra.Command {
	var taskName string

	cmd := &cobra.Command{
		Use:   "note <text>...",
		Short: "Attach a note (PR link, context, ...) to the running task",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := app.Notes.Add(taskName, strings.Join(args, " "), time.Now())
			if err != nil {
				return err
			}
			cmd.Printf("Noted on %q\n", n.TaskName)
			return nil
		},
	}
	cmd.Flags().StringVarP(&taskName, "task", "t", "", "attach to this task instead of the running one")
	return cmd
}
