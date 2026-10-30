// Package api implements the backend's HTTP surface — exactly the
// endpoints the UI calls. The contract source is the UI itself:
// ui/web/src/lib/services (client calls) and ui/web/src/lib/mocks
// (handler behavior). The binary entrypoint mounts this handler under
// /api on its root mux.
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
)

type api struct {
	queries *db.Queries
	logger  zerolog.Logger
}

// Handler builds the backend route table over an already-open,
// already-migrated database.
func Handler(sqlDB *sql.DB, logger zerolog.Logger) http.Handler {
	a := &api{queries: db.New(sqlDB), logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agents", a.getAgents)
	return mux
}

// errorBody is the UI's error shape ({message}); the ky client
// surfaces it verbatim in thrown errors.
type errorBody struct {
	Message string `json:"message"`
}

func (a *api) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		a.logger.Error().Err(err).Msg("encode JSON response")
	}
}

func (a *api) writeError(w http.ResponseWriter, status int, message string) {
	a.writeJSON(w, status, errorBody{Message: message})
}
