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
		if owned, ok := tool.ServerID.(string); ok {
			serverID = owned
		}

		var auditFail error
		if *req.Enabled {
			// Activation: the switch and its record commit together, so an
			// enabled tool is never live unrecorded.
			tx, err := sqlDB.BeginTx(r.Context(), nil)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("begin tool toggle %q: %w", id, err)
			}
			defer func() { _ = tx.Rollback() }()
			txq := db.New(tx)
			n, err := txq.ToggleToolEnabled(r.Context(), db.ToggleToolEnabledParams{
				Enabled: enabled,
				ID:      id,
			})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("toggle tool %q: %w", id, err)
			}
			if n == 0 {
				return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Tool %q not found", id))
			}
			if err := tooladmission.NewAuditor(txq).Record(r.Context(), tooladmission.Decision{
				Event:    tooladmission.EventApprovalChange,
				ServerID: serverID,
				ToolID:   id,
				Outcome:  outcome,
				Reason:   reason,
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("audit tool toggle %q: %w", id, err)
			}
			if err := tx.Commit(); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("commit tool toggle %q: %w", id, err)
			}
		} else {
			// Restriction: the switch commits alone and the admitted calls
			// are revoked before the record is attempted. A failing audit
			// degrades the trail — joined into the successful response —
			// and never keeps a disabled tool live.
			n, err := queries.ToggleToolEnabled(r.Context(), db.ToggleToolEnabledParams{
				Enabled: enabled,
				ID:      id,
			})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("toggle tool %q: %w", id, err)
			}
			if n == 0 {
				return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Tool %q not found", id))
			}
			// Builtin entries have no server and still cancel.
			if canceler != nil {
				canceler.CancelToolCalls(serverID, id)
			}
			if err := tooladmission.NewAuditor(queries).Record(r.Context(), tooladmission.Decision{
				Event:    tooladmission.EventApprovalChange,
				ServerID: serverID,
				ToolID:   id,
				Outcome:  outcome,
				Reason:   reason,
			}); err != nil {
				auditFail = fmt.Errorf("audit tool toggle %q: %w", id, err)
			}
		}

		row, err := queries.GetTool(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read tool %q after save: %w", id, err)
		}
		return errors.Join(api.WriteJSON(w, http.StatusOK, toolFromRow(row)), auditFail)
	}
}

func toolFromRow(row db.Tool) apitypes.Tool {
	return apitypes.Tool{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Builtin:     row.Builtin != 0,
		Enabled:     row.Enabled != 0,
		Kind:        row.Kind,
		ServerID:    serverIDPtr(row.ServerID),
	}
}

// serverIDPtr maps the nullable server_id column to its wire pointer.
func serverIDPtr(v interface{}) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}
