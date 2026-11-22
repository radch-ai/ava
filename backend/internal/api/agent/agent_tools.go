// The per-agent tool endpoints: GET /agent/{id}/tools and PATCH
// /agent/{id}/tools/{toolId} — live opt-outs over the inheritance model,
// kept out of the config save the way cron jobs are.
package agent

import (
	"context"
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

// auditGrant writes one approval-change record.
func auditGrant(ctx context.Context, queries *db.Queries, decision tooladmission.Decision) error {
	return tooladmission.NewAuditor(queries).Record(ctx, decision)
}

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

		var auditFail error
		if *req.Enabled {
			// Enabling means dropping the opt-out: inheritance resumes.
			// The change and its record commit together, so no activation
			// goes unrecorded.
			tx, err := sqlDB.BeginTx(r.Context(), nil)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("begin opt-in %q for %q: %w", toolID, id, err)
			}
			defer func() { _ = tx.Rollback() }()
			txq := db.New(tx)
			if err := txq.RemoveAgentDisabledTool(r.Context(), db.RemoveAgentDisabledToolParams{
				AgentID: id, ToolID: toolID,
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("remove opt-out %q for %q: %w", toolID, id, err)
			}
			if err := auditGrant(r.Context(), txq, tooladmission.Decision{
				Event:   tooladmission.EventApprovalChange,
				AgentID: id,
				ToolID:  toolID,
				Outcome: "approve",
				Reason:  "locally_enabled",
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("audit opt-in %q for %q: %w", toolID, id, err)
			}
			if err := tx.Commit(); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("commit opt-in %q for %q: %w", toolID, id, err)
			}
		} else {
			if err := queries.AddAgentDisabledTool(r.Context(), db.AddAgentDisabledToolParams{
				AgentID: id, ToolID: toolID,
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("add opt-out %q for %q: %w", toolID, id, err)
			}
			// The opt-out takes effect even when its record fails; the
			// cancellation is best-effort by nature. It is agent-scoped,
			// and builtin entries (no server) match too.
			if canceler != nil {
				serverIDStr := ""
				if serverID, ok := tool.ServerID.(string); ok {
					serverIDStr = serverID
				}
				canceler.CancelAgentToolCalls(id, serverIDStr, toolID)
			}
			if err := auditGrant(r.Context(), queries, tooladmission.Decision{
				Event:   tooladmission.EventApprovalChange,
				AgentID: id,
				ToolID:  toolID,
				Outcome: "revoke",
				Reason:  "locally_disabled",
			}); err != nil {
				auditFail = fmt.Errorf("audit opt-out %q for %q: %w", toolID, id, err)
			}
		}

		row, err := queries.GetAgentTool(r.Context(), db.GetAgentToolParams{AgentID: id, ID: toolID})
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read tool %q for %q after save: %w", toolID, id, err)
		}
		return errors.Join(api.WriteJSON(w, http.StatusOK, agentToolFromOne(row)), auditFail)
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
		ServerID:        serverIDPtr(row.ServerID),
	}
}

func agentToolFromOne(row db.GetAgentToolRow) apitypes.AgentTool {
	return apitypes.AgentTool{
		ID:              row.ID,
		Name:            row.Name,
		Description:     row.Description,
		GlobalEnabled:   row.GloballyEnabled != 0,
		LocallyDisabled: row.LocallyDisabled,
		Kind:            row.Kind,
		ServerID:        serverIDPtr(row.ServerID),
	}
}

// serverIDPtr maps the nullable server_id column to its wire pointer.
func serverIDPtr(v interface{}) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}
