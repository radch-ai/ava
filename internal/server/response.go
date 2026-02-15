package server

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Package-level error variables for consistent error handling.
// These can be used for error wrapping, comparison, and logging.
var (
	ErrSessionInvalid   = errors.New("session invalid")
	ErrSessionExpired   = errors.New("session expired")
	ErrAuthUnavailable  = errors.New("auth service unavailable")
	ErrInvalidResponse  = errors.New("invalid response")
	ErrContextCanceled  = errors.New("context canceled")
	ErrSecretRequired   = errors.New("internal secret required")
	ErrInvalidFormat    = errors.New("invalid format")
	ErrCacheUnavailable = errors.New("cache unavailable")
)

// respondJSON writes a JSON response with the given status code.
// The response body is set to the provided data value.
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Skip encoding for NoContent status
	if status == http.StatusNoContent {
		return
	}

	// Use encoder for streaming and to avoid extra byte buffer allocation
	_ = json.NewEncoder(w).Encode(data)
}

// respondError writes a JSON error response with the given status code and message.
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{
		"error": message,
	})
}

// InternalErrorResponse represents an error response for internal endpoints.
type InternalErrorResponse struct {
	Error string `json:"error"`
}

// respondInternalUnauthorized writes an unauthorized response for internal endpoints.
func respondInternalUnauthorized(w http.ResponseWriter, message string) {
	respondJSON(w, http.StatusUnauthorized, InternalErrorResponse{
		Error: message,
	})
}

// SessionResponse represents the response returned for session validation requests.
// This matches the auth service's response format exactly.
type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	UserID        string `json:"user_id,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}
