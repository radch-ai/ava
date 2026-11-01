// Package api renders the backend's HTTP responses. The JSON shapes
// are the UI contract: error bodies are {message}, which the ky client
// surfaces verbatim. Endpoint handlers live in their own packages
// (agents, agent, …) and write via api.WriteJSON / api.WriteError.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

// errorBody is the UI's error shape ({message}); the ky client
// surfaces it verbatim in thrown errors.
type errorBody struct {
	Message string `json:"message"`
}

// WriteJSON writes status and the JSON-encoded body, logging encode
// failures — the status is already sent, so the log is the only trace
// of a truncated response.
func WriteJSON(logger zerolog.Logger, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Error().Err(err).Msg("encode JSON response")
	}
}

// WriteError writes status with the UI's {message} error body.
func WriteError(logger zerolog.Logger, w http.ResponseWriter, status int, message string) {
	WriteJSON(logger, w, status, errorBody{Message: message})
}
