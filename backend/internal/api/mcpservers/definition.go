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
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
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
		headerKeys, err := mcpserver.DecodeStrings(server.HeaderKeys)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("decode headerKeys for %q: %w", serverID, err)
		}
		envKeys, err := mcpserver.DecodeStrings(server.EnvKeys)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("decode envKeys for %q: %w", serverID, err)
		}
		// The authority behind the definition: the endpoint (userinfo
		// masked) or the command, whether credentials ride it, and the
		// exceptions — plaintext and private-network are endpoint facts,
		// so a stdio row reports neither.
		out := apitypes.McpToolDefinitionResponse{
			DefinitionJSON: row.DefinitionJson,
			HasCredentials: len(headerKeys) > 0 || len(envKeys) > 0,
		}
		if server.Transport == mcp.TransportStdio {
			out.Command = server.Command.String
		} else {
			if server.Url.Valid {
				out.Endpoint = maskedURL(server.Url.String)
			}
			out.AllowPlaintext = server.AllowPlaintext != 0
			out.AllowPrivateNetwork = server.AllowPrivateNetwork != 0
		}
		return api.WriteJSON(w, http.StatusOK, out)
	}
}
