// Package user provides HTTP handlers for user-related endpoints.
package user

import (
	"encoding/json"
	"net/http"
	"strings"

	internalctx "github.com/nevaria-ai/backend/internal/ctx"
	"github.com/rs/zerolog"
)

// AgentsResponse represents the response for the agents endpoint.
type AgentsResponse struct {
	AgentIDs []string `json:"agent_ids"`
}

// Handler handles user-related HTTP requests.
type Handler struct {
	service *Service
	logger  zerolog.Logger
}

// NewHandler creates a new user handler.
func NewHandler(service *Service, logger zerolog.Logger) *Handler {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// GetUserAgents handles GET /users/agents requests.
// Returns a list of agent IDs for the authenticated user.
func (h *Handler) GetUserAgents(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context using shared context key
	userID := internalctx.GetUserIDFromContext(r.Context())
	if userID == "" {
		h.logger.Error().Msg("user ID not found in context")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Get agent IDs from service
	agentIDs, err := h.service.GetAgentIDs(r.Context(), userID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to get agent IDs")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Return response
	h.writeJSON(w, http.StatusOK, AgentsResponse{AgentIDs: agentIDs})
}

// writeJSON writes a JSON response with the given status code.
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error().Err(err).Msg("failed to encode JSON response")
	}
}

// writeError writes an error response.
func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}

// AgentConfigurationHandler routes to GET or PATCH based on method.
func (h *Handler) AgentConfigurationHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.GetAgentConfiguration(w, r)
	case http.MethodPatch:
		h.PatchAgentConfiguration(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GetAgentConfiguration handles GET /users/agents/{agentId} requests.
func (h *Handler) GetAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract agentId from path: /users/agents/{agentId}
	path := strings.TrimPrefix(r.URL.Path, "/users/agents/")
	agentID := strings.TrimSuffix(path, "/")

	if agentID == "" || agentID == "/" {
		h.writeError(w, http.StatusBadRequest, "agent ID is required")
		return
	}

	config, err := h.service.GetAgentConfiguration(r.Context(), agentID)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_id", agentID).Msg("failed to get agent configuration")
		h.writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	h.writeJSON(w, http.StatusOK, config)
}

// PatchAgentConfiguration handles PATCH /users/agents/{agentId} requests.
func (h *Handler) PatchAgentConfiguration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract agentId from path: /users/agents/{agentId}
	path := strings.TrimPrefix(r.URL.Path, "/users/agents/")
	agentID := strings.TrimSuffix(path, "/")

	if agentID == "" || agentID == "/" {
		h.writeError(w, http.StatusBadRequest, "agent ID is required")
		return
	}

	var req PatchAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	config, err := h.service.PatchAgentConfiguration(r.Context(), agentID, &req)
	if err != nil {
		h.logger.Error().Err(err).Str("agent_id", agentID).Msg("failed to patch agent configuration")
		h.writeError(w, http.StatusInternalServerError, "failed to update configuration")
		return
	}

	h.writeJSON(w, http.StatusOK, config)
}
