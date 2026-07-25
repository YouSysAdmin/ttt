package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"ttt/internal/models/task"
)

func newListCmd(app *App) *cobra.Command {
	var project string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks with status and total tracked time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := app.Tasks.List(time.Now(), project)
			if err != nil {
				return err
			}
			if app.JSON {
				type rowJSON struct {
					Task         *task.Task `json:"task"`
					TotalSeconds int64      `json:"total_seconds"`
					Total        string     `json:"total"`
					Running      bool       `json:"running"`
				}
				out := make([]rowJSON, 0, len(rows))
				for _, r := range rows {
					out = append(out, rowJSON{r.Task, secs(r.Total), formatDuration(r.Total), r.Running})
				}
				return printJSON(cmd, out)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPROJECT\tSTATUS\tTOTAL\tDESCRIPTION")
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					r.Task.Name, r.Task.Project, r.Task.Status, formatDuration(r.Total), r.Task.Description)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "only show tasks in this project")
	return cmd
}
