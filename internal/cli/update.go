package cli

import (
	"github.com/spf13/cobra"

	"ttt/internal/core/update"
)

func newUpdateCmd(version string) *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update ttt to the latest GitHub release",
		Long: "Update ttt in place from the latest GitHub release.\n\n" +
			"Installs owned by a package manager (brew, apk, deb, rpm) are detected\n" +
			"and self-update is blocked there — upgrade through the package manager\n" +
			"instead. Use --check to only report whether a newer version exists.",
		Args: cobra.NoArgs,
		// The update command never touches the database; overriding the root
		// PersistentPreRunE skips config loading and the store flock.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			if checkOnly {
				res := update.CheckLatestVersion(version)
				switch {
				case res.Err != nil:
					return res.Err
				case res.LatestVersion != "":
					cmd.Printf("Update available: v%s -> v%s\nRun `ttt update` to install it.\n",
						res.CurrentVersion, res.LatestVersion)
				case version == "" || version == "dev":
					cmd.Println("Development build; version check skipped")
				default:
					cmd.Printf("Already up to date (v%s)\n", res.CurrentVersion)
				}
				return nil
			}

			src, err := update.DetectSource()
			if err != nil {
				return err
			}
			if src.Managed() {
				return update.BlockedError(src)
			}
			return update.DownloadAndReplace(version, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for a newer version, do not install")
	return cmd
}
