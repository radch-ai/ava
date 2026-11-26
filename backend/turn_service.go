package backend

import (
	"database/sql"
	"sync"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/modelcatalog"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
	"github.com/radch-ai/ava/backend/internal/tools"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// TurnService serves the gateway's per-turn needs: model resolution,
// agent config reads, tool permissions, thread persistence, thread
// lifecycle, and the durable conversation lifecycle journal. New builds
// the one instance at boot, over the same queries and vault store the
// routes use.
type TurnService struct {
	queries    *db.Queries
	sqlDB      *sql.DB
	vaultStore *vault.Store
	registry   *tools.Registry
	logger     zerolog.Logger
	admitter   *tooladmission.Admitter
	// catalog is the last tier of context-window resolution: the
	// vendored models.dev snapshot. nil knows nothing.
	catalog *modelcatalog.Catalog
	// stopPrune ends the audit retention ticker on shutdown.
	stopPrune func()

	// Journal writer state — see item_journal.go. The mutex serializes
	// message batches, item transitions, thread switches, and retention,
	// and owns the monotonic timestamp counter.
	persistMu       sync.Mutex
	lastPersistedMs int64
	// historyID is this database's lifecycle-history identity, read once
	// at boot: it stamps every cursor, and a cursor minted by another
	// database cannot resume here.
	historyID string
	// appendCounts counts events appended per agent since the last
	// retention pass; guarded by persistMu.
	appendCounts map[string]int
}
