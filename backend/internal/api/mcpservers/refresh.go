// POST /api/mcp/servers/{id}/refresh - re-discover: upsert the server's
// current listing, prune vanished tools, update diagnostics. The healthy
// cached connection is reused, so a refresh never interrupts calls
// already on the wire; only a broken connection is replaced. A dead
// server answers 502 with its cause; the row's last_error says the same
// thing for later.
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
func Refresh(throttle *api.Throttle, manager *mcp.Manager, sqlDB *sql.DB, queries *db.Queries, vaultStore *vault.Store, canceler api.Canceler) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		row, ok, fault := readServer(r.Context(), queries, w, id)
		if !ok {
			return fault
		}
		if !throttle.Allow("mcp:" + id) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Refreshing too fast — try again in a moment")
		}
		if row.Enabled == 0 {
			return api.WriteError(w, http.StatusBadRequest,
				"Server is disabled — enable it before refreshing")
		}

		// The expected survivors before the publish: only the tools that
		// vanished have their in-flight calls canceled.
		before := toolIDs(r.Context(), queries, id)
		discoverCtx, cancel := requestContext(r.Context())
		listed, skipped, err := mcpserver.Discover(discoverCtx, sqlDB, queries, manager, vaultStore, row)
		cancel()
		if err != nil {
			return api.WriteError(w, http.StatusBadGateway,
				"Connection check failed: "+err.Error())
		}

		cancelRemovedTools(canceler, id, before, keptFromListing(id, listed))

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
			Server:       server,
			Tools:        discoveredTools(id, listed),
			SkippedTools: skippedTools(skipped),
		})
	}
}
