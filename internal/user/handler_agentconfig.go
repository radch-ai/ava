// Package user provides HTTP handlers for agent configuration endpoints.
package user

import (
	"encoding/json"
	"net/http"
	"strings"

	internalctx "github.com/nevaria-ai/backend/internal/ctx"
)

// =============================================================================
// Agent Configuration Endpoints
// =============================================================================

// AgentConfigurationHandler routes to GET, POST (create), or PATCH for a specific agent.
// Routes: POST /user/agent/{name} (create), GET /user/agent/{name}, PATCH /user/agent/{name}
func (h *Handler) AgentConfigurationHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateAgent(w, r)
	case http.MethodGet:
		h.GetAgentConfiguration(w, r)
	case http.MethodPatch:
		h.PatchAgentConfiguration(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GetAgentConfiguration handles GET /user/agent/{name}.
func (h *Handler) GetAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	userID, agentName := h.extractUserAndAgent(r)
	if userID == "" {
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if agentName == "" {
		h.writeError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	config, err := h.service.GetAgentConfiguration(r.Context(), userID, agentName)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_name", agentName).Msg("agent not found")
		h.writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	h.writeJSON(w, http.StatusOK, config)
}

// PatchAgentConfiguration handles PATCH /user/agent/{name}.
func (h *Handler) PatchAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	userID, agentName := h.extractUserAndAgent(r)
	if userID == "" {
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if agentName == "" {
		h.writeError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	var req PatchAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	config, err := h.service.PatchAgentConfiguration(r.Context(), userID, agentName, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_name", agentName).Msg("failed to patch agent")
		h.writeError(w, http.StatusInternalServerError, "failed to update configuration")
		return
	}

	h.writeJSON(w, http.StatusOK, config)
}

// =============================================================================
// Agent Create Endpoint
// =============================================================================

// CreateAgent handles POST /user/agent/{name}.
// The agent name comes from the URL path, not the request body.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	userID, agentName := h.extractUserAndAgent(r)
	if userID == "" {
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if agentName == "" {
		h.writeError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Name comes from URL path
	req.Name = agentName

	resp, err := h.service.CreateAgent(r.Context(), userID, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Str("agent_name", agentName).Msg("failed to create agent")
		h.writeError(w, http.StatusInternalServerError, "failed to create agent")
		return
	}

	h.writeJSON(w, http.StatusCreated, resp)
}

// =============================================================================
// Helpers
// =============================================================================

// extractUserAndAgent extracts userID from context and agentName from URL path.
func (h *Handler) extractUserAndAgent(r *http.Request) (userID, agentName string) {
	userID = internalctx.GetUserIDFromContext(r.Context())
	path := strings.TrimPrefix(r.URL.Path, "/user/agent/")
	agentName = strings.TrimSuffix(path, "/")
	return
}
