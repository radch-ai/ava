// The tool catalog endpoints: GET and PATCH /api/tools — the global
// switches the MCP & Tools page edits.
package tools

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

// GetTools answers the installed catalog in creation order.
func GetTools(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		rows, err := queries.ListTools(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("list tools: %w", err)
		}
		out := apitypes.GetToolsResponse{Tools: make([]apitypes.Tool, 0, len(rows))}
		for _, row := range rows {
			out.Tools = append(out.Tools, toolFromRow(row))
		}
		return api.WriteJSON(w, http.StatusOK, out)
	}
}

// ToggleTool flips one tool's global switch and answers the saved
// record. An enable is an activation: the switch and its approval-change
// record commit together, so a global enable is never live unrecorded. A
// disable is a restriction: the switch commits on its own and the record
// is best-effort, so a failing audit can never keep a disabled tool live.
// Bursts on the same tool draw a 429.
func ToggleTool(throttle *api.Throttle, sqlDB *sql.DB, queries *db.Queries, canceler api.Canceler) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.ToggleToolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		if req.Enabled == nil {
			return api.WriteError(w, http.StatusBadRequest, "enabled is required")
		}
		id := r.PathValue("id")

		if !throttle.Allow("tool:" + id) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Toggling too fast — try again in a moment")
		}

		enabled := int64(0)
		if *req.Enabled {
			enabled = 1
		}
		tool, err := queries.GetTool(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Tool %q not found", id))
			}
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("get tool %q: %w", id, err)
		}
		outcome, reason := "revoke", "global_disabled"
		if *req.Enabled {
			outcome, reason = "approve", "global_enabled"
		}
		serverID := ""
		if tool.ServerID.Valid {
			serverID = tool.ServerID.String
		}

		auditErr, err := api.ApplyToggle(r.Context(), sqlDB, queries, *req.Enabled,
			tooladmission.Decision{
				Event:    tooladmission.EventApprovalChange,
				ServerID: serverID,
				ToolID:   id,
				Outcome:  outcome,
				Reason:   reason,
			},
			func(runner *db.Queries) error {
				_, err := runner.ToggleToolEnabled(r.Context(), db.ToggleToolEnabledParams{
					Enabled: enabled, ID: id,
				})
				return err
			},
			// Builtin entries have no server and still cancel.
			func() {
				if canceler != nil {
					canceler.CancelToolCalls(serverID, id)
				}
			})
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("toggle tool %q: %w", id, err)
		}

		row, err := queries.GetTool(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read tool %q after save: %w", id, err)
		}
		return errors.Join(api.WriteJSON(w, http.StatusOK, toolFromRow(row)), auditErr)
	}
}

func toolFromRow(row db.Tool) apitypes.Tool {
	return apitypes.Tool{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Enabled:     row.Enabled != 0,
		Kind:        row.Kind,
		ServerID:    api.ServerIDPtr(row.ServerID),
	}
}
