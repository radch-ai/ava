// DELETE /api/mcp/servers/{id} - remove a server: its catalog rows
// and their grants cascade away, the secrets blob goes, the client
// (stdio process group included) is torn down.
package mcpservers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// Delete removes one server and everything it owns.
func Delete(throttle *api.Throttle, manager *mcp.Manager, queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		if _, err := readServer(r.Context(), queries, w, id); err != nil {
			return err
		}
		if !throttle.Allow("mcp:" + id) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Deleting too fast — try again in a moment")
		}

		// The secrets entry goes first: a failed row delete then
		// leaves a secrets-less server (re-enterable), never a stale
		// sealed blob.
		if err := vaultStore.Delete(r.Context(), vault.NameRuntime,
			mcpserver.EntryName(id)); err != nil && !errors.Is(err, vault.ErrNotFound) {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("delete secrets for %q: %w", id, err)
		}
		// Row delete cascades the server's tools and their grants.
		if _, err := queries.DeleteMcpServer(r.Context(), id); err != nil {
			return fmt.Errorf("delete mcp server %q: %w", id, err)
		}

		manager.Close(id)
		return api.WriteJSON(w, http.StatusOK,
			apitypes.DeleteMcpServerResponse{Message: "MCP server deleted"})
	}
}
