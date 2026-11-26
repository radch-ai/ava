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
	mcpservers "github.com/radch-ai/ava/backend/internal/api/mcpservers"
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/api/tools"
	"github.com/radch-ai/ava/backend/internal/api/vaults"
	"github.com/radch-ai/ava/backend/internal/db"
	mcpserver "github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/modelcatalog"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
	toolreg "github.com/radch-ai/ava/backend/internal/tools"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// ModelCatalog is the models.dev context-window snapshot the resolver's
// last tier reads. The binary builds one at boot; nil knows nothing.
type ModelCatalog = modelcatalog.Catalog

// LoadModelCatalog builds the context-window catalog: the embedded
// snapshot, fetched fresh from url when one is configured and cached at
// cachePath. Only a broken embedded snapshot fails.
func LoadModelCatalog(url, cachePath string, logger zerolog.Logger) (*ModelCatalog, error) {
	return modelcatalog.Load(url, cachePath, logger)
}

// New builds the backend's route table over an already-open,
// already-migrated database: agents, heartbeat and cron jobs,
// providers, user secrets, and the tool catalog.
//
// New builds the registry's built-ins and syncs them into the tools
// table: missing entries are created with their defaults, code-owned
// metadata is refreshed, and saved global switches are never
// overwritten. A built-in that fails to construct fails here, at boot;
// MCP server discovery is best-effort per server — a down server
// degrades to inert listings with its diagnostics recorded, never a
// boot failure.
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
// limits is the admission budget: operator-set starting defaults,
// never model- or server-influenced.
//
// Wire types live in backend/apitypes — the UI's TypeScript types are
// generated from it (`just typegen`).
//
// catalog is the vendored models.dev snapshot the resolver falls back
// to; nil disables that tier.
func New(sqlDB *sql.DB, logger zerolog.Logger, masterKey []byte, limits ToolLimits, catalog *ModelCatalog) (http.Handler, auth.Sessions, *TurnService, error) {
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
	// Migration 0012 guarantees the singleton history row exists, so a
	// read failure here is a broken database, not an empty one.
	historyID, err := queries.GetSyncHistory(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read sync history: %w", err)
	}
	// The invocation boundary: one admitter and one cancellation
	// registry per process, shared by every turn.
	registry2 := tooladmission.NewCancelRegistry()
	admitter := tooladmission.New(queries, limits, registry2, logger)
	turns := &TurnService{
		queries:         queries,
		sqlDB:           sqlDB,
		vaultStore:      vaultStore,
		registry:        registry,
		logger:          logger,
		admitter:        admitter,
		catalog:         catalog,
		lastPersistedMs: watermark,
		historyID:       historyID,
	}
	if err := turns.recoverUnfinished(context.Background()); err != nil {
		return nil, nil, nil, err
	}
	// Retention must expire by age without waiting for traffic. Each
	// later boot step stops the ticker on its failure — a rejected boot
	// leaks nothing.
	turns.stopPrune = admitter.StartPrune(time.Hour)
	if err := syncToolCatalog(queries, registry.Catalog()); err != nil {
		turns.stopPrune()
		return nil, nil, nil, err
	}
	// MCP discovery is best-effort per server: a down server's
	// tools stay listed but inert; boot never fails on it.
	if err := mcpserver.SyncEnabled(context.Background(), sqlDB, queries, registry.MCP(), vaultStore, logger); err != nil {
		turns.stopPrune()
		return nil, nil, nil, err
	}
	// Toggle endpoints absorb bursts: one accepted write per target
	// per window — human pace never trips it.
	toggleThrottle := api.NewThrottle(200 * time.Millisecond)
	mux := http.NewServeMux()

	// Agents.
	mux.Handle("GET /agents", api.Log(logger, agents.GetAgents(queries)))
	mux.Handle("POST /agent", api.Log(logger, agent.CreateAgent(sqlDB, queries)))
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
	mux.Handle("PATCH /agent/{id}/mcp/{serverId}",
		api.Log(logger, agent.ToggleAgentMcp(toggleThrottle, sqlDB, queries, turns)))
	mux.Handle("PATCH /agent/{id}/tools/{toolId}",
		api.Log(logger, agent.ToggleAgentTool(toggleThrottle, sqlDB, queries, turns)))

	// Tool catalog.
	mux.Handle("GET /tools", api.Log(logger, tools.GetTools(queries)))
	mux.Handle("PATCH /tools/{id}", api.Log(logger, tools.ToggleTool(toggleThrottle, sqlDB, queries, turns)))

	// MCP servers: CRUD + refresh; writes throttled per server.
	mcpThrottle := api.NewThrottle(500 * time.Millisecond)
	mux.Handle("GET /mcp/servers", api.Log(logger, mcpservers.List(queries)))
	mux.Handle("POST /mcp/servers", api.Log(logger, mcpservers.Create(registry.MCP(), sqlDB, queries, vaultStore)))
	mux.Handle("PATCH /mcp/servers/{id}",
		api.Log(logger, mcpservers.Update(mcpThrottle, registry.MCP(), sqlDB, queries, vaultStore, turns, logger)))
	mux.Handle("DELETE /mcp/servers/{id}",
		api.Log(logger, mcpservers.Delete(mcpThrottle, registry.MCP(), queries, vaultStore, turns)))
	mux.Handle("POST /mcp/servers/{id}/refresh",
		api.Log(logger, mcpservers.Refresh(mcpThrottle, registry.MCP(), sqlDB, queries, vaultStore, turns, logger)))
	mux.Handle("GET /mcp/servers/{id}/tools/{toolId}/definition",
		api.Log(logger, mcpservers.ToolDefinition(queries)))

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
