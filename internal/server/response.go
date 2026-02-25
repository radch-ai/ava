package server

import (
	"errors"
)

// Package-level error variables for consistent error handling.
// These can be used for error wrapping, comparison, and logging.
var (
	ErrSessionInvalid   = errors.New("session invalid")
	ErrSessionExpired   = errors.New("session expired")
	ErrAuthUnavailable  = errors.New("auth service unavailable")
	ErrInvalidResponse  = errors.New("invalid response")
	ErrContextCanceled  = errors.New("context canceled")
	ErrCacheUnavailable = errors.New("cache unavailable")
)

// SessionResponse represents the response returned for session validation requests.
// This matches the auth service's response format exactly.
type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	UserID        string `json:"user_id,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}
