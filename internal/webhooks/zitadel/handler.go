package zitadel

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

// UserService defines the interface for user operations.
type UserService interface {
	CreateUser(ctx context.Context, userID string) error
	DeleteUser(ctx context.Context, userID string) error
}

// Handler handles Zitadel webhook HTTP requests.
type Handler struct {
	service UserService
	logger  zerolog.Logger
}

// NewHandler creates a new Zitadel webhook handler.
func NewHandler(service UserService, logger zerolog.Logger) *Handler {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// ServeHTTP handles all Zitadel webhook requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.EventType {
	case EventTypeUserCreated:
		h.handleUserCreated(w, r, req)
	case EventTypeUserDeleted:
		h.handleUserDeleted(w, r, req)
	default:
		// Return 200 OK for unknown event types to prevent Zitadel retries
		h.logger.Warn().Str("event_type", req.EventType).Msg("unknown event type, ignoring")
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) handleUserCreated(w http.ResponseWriter, r *http.Request, req WebhookRequest) {
	if err := h.service.CreateUser(r.Context(), req.UserID); err != nil {
		h.logger.Error().Err(err).Str("user_id", req.UserID).Msg("failed to create user")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", req.UserID).Msg("user created")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleUserDeleted(w http.ResponseWriter, r *http.Request, req WebhookRequest) {
	if err := h.service.DeleteUser(r.Context(), req.UserID); err != nil {
		h.logger.Error().Err(err).Str("user_id", req.UserID).Msg("failed to delete user")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", req.UserID).Msg("user deleted")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:errcheck // encoding error response - nothing we can do if this fails
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
