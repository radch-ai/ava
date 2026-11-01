// Package agents serves GET /api/agents — the user's agent list.
package agents

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// getAgentsResponse wraps the list — {agents: [...]} per the UI
// contract. Entries are the sqlc rows: their json tags (id, name,
// icon) are the contract fields, and emit_empty_slices in
// backend/sqlc.yaml keeps an empty list an array, never null.
type getAgentsResponse struct {
	Agents []db.ListAgentsRow `json:"agents"`
}

// GetAgents answers the UI's getAgents(): the user's agents in
// creation order, never the system superagent — clients render it from
// their own constant.
func GetAgents(queries *db.Queries, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Debug().Str("method", r.Method).Str("path", r.URL.Path).Msg("handling get agents request")

		rows, err := queries.ListAgents(r.Context())
		if err != nil {
			logger.Error().Err(err).Msg("get agents failed")
			api.WriteError(logger, w, http.StatusInternalServerError, "Internal server error")
			return
		}

		logger.Info().Int("agent_count", len(rows)).Msg("retrieved agents")
		api.WriteJSON(logger, w, http.StatusOK, getAgentsResponse{Agents: rows})
	}
}
