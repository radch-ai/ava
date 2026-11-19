// Package mcpserver syncs MCP server rows into the tool catalog:
// connect, discover, upsert, prune, diagnostics. Boot and the API's
// create/patch/refresh paths both write through it, so catalog state
// is identical wherever it starts. A server that is down degrades to
// an inert listing.
package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// Kind is the vault kind of an MCP server's secrets entry.
const Kind = "mcp-server"

// Secrets is the sealed blob under runtime/{EntryName}: one entry per
// server, env and headers together.
type Secrets struct {
	Env     map[string]string `json:"env"`
	Headers map[string]string `json:"headers"`
}

// EntryName is the runtime-vault entry holding a server's secrets.
func EntryName(slug string) string { return "mcp-server/" + slug }

// bootDiscoveryParallel bounds concurrent boot dials: one hung or slow
// server costs its own handshake timeout, never a serial queue of them.
const bootDiscoveryParallel = 4

// SyncEnabled runs discovery for every enabled server at boot, bounded
// parallel. Only a broken catalog read fails the boot; per-server
// failures land in the row's diagnostics.
func SyncEnabled(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, vaultStore *vault.Store, logger zerolog.Logger) error {
	servers, err := queries.ListMcpServers(ctx)
	if err != nil {
		return fmt.Errorf("list mcp servers: %w", err)
	}
	var group errgroup.Group
	group.SetLimit(bootDiscoveryParallel)
	for _, server := range servers {
		if server.Enabled == 0 {
			// Kill switch: tools stay listed but inert, and this boot
			// does not touch the row's diagnostics.
			continue
		}
		group.Go(func() error {
			if _, err := Discover(ctx, sqlDB, queries, manager, vaultStore, server); err != nil {
				logger.Warn().Err(err).Str("server", server.ID).Msg("mcp discovery failed")
			}
			return nil
		})
	}
	return group.Wait()
}

// Discover connects one server (cached when unchanged), syncs its
// listing into the catalog, prunes rows it no longer reports — grants
// cascade away with them — and records diagnostics. Errors are
// returned for the caller to answer or log; the row always explains
// itself.
func Discover(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, vaultStore *vault.Store, server db.McpServer) ([]mcp.ListedTool, error) {
	listed, err := discover(ctx, sqlDB, queries, manager, vaultStore, server)
	if err != nil {
		if diagErr := SetDiagnostics(ctx, queries, server.ID, err); diagErr != nil {
			return nil, errors.Join(err, diagErr)
		}
		return nil, err
	}
	return listed, SetDiagnostics(ctx, queries, server.ID, nil)
}

func discover(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, vaultStore *vault.Store, server db.McpServer) ([]mcp.ListedTool, error) {
	cfg, err := Config(ctx, server, vaultStore)
	if err != nil {
		return nil, err
	}
	client, err := manager.Connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	listed, _, err := client.Sync(ctx)
	if err != nil {
		return nil, err
	}
	return listed, UpsertListing(ctx, sqlDB, server.ID, listed)
}

// UpsertListing writes one server's listing into the catalog as
// kind='mcp' rows and prunes the rest — one transaction, so readers
// (the catalog page, a turn's tool resolution) never see a torn
// listing mid-sync.
func UpsertListing(ctx context.Context, sqlDB *sql.DB, serverID string, listed []mcp.ListedTool) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin listing sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	queries := db.New(tx)

	kept := make([]string, 0, len(listed))
	for _, t := range listed {
		id := mcp.CatalogID(serverID, t.Remote)
		if err := queries.UpsertMcpTool(ctx, db.UpsertMcpToolParams{
			ID:          id,
			Name:        t.Remote,
			Description: t.Description,
			ServerID:    serverID,
		}); err != nil {
			return fmt.Errorf("upsert tool %q: %w", id, err)
		}
		kept = append(kept, id)
	}
	keptJSON, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("encode kept ids: %w", err)
	}
	if _, err := queries.DeleteVanishedMcpTools(ctx, db.DeleteVanishedMcpToolsParams{
		ServerID: serverID,
		KeptIds:  string(keptJSON),
	}); err != nil {
		return fmt.Errorf("prune vanished tools: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit listing sync: %w", err)
	}
	return nil
}

// Config resolves one server row into a connection config, unsealing
// the secrets blob. A missing blob is an honestly empty config, not an
// error: servers without secrets never seal one.
func Config(ctx context.Context, server db.McpServer, vaultStore *vault.Store) (mcp.ServerConfig, error) {
	cfg := mcp.ServerConfig{
		ID:        server.ID,
		Transport: server.Transport,
	}
	if server.Command.Valid {
		cfg.Command = server.Command.String
	}
	if server.Url.Valid {
		cfg.URL = server.Url.String
	}
	if err := json.Unmarshal([]byte(server.Args), &cfg.Args); err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("decode args %q: %w", server.Args, err)
	}
	if server.TimeoutMs.Valid && server.TimeoutMs.Int64 > 0 {
		cfg.Timeout = time.Duration(server.TimeoutMs.Int64) * time.Millisecond
	}

	blob, _, err := vaultStore.Get(ctx, vault.NameRuntime, EntryName(server.ID))
	if errors.Is(err, vault.ErrNotFound) {
		return cfg, nil
	}
	if err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("unseal secrets: %w", err)
	}
	var secrets Secrets
	if err := json.Unmarshal(blob, &secrets); err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("decode secrets: %w", err)
	}
	cfg.Env, cfg.Headers = secrets.Env, secrets.Headers
	return cfg, nil
}

// SetDiagnostics records the post-discovery state: the failure cause
// (NULL = healthy) and when the sync attempt ran.
func SetDiagnostics(ctx context.Context, queries *db.Queries, id string, lastErr error) error {
	params := db.SetMcpServerDiagnosticsParams{
		ID:          id,
		RefreshedAt: sql.NullInt64{Int64: time.Now().UnixMicro(), Valid: true},
	}
	if lastErr != nil {
		params.LastError = sql.NullString{String: lastErr.Error(), Valid: true}
	}
	return queries.SetMcpServerDiagnostics(ctx, params)
}
