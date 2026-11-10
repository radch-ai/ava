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
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// New builds the backend route table over an already-open,
// already-migrated database; the binary entrypoint mounts it under
// /api on its root mux.
//
// masterKey unlocks the secret vault: a key that does not unwrap the
// stored keyset fails here, at boot.
//
// Wire types live in backend/apitypes — the UI's TypeScript types are
// generated from it (`just typegen`).
func New(sqlDB *sql.DB, logger zerolog.Logger, masterKey []byte) (http.Handler, error) {
	master, err := vault.NewMasterAEAD(masterKey)
	if err != nil {
		return nil, err
	}
	// Unwrapping the keyset is the boot check: a wrong master key
	// cannot open it.
	if _, err := vault.NewStore(sqlDB, master); err != nil {
		return nil, err
	}
	queries := db.New(sqlDB)
	mux := http.NewServeMux()
	mux.Handle("GET /api/agents", api.Log(logger, agents.GetAgents(queries)))
	mux.Handle("POST /api/agent", api.Log(logger, agent.CreateAgent(queries)))
	mux.Handle("GET /api/agent/{id}/config", api.Log(logger, agent.GetAgentConfig(queries)))
	mux.Handle("PATCH /api/agent/{id}/config", api.Log(logger, agent.UpdateAgentConfig(queries)))
	mux.Handle("PATCH /api/agent/{id}/heartbeat", api.Log(logger, agent.UpdateHeartbeat(queries)))
	mux.Handle("GET /api/agent/{id}/cron-jobs", api.Log(logger, agent.GetCronJobs(queries)))
	mux.Handle("POST /api/agent/{id}/cron-jobs", api.Log(logger, agent.CreateCronJob(queries)))
	mux.Handle("PATCH /api/agent/{id}/cron-jobs/{jobId}", api.Log(logger, agent.UpdateCronJob(queries)))
	mux.Handle("DELETE /api/agent/{id}/cron-jobs/{jobId}", api.Log(logger, agent.DeleteCronJob(queries)))
	mux.Handle("GET /api/providers", api.Log(logger, providers.GetProviders(queries)))
	mux.Handle("GET /api/models", api.Log(logger, providers.GetModels(queries)))
	mux.Handle("GET /api/providers/{id}/models", api.Log(logger, providers.GetProviderModels(queries)))
	mux.Handle("POST /api/providers", api.Log(logger, providers.CreateProvider(queries)))
	mux.Handle("PATCH /api/providers/{id}", api.Log(logger, providers.UpdateProvider(queries)))
	mux.Handle("DELETE /api/providers/{id}", api.Log(logger, providers.DeleteProvider(queries)))
	return mux, nil
}
