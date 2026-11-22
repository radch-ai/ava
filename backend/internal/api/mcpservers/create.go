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
func Create(manager *mcp.Manager, sqlDB *sql.DB, queries *db.Queries, vaultStore *vault.Store, canceler api.Canceler) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.CreateMcpServerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		if !names.Valid(req.Name) {
			return api.WriteError(w, http.StatusBadRequest,
				"Invalid server name: use letters, digits and single spaces only")
		}
		if msg := configProblem(req.URL); msg != "" {
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

		// Policy flags default to deny: absence approves nothing.
		allowPrivate := req.AllowPrivateNetwork != nil && *req.AllowPrivateNetwork
		allowPlain := req.AllowPlaintext != nil && *req.AllowPlaintext
		cfg := mcp.ServerConfig{
			ID:                  slug,
			URL:                 req.URL,
			Headers:             req.Headers,
			AllowPrivateNetwork: allowPrivate,
			AllowPlaintext:      allowPlain,
		}
		if req.TimeoutMs != nil {
			cfg.Timeout = msDuration(*req.TimeoutMs)
		}
		// A new authority is recorded before the credential it
		// implies is ever transmitted — verification included.
		if err := approveAuthority(r.Context(), queries, slug, allowPrivate, allowPlain); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "audit unavailable")
			return fmt.Errorf("audit approvals for %q: %w", slug, err)
		}
		// Nothing installs on a rejection or a defect, so the approvals
		// written above are withdrawn with an explicit record: authority
		// that never activated must not read as granted.
		withdraw := func() error {
			return withdrawAuthority(r.Context(), queries, slug, allowPrivate, allowPlain)
		}
		client, _, _, err := verify(r.Context(), cfg)
		if err != nil {
			return errors.Join(
				api.WriteError(w, http.StatusBadRequest, "Connection check failed: "+err.Error()),
				withdraw())
		}
		// The verified process serves the live server too: every failure
		// before adoption closes it; adoption transfers ownership.
		adopted := false
		defer func() {
			if !adopted {
				_ = client.Close()
			}
		}()

		rawURL := sql.NullString{String: req.URL, Valid: true}
		timeout := sql.NullInt64{}
		if req.TimeoutMs != nil {
			timeout = sql.NullInt64{Int64: *req.TimeoutMs, Valid: true}
		}
		if err := queries.CreateMcpServer(r.Context(), db.CreateMcpServerParams{
			ID:                  slug,
			Name:                strings.TrimSpace(req.Name),
			Url:                 rawURL,
			HeaderKeys:          encodeKeyNames(req.Headers),
			TimeoutMs:           timeout,
			AllowPrivateNetwork: boolInt(allowPrivate),
			AllowPlaintext:      boolInt(allowPlain),
		}); err != nil {
			// Two installs of the same name race past the pre-check; the
			// loser's insert hits the primary-key constraint and must read
			// as the same 409, never a 500. Nothing activated on any
			// arm here, so the approvals written above are withdrawn.
			if isDuplicateSlug(err) {
				return errors.Join(api.WriteError(w, http.StatusConflict,
					fmt.Sprintf("An MCP server named %q already exists", req.Name)), withdraw())
			}
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return errors.Join(fmt.Errorf("insert mcp server %q: %w", slug, err), withdraw())
		}

		if len(req.Headers) > 0 {
			if err := sealSecrets(r.Context(), vaultStore, slug, mcpserver.Secrets{
				Headers: req.Headers,
			}); err != nil {
				// The row without its secrets is unusable and blocks the
				// name; nothing was adopted and Put cannot have partially
				// written, so those rollback arms are no-ops. The
				// approvals never activated either — withdrawn with the row.
				rollback := rollbackInstall(r.Context(), queries, vaultStore, manager, slug)
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return errors.Join(fmt.Errorf("seal secrets for %q: %w", slug, err), rollback, withdraw())
			}
		}
		// The catalog write publishes the listing verification already
		// staged — one listing, no second pass — and installs the same
		// client under the same critical section: the definition and the
		// implementation are one generation.
		row, err := queries.GetMcpServer(r.Context(), slug)
		if err != nil {
			rollback := rollbackInstall(r.Context(), queries, vaultStore, manager, slug)
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return errors.Join(fmt.Errorf("read back mcp server %q: %w", slug, err), rollback, withdraw())
		}
		discoverCtx, cancel := requestContext(r.Context())
		listed, skipped, err := mcpserver.PublishVerified(discoverCtx, sqlDB, queries, manager, row, cfg, client)
		cancel()
		if err != nil {
			// The row and its secrets are written; a retry would collide
			// with them. Roll both back so a failed install leaves nothing
			// behind and a clean retry is possible.
			rollback := rollbackInstall(r.Context(), queries, vaultStore, manager, slug)
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return errors.Join(fmt.Errorf("discover mcp server %q: %w", slug, err), rollback, withdraw())
		}
		adopted = true

		server, err := buildServer(r.Context(), queries, row)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("build server %q: %w", slug, err)
		}
		return api.WriteJSON(w, http.StatusCreated, apitypes.McpServerWithToolsResponse{
			Server:       server,
			Tools:        discoveredTools(slug, listed),
			SkippedTools: skippedTools(skipped),
		})
	}
}

// rollbackInstall undoes a partially-written install: the live client,
// the row (cascading its catalog tools and agent opt-outs), and the sealed
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
