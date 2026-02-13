// Package proxy provides LLM proxy functionality with routing, quota management, and usage tracking.
package proxy

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

// Context keys.
const (
	requestStartKey contextKey = "request_start"
)

// GatewayState represents the current state of the gateway
type GatewayState struct {
	Status      GatewayStatus
	PausedAt    int64
	PauseReason string
}

// Middleware provides HTTP middleware for the proxy
type Middleware struct {
	state GatewayState
}

// NewMiddleware creates a new proxy middleware
func NewMiddleware() *Middleware {
	return &Middleware{
		state: GatewayState{
			Status: StatusHealthy,
		},
	}
}

// GetStatus returns the current gateway status
func (m *Middleware) GetStatus() GatewayStatus {
	return m.state.Status
}

// SetStatus updates the gateway status
func (m *Middleware) SetStatus(status GatewayStatus, reason string) {
	m.state.Status = status
	m.state.PauseReason = reason

	if status == StatusPaused {
		m.state.PausedAt = 0 // TODO: Set timestamp
		log.Warn().Str("reason", reason).Msg("Gateway paused")
	} else {
		log.Info().Str("status", string(status)).Msg("Gateway status changed")
	}
}

// PauseCheck is HTTP middleware that returns 503 if service is paused
func (m *Middleware) PauseCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.state.Status == StatusPaused || m.state.Status == StatusCritical {
			http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
			log.Debug().
				Str("status", string(m.state.Status)).
				Str("path", r.URL.Path).
				Msg("Request rejected due to service status")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CheckContext is middleware to add context to requests
func CheckContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add request start time to context
		ctx := context.WithValue(r.Context(), requestStartKey, 0)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RecoveryMiddleware recovers from panics
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Error().Interface("panic", err).Str("path", r.URL.Path).Msg("Recovered from panic")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// Pause pauses the gateway
func (m *Middleware) Pause(reason string) error {
	m.SetStatus(StatusPaused, reason)
	return nil
}

// Resume resumes the gateway
func (m *Middleware) Resume() error {
	m.SetStatus(StatusHealthy, "")
	return nil
}

// IsPaused returns true if the gateway is paused
func (m *Middleware) IsPaused() bool {
	return m.state.Status == StatusPaused || m.state.Status == StatusCritical
}

// GetState returns the current gateway state
func (m *Middleware) GetState() GatewayState {
	return m.state
}

// HandleError creates an error response
func HandleError(w http.ResponseWriter, err error, statusCode int) {
	log.Error().Err(err).Int("status", statusCode).Msg("Request error")
	http.Error(w, fmt.Sprintf("Error: %v", err), statusCode)
}
