// The tool catalog endpoints: GET and PATCH /api/tools — the global
// switches the MCP & Tools page edits.
package tools

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
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
// record. Bursts on the same tool draw a 429.
func ToggleTool(throttle *api.Throttle, queries *db.Queries) api.Handler {
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

		row, err := queries.GetTool(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read tool %q after save: %w", id, err)
		}
		return api.WriteJSON(w, http.StatusOK, toolFromRow(row))
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
