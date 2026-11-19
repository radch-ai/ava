// POST /api/mcp/servers - install a server: verify the connection
// against the real (unsealed) config before anything is written, then
// save the row, seal the secrets blob, and sync the catalog.
package mcpservers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/names"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// Create installs one MCP server. A candidate that fails to connect,
// initialize, or fit the provider name contract writes nothing.
func Create(manager *mcp.Manager, sqlDB *sql.DB, queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.CreateMcpServerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		if !names.Valid(req.Name) {
			return api.WriteError(w, http.StatusBadRequest,
				"Invalid server name: use letters, digits and single spaces only")
		}
		if msg := configProblem(req.Transport, req.Command, req.URL); msg != "" {
			return api.WriteError(w, http.StatusBadRequest, msg)
		}
		if msg := timeoutProblem(req.TimeoutMs); msg != "" {
			return api.WriteError(w, http.StatusBadRequest, msg)
		}

		slug := names.Slug(req.Name)
		if _, err := queries.GetMcpServer(r.Context(), slug); err == nil {
			return api.WriteError(w, http.StatusConflict,
				fmt.Sprintf("An MCP server named %q already exists", req.Name))
		} else if !errors.Is(err, sql.ErrNoRows) {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("server id collision check: %w", err)
		}

		cfg := mcp.ServerConfig{
			ID:        slug,
			Transport: req.Transport,
			Command:   req.Command,
			Args:      req.Args,
			URL:       req.URL,
			Env:       req.Env,
			Headers:   req.Headers,
		}
		if req.TimeoutMs != nil {
			cfg.Timeout = msDuration(*req.TimeoutMs)
		}
		client, listed, err := verify(r.Context(), cfg)
		if err != nil {
			return api.WriteError(w, http.StatusBadRequest,
				"Connection check failed: "+err.Error())
		}
		// The verified process serves the live server too: every failure
		// before adoption closes it; adoption transfers ownership.
		adopted := false
		defer func() {
			if !adopted {
				_ = client.Close()
			}
		}()

		command := sql.NullString{}
		if req.Transport == mcp.TransportStdio {
			command = sql.NullString{String: req.Command, Valid: true}
		}
		rawURL := sql.NullString{}
		if req.Transport == mcp.TransportHTTP {
			rawURL = sql.NullString{String: req.URL, Valid: true}
		}
		timeout := sql.NullInt64{}
		if req.TimeoutMs != nil {
			timeout = sql.NullInt64{Int64: *req.TimeoutMs, Valid: true}
		}
		argsJSON, _ := json.Marshal(req.Args)
		if err := queries.CreateMcpServer(r.Context(), db.CreateMcpServerParams{
			ID:         slug,
			Name:       strings.TrimSpace(req.Name),
			Transport:  req.Transport,
			Command:    command,
			Args:       string(argsJSON),
			Url:        rawURL,
			EnvKeys:    encodeKeyNames(req.Env),
			HeaderKeys: encodeKeyNames(req.Headers),
			TimeoutMs:  timeout,
		}); err != nil {
			// Two installs of the same name race past the pre-check; the
			// loser's insert hits the primary-key constraint and must read
			// as the same 409, never a 500.
			if isDuplicateSlug(err) {
				return api.WriteError(w, http.StatusConflict,
					fmt.Sprintf("An MCP server named %q already exists", req.Name))
			}
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("insert mcp server %q: %w", slug, err)
		}

		if len(req.Env) > 0 || len(req.Headers) > 0 {
			if err := sealSecrets(r.Context(), vaultStore, slug, mcpserver.Secrets{
				Env:     req.Env,
				Headers: req.Headers,
			}); err != nil {
				// The row without its secrets is unusable and blocks the
				// name; nothing was adopted and Put cannot have partially
				// written, so those rollback arms are no-ops.
				rollback := rollbackInstall(r.Context(), queries, vaultStore, manager, slug)
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return errors.Join(fmt.Errorf("seal secrets for %q: %w", slug, err), rollback)
			}
		}
		// The verified client becomes the live server's client: the
		// manager owns it from here, and discovery below reuses it
		// instead of spawning a second process.
		manager.Adopt(cfg, client)
		adopted = true

		// Catalog sync through the live manager: upsert, prune,
		// diagnostics.
		row, err := queries.GetMcpServer(r.Context(), slug)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read back mcp server %q: %w", slug, err)
		}
		discoverCtx, cancel := requestContext(r.Context())
		_, err = mcpserver.Discover(discoverCtx, sqlDB, queries, manager, vaultStore, row)
		cancel()
		if err != nil {
			// The row, its secrets, and the adopted client are written; a
			// retry would collide with them. Roll all three back so a failed
			// install leaves nothing behind and a clean retry is possible.
			rollback := rollbackInstall(r.Context(), queries, vaultStore, manager, slug)
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return errors.Join(fmt.Errorf("discover mcp server %q: %w", slug, err), rollback)
		}

		server, err := buildServer(r.Context(), queries, row)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("build server %q: %w", slug, err)
		}
		return api.WriteJSON(w, http.StatusCreated, apitypes.McpServerWithToolsResponse{
			Server: server,
			Tools:  discoveredTools(slug, listed),
		})
	}
}

// rollbackInstall undoes a partially-written install: the live client,
// the row (cascading its catalog tools and grants), and the sealed
// blob. Best effort — each arm's failure is reported, never masking
// the original error. Unknown ids are no-ops, so arms for things a
// failure path never wrote stay silent.
func rollbackInstall(ctx context.Context, queries *db.Queries, vaultStore *vault.Store, manager *mcp.Manager, slug string) error {
	manager.Close(slug)
	var rollback error
	if _, err := queries.DeleteMcpServer(ctx, slug); err != nil {
		rollback = fmt.Errorf("rollback row: %w", err)
	}
	if err := vaultStore.Delete(ctx, vault.NameRuntime, mcpserver.EntryName(slug)); err != nil && !errors.Is(err, vault.ErrNotFound) {
		rollback = errors.Join(rollback, fmt.Errorf("rollback secrets: %w", err))
	}
	return rollback
}
