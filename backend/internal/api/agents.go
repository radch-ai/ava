package api

import (
	"net/http"

	"github.com/radch-ai/ava/backend/internal/db"
)

// getAgentsResponse wraps the list for GET /api/agents — {agents: [...]}
// per the UI contract. Entries are the sqlc rows: their json tags (id,
// name, icon) are the contract fields, and emit_empty_slices in
// backend/sqlc.yaml keeps an empty list an array, never null.
type getAgentsResponse struct {
	Agents []db.ListAgentsRow `json:"agents"`
}

// getAgents answers the UI's getAgents(): the user's agents in
// creation order, never the system superagent — clients render it from
// their own constant.
func (a *api) getAgents(w http.ResponseWriter, r *http.Request) {
	a.logger.Debug().Str("method", r.Method).Str("path", r.URL.Path).Msg("handling get agents request")

	rows, err := a.queries.ListAgents(r.Context())
	if err != nil {
		a.logger.Error().Err(err).Msg("get agents failed")
		a.writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	a.logger.Info().Int("agent_count", len(rows)).Msg("retrieved agents")
	a.writeJSON(w, http.StatusOK, getAgentsResponse{Agents: rows})
}
