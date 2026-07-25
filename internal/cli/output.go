package cli

import (
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	trackermodel "ttt/internal/models/tracker"
)

// printJSON writes interface (v any) to the command's stdout as indented JSON.
// Every command switches to it when the global --json flag is set, so automation gets one
// machine-readable document per invocation on stdout and nothing else.
func printJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// secs is the machine form of a duration; JSON payloads carry it next to the
// human HH:MM:SS string.
func secs(d time.Duration) int64 {
	return int64(d.Round(time.Second) / time.Second)
}

// importJSON reports the git-commit import that runs when a session closes —
// the JSON counterpart of reportImport. Import failures are data, not command
// errors, matching the soft-fail rule.
type importJSON struct {
	ImportedCommits int    `json:"imported_commits"`
	ImportRepo      string `json:"import_repo,omitempty"`
	ImportError     string `json:"import_error,omitempty"`
}

func runImport(app *App, st *trackermodel.State, end time.Time) importJSON {
	n, repo, err := app.Tracker.ImportCommits(st, end)
	if err != nil {
		return importJSON{ImportError: err.Error()}
	}
	out := importJSON{ImportedCommits: n}
	if n > 0 {
		out.ImportRepo = repo
	}
	return out
}

// sessionJSON is the shared pause/stop payload: the closed session plus the
// commit-import outcome.
type sessionJSON struct {
	TaskName       string `json:"task_name"`
	SessionSeconds int64  `json:"session_seconds"`
	Session        string `json:"session"`
	importJSON
}

func closedSession(app *App, st *trackermodel.State, now time.Time) sessionJSON {
	return sessionJSON{
		TaskName:       st.TaskName,
		SessionSeconds: secs(now.Sub(st.Start)),
		Session:        formatDuration(now.Sub(st.Start)),
		importJSON:     runImport(app, st, now),
	}
}
