package backend

import (
	"database/sql"
	"sync"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/tools"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// TurnService serves the gateway's per-turn needs: model resolution,
// agent config reads, tool permissions, thread persistence, and thread
// lifecycle (the active pointer and create-and-activate). New builds
// the one instance at boot, over the same queries and vault store the
// routes use.
type TurnService struct {
	queries    *db.Queries
	sqlDB      *sql.DB
	vaultStore *vault.Store
	registry   *tools.Registry

	// Thread-sink writer state — see AppendThreadMessages. The mutex
	// serializes batches and owns the monotonic timestamp counter.
	persistMu       sync.Mutex
	lastPersistedMs int64
}
