// Package env holds the Viper-backed config loader (Config) and the
// per-process Runtime handed to domain handlers.
package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type Config struct {
	Database Database `mapstructure:"database"`
}

type Database struct {
	Path string `mapstructure:"path"`
}

// configNames are the file names probed in each search directory, in order.
var configNames = []string{"ttt.yaml", "ttt.yml", "config.yaml", "config.yml"}

// Load resolves the configuration with precedence: the explicit path (must
// exist) > the first config file found in the search locations (see findConfigFile) > TTT_*-prefixed env vars > defaults.
// No config file at all is fine - env and defaults still apply.
func Load(path string) (*Config, error) {
	if path == "" {
		path = findConfigFile()
	}

	v := viper.New()
	v.SetEnvPrefix("TTT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// AutomaticEnv only resolves a key on Get, but Unmarshal builds the struct
	// from keys Viper already knows (config file + defaults + explicit binds).
	// A key set only in the env (TTT_*) wouldn't reach the struct without an
	// explicit bind, so bind every key. Keep this list in sync with the structs.
	for _, key := range []string{
		"database.path",
	} {
		_ = v.BindEnv(key)
	}

	v.SetDefault("database.path", defaultDBPath())

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}

	var c Config
	// Env values arrive as strings, so WeaklyTypedInput coerces them to
	// bools/ints (Viper's default hooks already handle slices/durations).
	if err := v.Unmarshal(&c, func(dc *mapstructure.DecoderConfig) {
		dc.WeaklyTypedInput = true
	}); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &c, nil
}

// findConfigFile returns the first existing config file, probing configNames
// in the current directory, then $XDG_CONFIG_HOME/ttt (~/.config/ttt),
// ~/.local/share/ttt, and ~/.ttt. Resolution is manual rather than Viper's
// because Viper expands neither "~" in search paths nor multiple config
// names, and its name matching would treat an extensionless file named "ttt"
// (e.g. the built binary) as config.
func findConfigFile() string {
	dirs := []string{"."}
	if home, err := os.UserHomeDir(); err == nil {
		cfgHome := os.Getenv("XDG_CONFIG_HOME")
		if cfgHome == "" {
			cfgHome = filepath.Join(home, ".config")
		}
		dirs = append(dirs,
			filepath.Join(cfgHome, "ttt"),
			filepath.Join(home, ".local", "share", "ttt"),
			filepath.Join(home, ".ttt"),
		)
	}
	for _, dir := range dirs {
		for _, name := range configNames {
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
	}
	return ""
}

// defaultDBPath places the database in the XDG data dir
// ($XDG_DATA_HOME/ttt or ~/.local/share/ttt) so every working directory
// shares one store, with a cwd-relative fallback only when home is
// unresolvable.
func defaultDBPath() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "ttt.db"
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "ttt", "ttt.db")
}
