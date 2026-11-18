// Package backend is the backend's public surface: internal/ is
// invisible outside this subtree, so this package and sqlstore are the
// importable entry points.
package backend

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/auth"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/api/agent"
	"github.com/radch-ai/ava/backend/internal/api/agents"
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/api/tools"
	"github.com/radch-ai/ava/backend/internal/api/vaults"
	"github.com/radch-ai/ava/backend/internal/db"
	toolreg "github.com/radch-ai/ava/backend/internal/tools"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// New builds the backend's route table over an already-open,
// already-migrated database: agents, heartbeat and cron jobs,
// providers, user secrets, and the tool catalog.
//
// New builds the registry's built-ins and syncs them into the tools
// table: missing entries are created with their defaults, code-owned
// metadata is refreshed, and saved global switches are never
// overwritten. A tool that fails to construct fails here, at boot.
//
// It also returns the vault-backed session store (auth.NewVaultSessions)
// the binary's auth layer consumes, and the TurnService whose method
// values the binary binds into the gateway.
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
func New(sqlDB *sql.DB, logger zerolog.Logger, masterKey []byte) (http.Handler, auth.Sessions, *TurnService, error) {
	master, err := vault.NewMasterAEAD(masterKey)
	if err != nil {
		return nil, nil, nil, err
	}
	vaultStore, err := vault.NewStore(sqlDB, master)
	if err != nil {
		return nil, nil, nil, err
	}
	queries := db.New(sqlDB)
	registry, err := toolreg.New()
	if err != nil {
		return nil, nil, nil, err
	}
	// The writer's watermark starts at the highest stored timestamp:
	// a restart into a backwards-corrected clock must never mint
	// created_at values that sort before existing rows.
	watermark, err := queries.MaxThreadCreatedAt(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("seed thread watermark: %w", err)
	}
	turns := &TurnService{
		queries:         queries,
		sqlDB:           sqlDB,
		vaultStore:      vaultStore,
		registry:        registry,
		lastPersistedMs: watermark,
	}
	if err := syncToolCatalog(queries, registry.Catalog()); err != nil {
		return nil, nil, nil, err
	}
	// Toggle endpoints absorb bursts: one accepted write per target
	// per window — human pace never trips it.
	toggleThrottle := api.NewThrottle(200 * time.Millisecond)
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

	// Thread and activity.
	mux.Handle("GET /agent/{id}/thread", api.Log(logger, agent.GetThread(queries)))
	mux.Handle("GET /agent/{id}/activity", api.Log(logger, agent.GetAgentActivity(queries)))

	// Per-agent tool preferences — live toggles, like cron jobs.
	mux.Handle("GET /agent/{id}/tools", api.Log(logger, agent.GetAgentTools(queries)))
	mux.Handle("PATCH /agent/{id}/tools/{toolId}",
		api.Log(logger, agent.ToggleAgentTool(toggleThrottle, queries)))

	// Tool catalog.
	mux.Handle("GET /tools", api.Log(logger, tools.GetTools(queries)))
	mux.Handle("PATCH /tools/{id}", api.Log(logger, tools.ToggleTool(toggleThrottle, queries)))

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

	return mux, auth.NewVaultSessions(vaultStore), turns, nil
}
