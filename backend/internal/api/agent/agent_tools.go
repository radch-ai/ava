// The per-agent tool endpoints: GET /agent/{id}/tools and PATCH
// /agent/{id}/tools/{toolId} — live opt-outs over the inheritance model,
// kept out of the config save the way cron jobs are.
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

// GetAgentTools answers the catalog as this agent sees it: every
// installed tool with its global switch and this agent's opt-out.
func GetAgentTools(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		if _, err := queries.GetAgentConfig(r.Context(), id); err != nil {
			return agentLookupErr(w, id, err)
		}
		return writeAgentTools(w, r, queries, id)
	}
}

// writeAgentTools answers the agent's tool listing plus its MCP gate
// state -- the one shape every tool-mutating endpoint returns, so the
// editor re-renders from a single source of truth. Every server is
// listed with its gate state (absent = off), so the editor can explain
// a global blocker and link to where it is managed.
func writeAgentTools(w http.ResponseWriter, r *http.Request, queries *db.Queries, id string) error {
	rows, err := queries.ListAgentTools(r.Context(), id)
	if err != nil {
		_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
		return fmt.Errorf("list agent tools for %q: %w", id, err)
	}
	gates, err := queries.ListAgentMcps(r.Context(), id)
	if err != nil {
		_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
		return fmt.Errorf("list agent mcp gates for %q: %w", id, err)
	}
	servers, err := queries.ListMcpServers(r.Context())
	if err != nil {
		_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
		return fmt.Errorf("list mcp servers: %w", err)
	}
	enabled := make(map[string]bool, len(gates))
	for _, gate := range gates {
		enabled[gate.ServerID] = gate.Enabled != 0
	}
	out := apitypes.GetAgentToolsResponse{
		Tools:   make([]apitypes.AgentTool, 0, len(rows)),
		Servers: make([]apitypes.AgentMcpPreference, 0, len(servers)),
	}
	for _, row := range rows {
		out.Tools = append(out.Tools, agentToolFromRow(row))
	}
	for _, server := range servers {
		out.Servers = append(out.Servers, apitypes.AgentMcpPreference{
			ServerID:      server.ID,
			Name:          server.Name,
			AgentEnabled:  enabled[server.ID],
			ServerEnabled: server.Enabled != 0,
			CatalogState:  server.CatalogState,
		})
	}
	return api.WriteJSON(w, http.StatusOK, out)
}

// ToggleAgentTool writes or clears one agent's opt-out for one tool and
// answers the saved view. Bursts on the same target draw a 429.
func ToggleAgentTool(throttle *api.Throttle, sqlDB *sql.DB, queries *db.Queries, canceler api.Canceler) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.ToggleAgentToolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		if req.Enabled == nil {
			return api.WriteError(w, http.StatusBadRequest, "enabled is required")
		}
		id := r.PathValue("id")
		toolID := r.PathValue("toolId")

		if _, err := queries.GetAgentConfig(r.Context(), id); err != nil {
			return agentLookupErr(w, id, err)
		}
		tool, err := queries.GetTool(r.Context(), toolID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Tool %q not found", toolID))
			}
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("get tool %q: %w", toolID, err)
		}

		key := "agent:" + id + ":tool:" + toolID
		if !throttle.Allow(key) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Toggling too fast — try again in a moment")
		}

		outcome, reason := "revoke", "locally_disabled"
		if *req.Enabled {
			outcome, reason = "approve", "locally_enabled"
		}
		serverIDStr := ""
		if tool.ServerID.Valid {
			serverIDStr = tool.ServerID.String
		}
		auditErr, err := api.ApplyToggle(r.Context(), sqlDB, queries, *req.Enabled,
			tooladmission.Decision{
				Event:    tooladmission.EventApprovalChange,
				AgentID:  id,
				ServerID: serverIDStr,
				ToolID:   toolID,
				Outcome:  outcome,
				Reason:   reason,
			},
			func(runner *db.Queries) error {
				if *req.Enabled {
					// Enabling drops the opt-out: inheritance resumes.
					return runner.RemoveAgentDisabledTool(r.Context(), db.RemoveAgentDisabledToolParams{
						AgentID: id, ToolID: toolID,
					})
				}
				return runner.AddAgentDisabledTool(r.Context(), db.AddAgentDisabledToolParams{
					AgentID: id, ToolID: toolID,
				})
			},
			// The opt-out takes effect even when its record fails; the
			// cancellation is best-effort by nature. Builtin entries
			// (no server) match too.
			func() {
				if canceler != nil {
					canceler.CancelAgentToolCalls(id, serverIDStr, toolID)
				}
			})
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("toggle opt-out %q for %q: %w", toolID, id, err)
		}

		// The toggle answers the same listing shape as GET and the gate
		// toggle: the editor re-renders every switch from one source.
		return errors.Join(writeAgentTools(w, r, queries, id), auditErr)
	}
}

// agentLookupErr maps the agent-row read to 404 or a server fault.
func agentLookupErr(w http.ResponseWriter, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
	}
	_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
	return fmt.Errorf("get agent %q: %w", id, err)
}

func agentToolFromRow(row db.ListAgentToolsRow) apitypes.AgentTool {
	return apitypes.AgentTool{
		ID:              row.ID,
		Name:            row.Name,
		Description:     row.Description,
		GlobalEnabled:   row.GloballyEnabled != 0,
		LocallyDisabled: row.LocallyDisabled,
		Kind:            row.Kind,
		ServerID:        api.ServerIDPtr(row.ServerID),
	}
}
