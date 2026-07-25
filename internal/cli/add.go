package cli

import "github.com/spf13/cobra"

func newAddCmd(app *App) *cobra.Command {
	var description, project, repo string

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := app.Tasks.Add(args[0], description, project, repo)
			if err != nil {
				return err
			}
			if app.JSON {
				return printJSON(cmd, t)
			}
			cmd.Printf("Added task %q\n", t.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&description, "description", "d", "", "task description")
	cmd.Flags().StringVarP(&project, "project", "p", "", "task project (e.g. an org or app)")
	cmd.Flags().StringVarP(&repo, "git", "g", "", "link a git repository. Your commits made while tracking are imported as notes")
	return cmd
}
