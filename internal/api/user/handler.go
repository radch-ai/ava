// Package user provides HTTP handlers for user agent list endpoints.
package user

import (
	"net/http"

	"github.com/nevaria-ai/backend/internal/api/shared"
	internalctx "github.com/nevaria-ai/backend/internal/ctx"
	"github.com/rs/zerolog"
)

// Handler serves user agent list endpoints.
type Handler struct {
	service *Service
	logger  zerolog.Logger
}

// NewHandler creates a new user HTTP handler.
func NewHandler(service *Service, logger zerolog.Logger) *Handler {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Handler{service: service, logger: logger}
}

// GetUserAgents handles GET /user/agents.
func (h *Handler) GetUserAgents(w http.ResponseWriter, r *http.Request) {
	userID := internalctx.GetUserIDFromContext(r.Context())
	if userID == "" {
		h.logger.Error().Msg("user ID not found in context")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		return
	}

	agents, err := h.service.GetAgents(r.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to get agents")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		return
	}

	shared.WriteJSON(h.logger, w, http.StatusOK, GetAgentsResponse{Agents: agents})
}
