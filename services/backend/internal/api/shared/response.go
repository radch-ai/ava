package shared

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

func WriteJSON(logger zerolog.Logger, w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Error().Err(err).Msg("failed to encode JSON")
	}
}

func WriteError(logger zerolog.Logger, w http.ResponseWriter, status int, message string) {
	WriteJSON(logger, w, status, map[string]string{"error": message})
}
