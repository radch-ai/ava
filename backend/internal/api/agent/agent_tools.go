// The per-agent tool endpoints: GET /agent/{id}/tools and PATCH
// /agent/{id}/tools/{toolId} — live toggles over the preference
// rows, kept out of the config save the way cron jobs are.
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

// GetAgentTools answers the catalog as this agent sees it: every
// installed tool with its global switch and the agent's preference.
func GetAgentTools(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		if _, err := queries.GetAgentConfig(r.Context(), id); err != nil {
			return agentLookupErr(w, id, err)
		}
		rows, err := queries.ListAgentTools(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("list agent tools for %q: %w", id, err)
		}
		out := apitypes.GetAgentToolsResponse{Tools: make([]apitypes.AgentTool, 0, len(rows))}
		for _, row := range rows {
			out.Tools = append(out.Tools, agentToolFromRow(row))
		}
		return api.WriteJSON(w, http.StatusOK, out)
	}
}

// ToggleAgentTool toggles one agent's preference for one tool and
// answers the saved view. Bursts on the same target draw a 429.
func ToggleAgentTool(throttle *api.Throttle, queries *db.Queries) api.Handler {
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
		if _, err := queries.GetTool(r.Context(), toolID); err != nil {
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

		if *req.Enabled {
			if _, err := queries.AddAgentTool(r.Context(), db.AddAgentToolParams{
				AgentID: id, ToolID: toolID,
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("add tool %q for %q: %w", toolID, id, err)
			}
		} else {
			if _, err := queries.RemoveAgentTool(r.Context(), db.RemoveAgentToolParams{
				AgentID: id, ToolID: toolID,
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("remove tool %q for %q: %w", toolID, id, err)
			}
		}

		row, err := queries.GetAgentTool(r.Context(), db.GetAgentToolParams{AgentID: id, ID: toolID})
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read tool %q for %q after save: %w", toolID, id, err)
		}
		return api.WriteJSON(w, http.StatusOK, agentToolFromOne(row))
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
		ID:            row.ID,
		Name:          row.Name,
		Description:   row.Description,
		GlobalEnabled: row.GloballyEnabled != 0,
		AgentEnabled:  row.AgentEnabled != 0,
	}
}

func agentToolFromOne(row db.GetAgentToolRow) apitypes.AgentTool {
	return apitypes.AgentTool{
		ID:            row.ID,
		Name:          row.Name,
		Description:   row.Description,
		GlobalEnabled: row.GloballyEnabled != 0,
		AgentEnabled:  row.AgentEnabled != 0,
	}
}
