// Package backend is the backend's public surface: internal/ is
// invisible outside this subtree, so this package and sqlstore are the
// importable entry points.
package backend

import (
	"database/sql"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/auth"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/api/agent"
	"github.com/radch-ai/ava/backend/internal/api/agents"
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/api/vaults"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// New builds the backend's route table over an already-open,
// already-migrated database: agents, heartbeat and cron jobs,
// providers, and user secrets.
//
// It also returns the vault-backed session store (auth.NewVaultSessions)
// the binary's auth layer consumes.
//
// Routes are mount-relative and carry no auth of their own: the
// binary wires the backend/auth middleware around them, mounting
// under /api with the prefix stripped.
//
// masterKey unlocks the secret vault: a key that does not unwrap the
// stored keyset fails here, at boot.
//
// Wire types live in backend/apitypes — the UI's TypeScript types are
// generated from it (`just typegen`).
func New(sqlDB *sql.DB, logger zerolog.Logger, masterKey []byte) (http.Handler, auth.Sessions, error) {
	master, err := vault.NewMasterAEAD(masterKey)
	if err != nil {
		return nil, nil, err
	}
	vaultStore, err := vault.NewStore(sqlDB, master)
	if err != nil {
		return nil, nil, err
	}
	queries := db.New(sqlDB)
	mux := http.NewServeMux()

	// Agents.
	mux.Handle("GET /agents", api.Log(logger, agents.GetAgents(queries)))
	mux.Handle("POST /agent", api.Log(logger, agent.CreateAgent(queries)))
	mux.Handle("GET /agent/{id}/config", api.Log(logger, agent.GetAgentConfig(queries)))
	mux.Handle("PATCH /agent/{id}/config", api.Log(logger, agent.UpdateAgentConfig(queries)))

	// Heartbeat and cron jobs.
	mux.Handle("PATCH /agent/{id}/heartbeat", api.Log(logger, agent.UpdateHeartbeat(queries)))
	mux.Handle("GET /agent/{id}/cron-jobs", api.Log(logger, agent.GetCronJobs(queries)))
	mux.Handle("POST /agent/{id}/cron-jobs", api.Log(logger, agent.CreateCronJob(queries)))
	mux.Handle("PATCH /agent/{id}/cron-jobs/{jobId}",
		api.Log(logger, agent.UpdateCronJob(queries)))
	mux.Handle("DELETE /agent/{id}/cron-jobs/{jobId}",
		api.Log(logger, agent.DeleteCronJob(queries)))

	// Providers.
	mux.Handle("GET /providers", api.Log(logger, providers.GetProviders(queries, vaultStore)))
	mux.Handle("GET /models", api.Log(logger, providers.GetModels(queries, vaultStore)))
	mux.Handle("GET /providers/{id}/models",
		api.Log(logger, providers.GetProviderModels(queries, vaultStore)))
	mux.Handle("POST /providers", api.Log(logger, providers.CreateProvider(queries, vaultStore)))
	mux.Handle("PATCH /providers/{id}",
		api.Log(logger, providers.UpdateProvider(queries, vaultStore)))
	mux.Handle("DELETE /providers/{id}",
		api.Log(logger, providers.DeleteProvider(queries, vaultStore)))

	// User secrets.
	mux.Handle("GET /vault", api.Log(logger, vaults.GetVault(vaultStore)))
	mux.Handle("PUT /vault/{name}", api.Log(logger, vaults.PutSecret(vaultStore)))
	mux.Handle("DELETE /vault/{name}", api.Log(logger, vaults.DeleteSecret(vaultStore)))

	return mux, auth.NewVaultSessions(vaultStore), nil
}
