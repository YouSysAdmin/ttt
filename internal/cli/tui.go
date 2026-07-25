package cli

import (
	"github.com/spf13/cobra"

	"ttt/internal/tui"
)

// noUpdateCheck is a pointer to the root's persistent flag: it is read in RunE,
// after cobra has parsed the flags.
func newTuiCmd(app *App, version string, noUpdateCheck *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Run the interactive terminal UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(tui.Deps{
				Tasks:         app.Tasks,
				Tracker:       app.Tracker,
				Notes:         app.Notes,
				Version:       version,
				NoUpdateCheck: *noUpdateCheck,
			})
		},
	}
}
