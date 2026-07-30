package cli

import (
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"ttt/internal/tui"
)

// noUpdateCheck is a pointer to the root's persistent flag: it is read in RunE,
// after cobra has parsed the flags.
func newTuiCmd(app *App, version string, noUpdateCheck *bool) *cobra.Command {
	var debug bool

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Run the interactive terminal UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var remote *tui.RemoteInfo
			if app.remote != nil {
				remote = &tui.RemoteInfo{
					Host:     remoteHost(app.rt.Config.Remote.URL),
					NextSync: app.remote.NextSync,
					ShowSync: debug,
				}
			}
			return tui.Run(tui.Deps{
				Tasks:         app.Tasks,
				Tracker:       app.Tracker,
				Notes:         app.Notes,
				Version:       version,
				NoUpdateCheck: *noUpdateCheck,
				Remote:        remote,
			})
		},
	}
	cmd.Flags().BoolVar(&debug, "debug", false, "show diagnostics)")
	return cmd
}

// remoteHost compacts the remote URL for the badge: host (and port when
// non-standard), no scheme.
func remoteHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	return u.Host
}
