// GET /api/mcp/servers/{id}/tools/{toolId}/definition - the stored,
// sanitized definition of one catalog tool, for the settings page's
// definition inspector.
package mcpservers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// ToolDefinition answers one tool's stored definition.
func ToolDefinition(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		serverID := r.PathValue("id")
		toolID := r.PathValue("toolId")

		server, ok, fault := readServer(r.Context(), queries, w, serverID)
		if !ok {
			return fault
		}
		row, err := queries.GetTool(r.Context(), toolID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusNotFound,
					fmt.Sprintf("Tool %q not found", toolID))
			}
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read tool %q: %w", toolID, err)
		}
		// A tool belongs to exactly one server: a mismatched path pair is
		// a client error, not a cross-server read.
		if !row.ServerID.Valid || row.ServerID.String != serverID {
			return api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("Tool %q not found on server %q", toolID, serverID))
		}
		headerKeys, err := decodeStrings(server.HeaderKeys)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("decode headerKeys for %q: %w", serverID, err)
		}
		// A NULL url renders an empty endpoint, matching serverView.
		endpoint := ""
		if server.Url.Valid {
			endpoint = maskedURL(server.Url.String)
		}
		// The authority behind the definition: the endpoint (userinfo
		// masked), whether credentials ride it, and the exceptions.
		return api.WriteJSON(w, http.StatusOK, apitypes.McpToolDefinitionResponse{
			DefinitionJSON:      row.DefinitionJson,
			Endpoint:            endpoint,
			HasCredentials:      len(headerKeys) > 0,
			AllowPlaintext:      server.AllowPlaintext != 0,
			AllowPrivateNetwork: server.AllowPrivateNetwork != 0,
		})
	}
}
