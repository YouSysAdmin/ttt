package cli

import (
	"github.com/spf13/cobra"

	"ttt/internal/domain/tasks"
)

func newEditCmd(app *App) *cobra.Command {
	var name, description, project, repo string

	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit a task's fields or rename it (pass an empty value to clear a field)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Only flags the user actually passed become changes, so an
			// empty value means "clear", not "leave as is".
			var ch tasks.Changes
			f := cmd.Flags()
			if f.Changed("name") {
				ch.Name = &name
			}
			if f.Changed("description") {
				ch.Description = &description
			}
			if f.Changed("project") {
				ch.Project = &project
			}
			if f.Changed("git") {
				ch.Repo = &repo
			}
			t, err := app.Tasks.Edit(args[0], ch)
			if err != nil {
				return err
			}
			if app.JSON {
				return printJSON(cmd, t)
			}
			cmd.Printf("Updated task %q\n", t.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "rename the task")
	cmd.Flags().StringVarP(&description, "description", "d", "", "set the description")
	cmd.Flags().StringVarP(&project, "project", "p", "", "set the project")
	cmd.Flags().StringVarP(&repo, "git", "g", "", "set the linked git repository")
	return cmd
}
