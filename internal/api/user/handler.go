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

// GetLylaThread handles GET /user/lyla-thread.
func (h *Handler) GetLylaThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		shared.WriteError(h.logger, w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID := internalctx.GetUserIDFromContext(r.Context())
	if userID == "" {
		h.logger.Error().Msg("user ID not found in context")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		return
	}

	limit := r.URL.Query().Get("limit")
	cursor := r.URL.Query().Get("cursor")

	page, err := h.service.GetLylaThread(r.Context(), userID, limit, cursor)
	if err != nil {
		if err == shared.ErrInvalidCursor {
			shared.WriteError(h.logger, w, http.StatusBadRequest, "invalid cursor")
			return
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to get lyla thread")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		return
	}

	shared.WriteJSON(h.logger, w, http.StatusOK, page)
}
