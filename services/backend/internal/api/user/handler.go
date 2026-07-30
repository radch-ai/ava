// Package user provides HTTP handlers for user agent list endpoints.
package user

import (
	"net/http"

	"github.com/radch-ai/ava/services/backend/internal/api/shared"
	internalctx "github.com/radch-ai/ava/services/backend/internal/ctx"
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
	h.logger.Debug().Str("method", r.Method).Str("path", r.URL.Path).Msg("handling get user agents request")

	userID := internalctx.GetUserIDFromContext(r.Context())
	if userID == "" {
		h.logger.Error().Msg("user ID not found in context")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Debug().Str("user_id", userID).Msg("fetching agents for user")
	agents, err := h.service.GetAgents(r.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to get agents")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", userID).Int("agent_count", len(agents)).Msg("retrieved user agents")
	shared.WriteJSON(h.logger, w, http.StatusOK, GetAgentsResponse{Agents: agents})
}

// GetThread handles GET /user/thread.
func (h *Handler) GetThread(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Str("method", r.Method).Str("path", r.URL.Path).Msg("handling get thread request")

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

	h.logger.Debug().Str("user_id", userID).Str("limit", limit).Bool("has_cursor", cursor != "").Msg("fetching thread")

	page, err := h.service.GetThread(r.Context(), userID, limit, cursor)
	if err != nil {
		if err == shared.ErrInvalidCursor {
			h.logger.Debug().Str("user_id", userID).Msg("invalid cursor provided")
			shared.WriteError(h.logger, w, http.StatusBadRequest, "invalid cursor")
			return
		}
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to get thread")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", userID).Int("message_count", len(page.Items)).Bool("has_next", page.NextCursor != nil).Msg("retrieved thread")
	shared.WriteJSON(h.logger, w, http.StatusOK, page)
}
