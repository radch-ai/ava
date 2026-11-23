// The per-agent MCP gate endpoint: PATCH /agent/{id}/mcp/{serverId}
// flips the parent switch over one server's tools. The gate decides
// whether the agent uses the server's globally enabled tools; it never
// seeds or revokes a per-tool choice, so the agent's opt-outs survive
// every gate toggle.
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
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// ToggleAgentMcp flips one agent's parent gate over one MCP server and
// answers the refreshed tool listing. Bursts on the same target draw a
// 429, like the per-tool toggle.
func ToggleAgentMcp(throttle *api.Throttle, sqlDB *sql.DB, queries *db.Queries, canceler api.Canceler) api.Handler {
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

		outcome, reason := "revoke", "mcp_disabled"
		if *req.Enabled {
			outcome, reason = "approve", "mcp_enabled"
		}
		auditErr, err := api.ApplyToggle(r.Context(), sqlDB, queries, *req.Enabled,
			tooladmission.Decision{
				Event:    tooladmission.EventApprovalChange,
				AgentID:  id,
				ServerID: serverID,
				Outcome:  outcome,
				Reason:   reason,
			},
			// The upsert always writes a row; missing targets were
			// answered by the reads above.
			func(runner *db.Queries) error {
				_, err := runner.SetAgentMcpEnabled(r.Context(), params)
				return err
			},
			// Closing the gate revokes: cancel this server's admitted
			// calls for this agent.
			func() {
				if canceler != nil {
					canceler.CancelAgentServerTools(id, serverID)
				}
			})
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("toggle mcp gate %q/%q: %w", id, serverID, err)
		}

		return errors.Join(writeAgentTools(w, r, queries, id), auditErr)
	}
}
