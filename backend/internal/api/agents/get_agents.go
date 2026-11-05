// Package agents serves GET /api/agents — the user's agent list.
package agents

import (
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// GetAgents answers the UI's getAgents(): the user's agents in
// creation order, never the system superagent — clients render it
// from their own constant. emit_empty_slices in backend/sqlc.yaml
// keeps an empty list an array, never null.
func GetAgents(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		rows, err := queries.GetAgents(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("list agents: %w", err)
		}
		return api.WriteJSON(w, http.StatusOK, apitypes.GetAgentsResponse{Agents: rows})
	}
}
