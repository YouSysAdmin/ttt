package cli

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var periodRe = regexp.MustCompile(`^([1-9][0-9]*)([hdwmy])$`)

// periodStart returns now minus the period "<N><unit>".
// All units use calendar arithmetic except hours, so "1m" means one month back,
// not 30 days, and day boundaries survive DST.
func periodStart(period string, now time.Time) (time.Time, error) {
	m := periodRe.FindStringSubmatch(period)
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid period %q: use <N><unit> with unit h, d, w, m, or y (e.g. 10d, 2w, 6m)", period)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return time.Time{}, err
	}
	switch m[2] {
	case "h":
		return now.Add(-time.Duration(n) * time.Hour), nil
	case "d":
		return now.AddDate(0, 0, -n), nil
	case "w":
		return now.AddDate(0, 0, -7*n), nil
	case "m":
		return now.AddDate(0, -n, 0), nil
	default: // y
		return now.AddDate(-n, 0, 0), nil
	}
}

func newStatsCmd(app *App) *cobra.Command {
	var period, project string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show tracked time per task and project over a period",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now()
			from, err := periodStart(period, now)
			if err != nil {
				return err
			}
			rows, err := app.Tasks.Stats(from, now, project)
			if err != nil {
				return err
			}

			cmd.Printf("Period: %s → %s\n\n", formatTime(from), formatTime(now))
			if len(rows) == 0 {
				cmd.Println("No tracked time in this period")
				return nil
			}

			byProject := map[string]time.Duration{}
			var total time.Duration
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TASK\tPROJECT\tTIME")
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\n", r.Task.Name, r.Task.Project, formatDuration(r.Total))
				byProject[r.Task.Project] += r.Total
				total += r.Total
			}

			// Project subtotals only make sense once categories are in use.
			if len(byProject) > 1 || (len(byProject) == 1 && !hasKey(byProject, "")) {
				fmt.Fprintln(w, "\nPROJECT\tTIME")
				for _, c := range sortedByDuration(byProject) {
					name := c
					if name == "" {
						name = "(none)"
					}
					fmt.Fprintf(w, "%s\t%s\n", name, formatDuration(byProject[c]))
				}
			}

			fmt.Fprintf(w, "\nTOTAL\t%s\n", formatDuration(total))
			return w.Flush()
		},
	}
	cmd.Flags().StringVarP(&period, "period", "p", "1m", "period to cover: <N><unit>, units h, d, w, m, y (e.g. 10d, 2w, 6m)")
	cmd.Flags().StringVar(&project, "project", "", "only count tasks in this project")
	return cmd
}

func hasKey(m map[string]time.Duration, k string) bool {
	_, ok := m[k]
	return ok
}

// sortedByDuration returns m's keys, largest value first.
func sortedByDuration(m map[string]time.Duration) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortStableFunc(keys, func(a, b string) int {
		return cmp.Compare(m[b], m[a])
	})
	return keys
}
