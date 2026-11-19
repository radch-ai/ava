// GET /api/mcp/servers - the installed servers with tool counts and
// diagnostics; secret values never appear.
package mcpservers

import (
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// List answers every server row in creation order, counts joined in —
// one query, no per-row count round-trip.
func List(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		rows, err := queries.ListMcpServersWithCounts(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("list mcp servers: %w", err)
		}
		out := apitypes.GetMcpServersResponse{Servers: make([]apitypes.McpServer, 0, len(rows))}
		for _, row := range rows {
			server, err := serverView(row.McpServer, row.ToolCount)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("render server list: %w", err)
			}
			out.Servers = append(out.Servers, server)
		}
		return api.WriteJSON(w, http.StatusOK, out)
	}
}
