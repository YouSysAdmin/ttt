// Package cli wires the ttt command tree: config loading, store lifecycle,
// and thin cobra commands that delegate to the domain handlers.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"ttt/internal/core/env"
	"ttt/internal/core/update"
	"ttt/internal/database/boltkv"
	remoteclient "ttt/internal/remote/client"
	"ttt/internal/domain/notes"
	"ttt/internal/domain/store"
	"ttt/internal/domain/tasks"
	"ttt/internal/domain/tracker"
	"ttt/internal/models/task"
	trackermodel "ttt/internal/models/tracker"
)

// App carries the per-process wiring built in the root PersistentPreRunE and
// torn down by Execute.
type App struct {
	rt *env.Runtime
	st *store.Store
	kv *boltkv.Store
	// remote is set in client mode - the TUI mode badge reads its sync state.
	remote *remoteclient.Client

	Tasks   *tasks.Handler
	Tracker *tracker.Handler
	Notes   *notes.Handler

	// JSON is the global --json flag: every command output (and the error printer)
	// switches to machine-readable JSON.
	JSON bool
}

// Close releases the store. Safe on a partially-initialized App.
func (a *App) Close() error {
	if a.kv == nil {
		return nil
	}
	return a.kv.Close()
}

// Execute runs the CLI and always closes the store, even when a command
// errors (PersistentPostRunE would be skipped by cobra in that case). It is
// also the single error printer: text or a JSON envelope depending on --json,
// so main only turns the returned error into the exit code.
func Execute(version string) error {
	app := &App{}
	root := newRootCmd(app, version)
	err := root.Execute()
	if cerr := app.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		if app.JSON {
			data, _ := json.Marshal(struct {
				Error string `json:"error"`
			}{err.Error()})
			fmt.Fprintln(os.Stderr, string(data))
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
	}
	return err
}

func newRootCmd(app *App, version string) *cobra.Command {
	var configPath, dbPath string
	var remoteURL, remoteToken string
	var remoteInsecure bool
	var noUpdateCheck bool

	root := &cobra.Command{
		Use:           "ttt",
		Short:         "Tasks and time tracker",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := env.Load(configPath)
			if err != nil {
				return err
			}
			// Flags outrank every config source (file, env, default).
			if dbPath != "" {
				cfg.Database.Path = dbPath
			}
			if remoteURL != "" {
				cfg.Remote.URL = remoteURL
			}
			if remoteToken != "" {
				cfg.Remote.Token = remoteToken
			}
			if remoteInsecure {
				cfg.Remote.Insecure = true
			}

			// A configured remote URL switches every command to client mode -
			// except `server`, so a host whose config points at a remote can
			// still serve its local store. An explicit --db forces local mode
			// (it names a file, which a remote can't honor).
			useRemote := cfg.Remote.URL != "" && cmd.Name() != "server"
			if dbPath != "" {
				if remoteURL != "" {
					return errors.New("--db and --remote-url are mutually exclusive")
				}
				useRemote = false
			}

			// In client mode Runtime.DB and Runtime.StoreProvider stay nil:
			// only the boltkv-backed domain stores ever dereference them.
			app.rt = &env.Runtime{Config: cfg}
			app.st = &store.Store{}
			if useRemote {
				c, err := remoteclient.New(cfg.Remote.URL, remoteclient.Options{
					Token:    cfg.Remote.Token,
					Insecure: cfg.Remote.Insecure,
					CertFile: cfg.Remote.TLS.Cert,
					KeyFile:  cfg.Remote.TLS.Key,
					CAFile:   cfg.Remote.TLS.CA,
					CacheTTL: cfg.Remote.CacheTTL,
				})
				if err != nil {
					return err
				}
				app.remote = c
				remoteclient.BindProvider(app.st, c)
			} else {
				kv, err := boltkv.Open(cfg.Database.Path)
				if err != nil {
					return err
				}
				app.kv = kv
				boltkv.BindProvider(app.rt, app.st, kv)
			}
			app.Tasks = &tasks.Handler{Runtime: app.rt, Store: app.st}
			app.Tracker = &tracker.Handler{Runtime: app.rt, Store: app.st}
			app.Notes = &notes.Handler{Runtime: app.rt, Store: app.st}
			return nil
		},
		// After any successful command, nudge about a newer release (cached,
		// at most one short-timeout request per day). The TUI has its own
		// banner and `update` reports explicitly. Scripts are spared by the
		// stderr TTY check. Cobra skips PostRun entirely when RunE errors.
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			switch cmd.Name() {
			case "tui", "update", "server", "help", "completion", "__complete":
				return
			}
			if noUpdateCheck || app.JSON || !isTerminal(os.Stderr) {
				return
			}
			update.NotifyIfOutdated(version, cmd.ErrOrStderr())
		},
		// With no subcommand, report what's being tracked.
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := app.Tracker.Status(time.Now())
			if err != nil {
				return err
			}
			if app.JSON {
				if s.State == nil {
					return printJSON(cmd, struct {
						Tracking bool `json:"tracking"`
					}{false})
				}
				return printJSON(cmd, struct {
					Tracking       bool       `json:"tracking"`
					Task           *task.Task `json:"task"`
					StartedAt      time.Time  `json:"started_at"`
					SessionSeconds int64      `json:"session_seconds"`
					Session        string     `json:"session"`
					TotalSeconds   int64      `json:"total_seconds"`
					Total          string     `json:"total"`
				}{true, s.Task, s.State.Start,
					secs(s.Elapsed), formatDuration(s.Elapsed),
					secs(s.Total), formatDuration(s.Total)})
			}
			if s.State == nil {
				cmd.Println("Not tracking")
				return nil
			}
			cmd.Printf("Tracking on %s for %s (current session %s)\n",
				s.State.TaskName, formatDuration(s.Total), formatDuration(s.Elapsed))
			return nil
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to config file (default: first of {ttt,config}.{yaml,yml} in ., ~/.config/ttt, ~/.local/share/ttt, ~/.ttt)")
	root.PersistentFlags().StringVar(&dbPath, "db", "", "path to the database file (overrides config and TTT_DATABASE_PATH; forces local mode)")
	root.PersistentFlags().StringVar(&remoteURL, "remote-url", "", "ttt server URL; when set, commands use the server instead of a local database (overrides config and TTT_REMOTE_URL)")
	root.PersistentFlags().StringVar(&remoteToken, "remote-token", "", "bearer token for the remote server (overrides config and TTT_REMOTE_TOKEN)")
	root.PersistentFlags().BoolVar(&remoteInsecure, "remote-insecure", false, "skip TLS certificate verification for the remote server (self-signed certs)")
	root.PersistentFlags().BoolVar(&noUpdateCheck, "no-update-check", false, "disable the automatic update check (CLI notice and TUI banner)")
	root.PersistentFlags().BoolVar(&app.JSON, "json", false, "output results as JSON (for scripting)")

	root.AddCommand(
		newAddCmd(app),
		newListCmd(app),
		newStartCmd(app),
		newPauseCmd(app),
		newStopCmd(app),
		newNoteCmd(app),
		newShowCmd(app),
		newEditCmd(app),
		newStatsCmd(app),
		newTuiCmd(app, version, &noUpdateCheck),
		newUpdateCmd(app, version),
		newServerCmd(app, version),
	)
	return root
}

// reportImport imports the closed session's commits from the task's linked
// repo and prints the outcome. Git failures are warnings, never command
// failures - tracking state has already changed by the time we get here.
func reportImport(cmd *cobra.Command, app *App, st *trackermodel.State, end time.Time) {
	n, repo, err := app.Tracker.ImportCommits(st, end)
	if err != nil {
		cmd.PrintErrf("warning: import commits: %v\n", err)
		return
	}
	if n > 0 {
		cmd.Printf("Imported %d commit(s) from %s\n", n, repo)
	}
}

// isTerminal reports whether f is attached to a terminal, so update notices
// never leak into pipes or redirected output.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// formatTime renders a timestamp for display, in local time.
func formatTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

// formatDuration renders d as HH:MM:SS. Hours may exceed 24.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
