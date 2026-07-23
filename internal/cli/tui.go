package cli

import (
	"github.com/spf13/cobra"

	"ttt/internal/tui"
)

func newTuiCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Run the interactive terminal UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(tui.Deps{
				Tasks:   app.Tasks,
				Tracker: app.Tracker,
				Notes:   app.Notes,
			})
		},
	}
}
