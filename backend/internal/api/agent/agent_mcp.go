// The per-agent MCP gate endpoint: PATCH /agent/{id}/mcp/{serverId}
// flips the parent switch over one server's tools. First enable (no
// gate row) approves the server's currently listed tools in the same
// transaction; every later toggle flips only the gate and keeps the
// agent's per-tool choices.
package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// ToggleAgentMcp flips one agent's parent gate over one MCP server and
// answers the refreshed tool listing. Bursts on the same target draw a
// 429, like the per-tool toggle.
func ToggleAgentMcp(throttle *api.Throttle, sqlDB *sql.DB, queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.ToggleAgentMcpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		if req.Enabled == nil {
			return api.WriteError(w, http.StatusBadRequest, "enabled is required")
		}
		id := r.PathValue("id")
		serverID := r.PathValue("serverId")

		if _, err := queries.GetAgentConfig(r.Context(), id); err != nil {
			return agentLookupErr(w, id, err)
		}
		if _, err := queries.GetMcpServer(r.Context(), serverID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusNotFound,
					fmt.Sprintf("MCP server %q not found", serverID))
			}
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("get mcp server %q: %w", serverID, err)
		}

		key := "agent:" + id + ":mcp:" + serverID
		if !throttle.Allow(key) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Toggling too fast — try again in a moment")
		}

		params := db.SetAgentMcpEnabledParams{
			AgentID:  id,
			ServerID: serverID,
			Enabled:  0,
		}
		if *req.Enabled {
			params.Enabled = 1
		}

		_, err := queries.GetAgentMcp(r.Context(), db.GetAgentMcpParams{
			AgentID: id, ServerID: serverID,
		})
		neverApproved := errors.Is(err, sql.ErrNoRows)
		if err != nil && !neverApproved {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read mcp gate %q/%q: %w", id, serverID, err)
		}

		if neverApproved && *req.Enabled {
			// Approve the server's current tools and the gate in one
			// transaction -- no half-approved state.
			tx, err := sqlDB.BeginTx(r.Context(), nil)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("begin first enable %q/%q: %w", id, serverID, err)
			}
			defer func() { _ = tx.Rollback() }()
			txq := db.New(tx)
			if _, err := txq.SetAgentMcpEnabled(r.Context(), params); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("set mcp gate %q/%q: %w", id, serverID, err)
			}
			if _, err := txq.SeedAgentMcpTools(r.Context(), db.SeedAgentMcpToolsParams{
				AgentID:  id,
				ServerID: sql.NullString{String: serverID, Valid: true},
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("seed mcp tools %q/%q: %w", id, serverID, err)
			}
			if err := tx.Commit(); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("commit first enable %q/%q: %w", id, serverID, err)
			}
		} else {
			// A configured gate only flips; choices stay.
			if _, err := queries.SetAgentMcpEnabled(r.Context(), params); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("set mcp gate %q/%q: %w", id, serverID, err)
			}
		}

		return writeAgentTools(w, r, queries, id)
	}
}
