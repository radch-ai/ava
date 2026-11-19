// Package agent serves POST /api/agent — agent creation.
package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/names"
)

// Identity follows internal/names (the UI mock mirrors it): the client
// never derives ids — it sends a name, the server owns the mapping.

// CreateAgent answers the UI's createAgent(): derive the id from the
// display name, reject invalid names (400) and colliding ids (409) —
// never auto-suffixed — then insert the agent and its initial thread
// in one transaction, the agent row first (the pointer FK is
// deferred). A half-created agent is never visible.
func CreateAgent(sqlDB *sql.DB, queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.CreateAgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}

		if !names.Valid(req.Name) {
			return api.WriteError(w, http.StatusBadRequest,
				"Invalid agent name: use letters, digits and single spaces only")
		}
		id := names.Slug(req.Name)

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
		threadID := id + "-t1"
		tx, err := sqlDB.BeginTx(r.Context(), nil)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("begin create agent: %w", err)
		}
		qtx := queries.WithTx(tx)
		if err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
			ID:                    id,
			Name:                  req.Name,
			Icon:                  req.Icon,
			SoulPersonality:       req.Soul.Personality,
			SoulIdentity:          req.Soul.Identity,
			UserContext:           userContext,
			OperatingInstructions: req.OperatingInstructions,
			ToolsContext:          req.ToolsContext,
			ActiveThreadID:        threadID,
		}); err != nil {
			_ = tx.Rollback()
			// The pre-check races other writers; the constraint is the
			// authority and answers the same 409 body.
			if isAgentIDConflict(err) {
				return api.WriteError(w, http.StatusConflict,
					fmt.Sprintf("An agent named \"%s\" already exists", req.Name))
			}
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("insert agent: %w", err)
		}
		if err := qtx.InsertThread(r.Context(), db.InsertThreadParams{
			ID:      threadID,
			AgentID: id,
			// The initial thread has no predecessor: it is the
			// start of the agent's display chain.
			PreviousThreadID: sql.NullString{},
		}); err != nil {
			_ = tx.Rollback()
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("insert initial thread: %w", err)
		}
		if err := tx.Commit(); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("commit create agent: %w", err)
		}

		return api.WriteJSON(w, http.StatusCreated, apitypes.CreateAgentResponse{ID: id})
	}
}

// isAgentIDConflict reports whether a contended insert lost the id
// race: no typed sqlite error exists in the driver surface, so the
// constraint's message is what distinguishes it.
func isAgentIDConflict(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed: agents.id")
}
