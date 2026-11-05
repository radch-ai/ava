// Package backend is the backend's public surface: internal/ is
// invisible outside this subtree, so this package and sqlstore are the
// importable entry points.
package backend

import (
	"database/sql"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/api/agent"
	"github.com/radch-ai/ava/backend/internal/api/agents"
	"github.com/radch-ai/ava/backend/internal/db"
)

// New builds the backend route table over an already-open,
// already-migrated database; the binary entrypoint mounts it under
// /api on its root mux. Wire types live in backend/apitypes — the
// UI's TypeScript types are generated from it (`just typegen`);
func New(sqlDB *sql.DB, logger zerolog.Logger) http.Handler {
	queries := db.New(sqlDB)
	mux := http.NewServeMux()
	mux.Handle("GET /api/agents", api.Log(logger, agents.GetAgents(queries)))
	mux.Handle("POST /api/agent", api.Log(logger, agent.CreateAgent(queries)))
	return mux
}
