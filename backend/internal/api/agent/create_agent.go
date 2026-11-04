// Package agent serves POST /api/agent — agent creation.
package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// Agent identity rules, ported from the UI (mocks/agent-id.ts) so
// backend and mock server answer identically. The
// client never derives ids — it sends a name, the server owns the
// mapping.

// validAgentNameRE: ASCII letters, digits and single spaces between
// words, tested on the trimmed name.
var validAgentNameRE = regexp.MustCompile(`^[A-Za-z0-9]+(?: [A-Za-z0-9]+)*$`)

func isValidAgentName(name string) bool {
	return validAgentNameRE.MatchString(strings.TrimSpace(name))
}

// deriveAgentID maps a validated name to its id: lowercase, spaces as
// hyphens. Only the shapes isValidAgentName admits reach here, so no
// further normalization applies — and ids can never start with "_",
// the system-reserved prefix.
func deriveAgentID(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}

// createAgentRequest is the wire body: identity and config fields
// at one level; only the column fields persist.
type createAgentRequest struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
	Soul struct {
		Personality string `json:"personality"`
		Identity    string `json:"identity"`
	} `json:"soul"`
	UserContext           *string `json:"userContext"`
	OperatingInstructions string  `json:"operatingInstructions"`
	ToolsContext          string  `json:"toolsContext"`
}

// createAgentResponse is the mock's 201 body: just the id — the UI
// navigates from it alone.
type createAgentResponse struct {
	ID string `json:"id"`
}

// CreateAgent answers the UI's createAgent(): derive the id from the
// display name, reject invalid names (400) and colliding ids (409) —
// never auto-suffixed.
func CreateAgent(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req createAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}

		if !isValidAgentName(req.Name) {
			return api.WriteError(w, http.StatusBadRequest,
				"Invalid agent name: use letters, digits and single spaces only")
		}
		id := deriveAgentID(req.Name)

		if _, err := queries.GetAgentID(r.Context(), id); err == nil {
			return api.WriteError(w, http.StatusConflict,
				fmt.Sprintf("An agent named \"%s\" already exists", req.Name))
		} else if !errors.Is(err, sql.ErrNoRows) {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("agent id collision check: %w", err)
		}
		// Absent userContext stores NULL; regular agents never send
		// the field.
		userContext := sql.NullString{}
		if req.UserContext != nil {
			userContext = sql.NullString{String: *req.UserContext, Valid: true}
		}
		if err := queries.CreateAgent(r.Context(), db.CreateAgentParams{
			ID:                    id,
			Name:                  req.Name,
			Icon:                  req.Icon,
			SoulPersonality:       req.Soul.Personality,
			SoulIdentity:          req.Soul.Identity,
			UserContext:           userContext,
			OperatingInstructions: req.OperatingInstructions,
			ToolsContext:          req.ToolsContext,
		}); err != nil {
			return fmt.Errorf("insert agent: %w", err)
		}

		return api.WriteJSON(w, http.StatusCreated, createAgentResponse{ID: id})
	}
}
