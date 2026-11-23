// Package api renders the backend's HTTP responses. The JSON shapes
// are the UI contract: error bodies are {message}, which the ky client
// surfaces verbatim. Endpoint handlers live in their own packages
// (agents, agent, …): they return errors instead of logging, and
// backend.New logs those via api.Log — the one place with a logger.
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
)

// Handler serves one request. A non-nil error is logged by api.Log;
// handlers write their own status before returning it. Handled client
// errors return nil — they need no log line.
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

// ServerIDPtr maps a nullable server_id column to its wire pointer.
func ServerIDPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// Canceler stops in-flight tool calls when authority changes: a disabled
// server, a disabled tool, a closed agent selection or a removed tool
// cancels the calls it already admitted. Cancellation is best-effort — the
// remote outcome stays unknown and nothing is retried.
type Canceler interface {
	CancelServerTools(serverID string) int
	CancelToolCalls(serverID, toolID string) int
	CancelAgentServerTools(agentID, serverID string) int
	CancelAgentToolCalls(agentID, serverID, toolID string) int
}
