// Package api renders the backend's HTTP responses. The JSON shapes
// are the UI contract: error bodies are {message}, which the ky client
// surfaces verbatim. Endpoint handlers live in their own packages
// (agents, agent, …): they return errors instead of logging, and
// backend.New logs those via api.Log — the one place with a logger.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
)

// Handler serves one request. A non-nil error marks a server-side
// fault (answered with a 500, or the response failed to encode);
// handled client errors return nil — they need no log line.
type Handler func(http.ResponseWriter, *http.Request) error

// WriteJSON writes status and the JSON-encoded body. The status goes
// out before the body, so an encode error is returned for the caller
// to log — the response is already truncated.
func WriteJSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(body)
}

// WriteError writes status with the UI's {message} error body.
func WriteError(w http.ResponseWriter, status int, message string) error {
	return WriteJSON(w, status, apitypes.ErrorResponse{Message: message})
}

// Log adapts a Handler to http.HandlerFunc, logging returned errors
// with the request that caused them.
func Log(logger zerolog.Logger, h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			logger.Error().Err(err).
				Str("method", r.Method).Str("path", r.URL.Path).
				Msg("request failed")
		}
	}
}
