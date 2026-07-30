package env_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ttt/internal/core/env"
)

// isolate points HOME (and clears XDG vars) at a temp dir and chdirs into a
// fresh workdir inside it, so tests never see the developer's real config.
func isolate(t *testing.T) (home, work string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("TTT_DATABASE_PATH", "")
	os.Unsetenv("TTT_DATABASE_PATH")
	work = filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	return home, work
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaults(t *testing.T) {
	home, _ := isolate(t)
	c, err := env.Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "ttt", "ttt.db")
	if c.Database.Path != want {
		t.Fatalf("default db path = %q, want %q", c.Database.Path, want)
	}
	// The default must decode into a non-zero duration - a decode failure
	// would silently disable the remote read cache (0 means off).
	if c.Remote.CacheTTL != 10*time.Second {
		t.Fatalf("default remote.cache_ttl = %v, want 10s", c.Remote.CacheTTL)
	}
}

func TestLoadExplicitPath(t *testing.T) {
	home, _ := isolate(t)
	cfg := filepath.Join(home, "elsewhere", "my.yaml")
	write(t, cfg, "database:\n  path: /tmp/explicit.db\n")

	c, err := env.Load(cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Database.Path != "/tmp/explicit.db" {
		t.Fatalf("db path = %q", c.Database.Path)
	}

	if _, err := env.Load(filepath.Join(home, "missing.yaml")); err == nil {
		t.Fatal("expected error for missing explicit config")
	}
}

func TestLoadSearchOrder(t *testing.T) {
	home, work := isolate(t)
	// Lower-priority XDG config...
	write(t, filepath.Join(home, ".config", "ttt", "config.yaml"), "database:\n  path: /tmp/xdg.db\n")
	c, err := env.Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Database.Path != "/tmp/xdg.db" {
		t.Fatalf("db path = %q, want XDG config value", c.Database.Path)
	}

	// ...is beaten by ttt.yaml in the current directory.
	write(t, filepath.Join(work, "ttt.yaml"), "database:\n  path: /tmp/cwd.db\n")
	c, err = env.Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Database.Path != "/tmp/cwd.db" {
		t.Fatalf("db path = %q, want cwd value", c.Database.Path)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	_, work := isolate(t)
	write(t, filepath.Join(work, "ttt.yaml"), "database:\n  path: /tmp/file.db\n")
	t.Setenv("TTT_DATABASE_PATH", "/tmp/env.db")

	c, err := env.Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Database.Path != "/tmp/env.db" {
		t.Fatalf("db path = %q, want env value", c.Database.Path)
	}
}

func TestLoadIgnoresExtensionlessBinary(t *testing.T) {
	_, work := isolate(t)
	// A non-YAML file named like the binary must not be picked up as config.
	write(t, filepath.Join(work, "ttt"), "\x7fELF not yaml")

	c, err := env.Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Database.Path == "" {
		t.Fatal("expected default db path")
	}
}
