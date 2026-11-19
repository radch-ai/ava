// POST /api/mcp/servers/{id}/refresh - reconnect and re-discover:
// upsert the server's current listing, prune vanished tools, update
// diagnostics. A dead server answers 502 with its cause; the row's
// last_error says the same thing for later.
package mcpservers

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// Refresh re-runs discovery for one enabled server.
func Refresh(throttle *api.Throttle, manager *mcp.Manager, sqlDB *sql.DB, queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		row, err := readServer(r.Context(), queries, w, id)
		if err != nil {
			return err
		}
		if !throttle.Allow("mcp:" + id) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Refreshing too fast — try again in a moment")
		}
		if row.Enabled == 0 {
			return api.WriteError(w, http.StatusBadRequest,
				"Server is disabled — enable it before refreshing")
		}

		discoverCtx, cancel := requestContext(r.Context())
		listed, err := mcpserver.Discover(discoverCtx, sqlDB, queries, manager, vaultStore, row)
		cancel()
		if err != nil {
			return api.WriteError(w, http.StatusBadGateway,
				"Connection check failed: "+err.Error())
		}

		saved, err := queries.GetMcpServer(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read back mcp server %q: %w", id, err)
		}
		server, err := buildServer(r.Context(), queries, saved)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("build server %q: %w", id, err)
		}
		return api.WriteJSON(w, http.StatusOK, apitypes.McpServerWithToolsResponse{
			Server: server,
			Tools:  discoveredTools(id, listed),
		})
	}
}
