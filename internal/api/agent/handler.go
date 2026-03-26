// Package agent provides HTTP handlers for agent configuration endpoints.
package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nevaria-ai/backend/internal/api/shared"
	internalctx "github.com/nevaria-ai/backend/internal/ctx"
	"github.com/rs/zerolog"
)

// Handler serves agent configuration endpoints.
type Handler struct {
	service *Service
	logger  zerolog.Logger
}

// NewHandler creates a new agent HTTP handler.
func NewHandler(service *Service, logger zerolog.Logger) *Handler {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Handler{service: service, logger: logger}
}

// AgentConfigurationHandler routes to GET, POST, PATCH, or activity endpoint for a specific agent.
func (h *Handler) AgentConfigurationHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Str("method", r.Method).Str("path", r.URL.Path).Msg("routing agent configuration request")

	switch r.Method {
	case http.MethodPost:
		h.CreateAgent(w, r)
	case http.MethodGet:
		// Check if this is an activity endpoint request (path ends with /activity)
		if strings.HasSuffix(r.URL.Path, "/activity") {
			h.GetActivity(w, r)
			return
		}
		h.GetAgentConfiguration(w, r)
	case http.MethodPatch:
		h.PatchAgentConfiguration(w, r)
	default:
		shared.WriteError(h.logger, w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GetAgentConfiguration handles GET /user/agent/{name}.
func (h *Handler) GetAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Str("path", r.URL.Path).Msg("handling get agent configuration")

	userID, agentName, status, message := h.extractUserAndAgent(r)
	if status != 0 {
		shared.WriteError(h.logger, w, status, message)
		return
	}

	h.logger.Debug().Str("user_id", userID).Str("agent_name", agentName).Msg("fetching agent configuration")
	config, err := h.service.GetAgentConfiguration(r.Context(), userID, agentName)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_name", agentName).Msg("agent not found")
		shared.WriteError(h.logger, w, http.StatusNotFound, "agent not found")
		return
	}

	h.logger.Info().Str("user_id", userID).Str("agent_name", agentName).Msg("retrieved agent configuration")
	shared.WriteJSON(h.logger, w, http.StatusOK, config)
}

// GetActivity handles GET /user/agent/{name}/activity.
func (h *Handler) GetActivity(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Str("path", r.URL.Path).Msg("handling get agent activity")

	if r.Method != http.MethodGet {
		shared.WriteError(h.logger, w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, agentName, status, message := h.extractUserAndAgent(r)
	if status != 0 {
		shared.WriteError(h.logger, w, status, message)
		return
	}

	limit := r.URL.Query().Get("limit")
	cursor := r.URL.Query().Get("cursor")

	h.logger.Debug().Str("user_id", userID).Str("agent_name", agentName).Str("limit", limit).Bool("has_cursor", cursor != "").Msg("fetching agent activity")

	page, err := h.service.GetActivity(
		r.Context(),
		userID,
		agentName,
		limit,
		cursor,
	)
	if err != nil {
		switch {
		case errors.Is(err, shared.ErrInvalidCursor):
			h.logger.Debug().Str("agent_name", agentName).Msg("invalid cursor provided")
			shared.WriteError(h.logger, w, http.StatusBadRequest, "invalid cursor")
		default:
			h.logger.Error().Err(err).Str("agent_name", agentName).Msg("failed to get agent activity")
			shared.WriteError(h.logger, w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	h.logger.Info().Str("user_id", userID).Str("agent_name", agentName).Int("item_count", len(page.Items)).Bool("has_next", page.NextCursor != nil).Msg("retrieved agent activity")
	shared.WriteJSON(h.logger, w, http.StatusOK, page)
}

// PatchAgentConfiguration handles PATCH /user/agent/{name}.
func (h *Handler) PatchAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Str("path", r.URL.Path).Msg("handling patch agent configuration")

	userID, agentName, status, message := h.extractUserAndAgent(r)
	if status != 0 {
		shared.WriteError(h.logger, w, status, message)
		return
	}

	var req PatchAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Debug().Str("agent_name", agentName).Err(err).Msg("invalid request body")
		shared.WriteError(h.logger, w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.logger.Debug().Str("user_id", userID).Str("agent_name", agentName).Msg("patching agent configuration")
	config, err := h.service.PatchAgentConfiguration(r.Context(), userID, agentName, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_name", agentName).Msg("failed to patch agent")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "failed to update configuration")
		return
	}

	h.logger.Info().Str("user_id", userID).Str("agent_name", agentName).Msg("agent configuration patched")
	shared.WriteJSON(h.logger, w, http.StatusOK, config)
}

// CreateAgent handles POST /user/agent/{name}.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Str("path", r.URL.Path).Msg("handling create agent")

	userID, agentName, status, message := h.extractUserAndAgent(r)
	if status != 0 {
		shared.WriteError(h.logger, w, status, message)
		return
	}

	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Debug().Str("agent_name", agentName).Err(err).Msg("invalid request body")
		shared.WriteError(h.logger, w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = agentName

	h.logger.Debug().Str("user_id", userID).Str("agent_name", agentName).Msg("creating agent")
	resp, err := h.service.CreateAgent(r.Context(), userID, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Str("agent_name", agentName).Msg("failed to create agent")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "failed to create agent")
		return
	}

	h.logger.Info().Str("user_id", userID).Str("agent_name", resp.Name).Msg("agent created")
	shared.WriteJSON(h.logger, w, http.StatusCreated, resp)
}

// extractUserAndAgent extracts userID from context and agentName from URL path.
// It removes the /user/agent/ prefix and strips any /activity suffix if present.
func (h *Handler) extractUserAndAgent(r *http.Request) (userID, agentName string, status int, message string) {
	userID = internalctx.GetUserIDFromContext(r.Context())
	if userID == "" {
		return "", "", http.StatusInternalServerError, "internal error"
	}

	const prefix = "/user/agent/"
	path := strings.TrimPrefix(r.URL.Path, prefix)
	if path == r.URL.Path {
		return userID, "", http.StatusBadRequest, "invalid path"
	}

	// Remove trailing slash
	agentName = strings.TrimSuffix(path, "/")

	// Strip /activity suffix if present
	agentName = strings.TrimSuffix(agentName, "/activity")

	if agentName == "" {
		return userID, "", http.StatusBadRequest, "agent name is required"
	}

	return
}
