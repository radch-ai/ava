// PATCH /api/agent/{id}/heartbeat: save the agent's single
// heartbeat row.
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

// UpdateHeartbeat answers the UI's updateHeartbeat(): upsert the
// agent's one heartbeat row - the whole HeartbeatConfig, every field
// written every time. Unknown agent fails before any write.
func UpdateHeartbeat(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.HeartbeatConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		id := r.PathValue("id")

		if _, err := queries.GetAgentID(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		} else if err != nil {
			return fmt.Errorf("get agent %q: %w", id, err)
		}

		enabled := int64(0)
		if req.Enabled {
			enabled = 1
		}
		if err := queries.UpdateHeartbeat(r.Context(), db.UpdateHeartbeatParams{
			AgentID:  id,
			Model:    modelToDB(req.Model),
			Enabled:  enabled,
			Duration: string(req.Duration),
			Content:  req.Content,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("upsert heartbeat for %q: %w", id, err)
		}

		bundle, found, err := loadBundle(r.Context(), queries, id)
		if err != nil {
			return err
		}
		if !found {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		}
		return api.WriteJSON(w, http.StatusOK, bundle)
	}
}
