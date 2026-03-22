// Package agent provides HTTP handlers for agent configuration endpoints.
package agent

import (
	"encoding/json"
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

// AgentConfigurationHandler routes to GET, POST, or PATCH for a specific agent.
func (h *Handler) AgentConfigurationHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateAgent(w, r)
	case http.MethodGet:
		h.GetAgentConfiguration(w, r)
	case http.MethodPatch:
		h.PatchAgentConfiguration(w, r)
	default:
		shared.WriteError(h.logger, w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GetAgentConfiguration handles GET /user/agent/{name}.
func (h *Handler) GetAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	userID, agentName, status, message := h.extractUserAndAgent(r)
	if status != 0 {
		shared.WriteError(h.logger, w, status, message)
		return
	}

	config, err := h.service.GetAgentConfiguration(r.Context(), userID, agentName)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_name", agentName).Msg("agent not found")
		shared.WriteError(h.logger, w, http.StatusNotFound, "agent not found")
		return
	}

	shared.WriteJSON(h.logger, w, http.StatusOK, config)
}

// PatchAgentConfiguration handles PATCH /user/agent/{name}.
func (h *Handler) PatchAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	userID, agentName, status, message := h.extractUserAndAgent(r)
	if status != 0 {
		shared.WriteError(h.logger, w, status, message)
		return
	}

	var req PatchAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(h.logger, w, http.StatusBadRequest, "invalid request body")
		return
	}

	config, err := h.service.PatchAgentConfiguration(r.Context(), userID, agentName, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_name", agentName).Msg("failed to patch agent")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "failed to update configuration")
		return
	}

	shared.WriteJSON(h.logger, w, http.StatusOK, config)
}

// CreateAgent handles POST /user/agent/{name}.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	userID, agentName, status, message := h.extractUserAndAgent(r)
	if status != 0 {
		shared.WriteError(h.logger, w, status, message)
		return
	}

	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(h.logger, w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = agentName

	resp, err := h.service.CreateAgent(r.Context(), userID, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Str("agent_name", agentName).Msg("failed to create agent")
		shared.WriteError(h.logger, w, http.StatusInternalServerError, "failed to create agent")
		return
	}

	shared.WriteJSON(h.logger, w, http.StatusCreated, resp)
}

// extractUserAndAgent extracts userID from context and agentName from URL path.
func (h *Handler) extractUserAndAgent(r *http.Request) (userID, agentName string, status int, message string) {
	userID = internalctx.GetUserIDFromContext(r.Context())
	if userID == "" {
		return "", "", http.StatusInternalServerError, "internal error"
	}

	path := strings.TrimPrefix(r.URL.Path, "/user/agent/")
	agentName = strings.TrimSuffix(path, "/")
	if agentName == "" {
		return userID, "", http.StatusBadRequest, "agent name is required"
	}

	return
}
