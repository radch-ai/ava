package backend

import (
	"sync"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// TurnService serves the gateway's per-turn needs: model resolution,
// agent config reads, and thread persistence. New builds the one
// instance at boot, over the same queries and vault store the routes
// use.
type TurnService struct {
	queries    *db.Queries
	vaultStore *vault.Store

	// Thread-sink writer state — see AppendThreadMessages.
	persistMu       sync.Mutex
	lastPersistedMs int64
}
