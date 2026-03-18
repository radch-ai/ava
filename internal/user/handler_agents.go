// Package user provides HTTP handlers for agent endpoints.
package user

import (
	"encoding/json"
	"net/http"

	internalctx "github.com/nevaria-ai/backend/internal/ctx"
	"github.com/rs/zerolog"
)

// =============================================================================
// Handler
// =============================================================================

// Handler handles HTTP requests for agent endpoints.
type Handler struct {
	service *Service
	logger  zerolog.Logger
}

// NewHandler creates a new handler.
func NewHandler(service *Service, logger zerolog.Logger) *Handler {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Handler{service: service, logger: logger}
}

// =============================================================================
// Agent List Endpoint
// =============================================================================

// GetUserAgents handles GET /user/agents - returns list of agents with name and icon.
func (h *Handler) GetUserAgents(w http.ResponseWriter, r *http.Request) {
	userID := internalctx.GetUserIDFromContext(r.Context())
	if userID == "" {
		h.logger.Error().Msg("user ID not found in context")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	agents, err := h.service.GetAgents(r.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to get agents")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.writeJSON(w, http.StatusOK, AgentsResponse{Agents: agents})
}

// =============================================================================
// Helpers
// =============================================================================

// writeJSON writes a JSON response.
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error().Err(err).Msg("failed to encode JSON")
	}
}

// writeError writes an error response.
func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}
