package env

import (
	"ttt/internal/database"

	"go.etcd.io/bbolt"
)

// Runtime is the process-scoped bag of dependencies.
// Built once in internal/cli and handed to every domain Handler (config, DB handle, store provider).
type Runtime struct {
	Config *Config

	// DB is the raw bbolt handle for domain-store transactions. StoreProvider is
	// the engine-agnostic surface (bucket names + lifecycle) so stores aren't
	// bound to one backend. The aggregate store.Store is NOT here - it embeds
	// Context{*Runtime}, so it lives in Handler{Runtime, Store} to avoid an env cycle.
	DB            *bbolt.DB
	StoreProvider database.Database
}
