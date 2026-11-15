// The agent config endpoints: GET and PATCH /api/agent/{id}/config —
// the server side of the UI's services/agent/config.ts.
package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// GetAgentConfig answers the UI's getAgentConfig(): the agent row
// plus its heartbeat, or the disabled default. System rows
// ("_"-prefixed) are served like any other.
func GetAgentConfig(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		bundle, found, err := loadBundle(r.Context(), queries, id)
		if err != nil {
			return err
		}
		if !found {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		}

		return api.WriteJSON(w, http.StatusOK, bundle)
	}
}

// UpdateAgentConfig answers the UI's updateAgentConfig(): read the
// row, replace the columns the patch carries, rewrite the row. No
// name validation — renaming to anything, including other agents'
// names, is the UI's call. System rows keep their identity: name/icon
// patches are skipped, config patches still apply.
func UpdateAgentConfig(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.AgentEditableFields
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		id := r.PathValue("id")

		row, err := queries.GetAgentConfig(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		} else if err != nil {
			return fmt.Errorf("get agent %q: %w", id, err)
		}

		// Shallow merge onto the read row; soul replaces both columns
		// as one unit.
		if !strings.HasPrefix(id, "_") {
			if req.Name != nil {
				row.Name = *req.Name
			}
			if req.Icon != nil {
				row.Icon = *req.Icon
			}
		}
		if req.Soul != nil {
			row.SoulPersonality = req.Soul.Personality
			row.SoulIdentity = req.Soul.Identity
		}
		userContext := row.UserContext
		if req.UserContext != nil {
			userContext = sql.NullString{String: *req.UserContext, Valid: true}
		}
		if req.OperatingInstructions != nil {
			row.OperatingInstructions = *req.OperatingInstructions
		}
		if req.ToolsContext != nil {
			row.ToolsContext = *req.ToolsContext
		}
		toolsEnabled := row.ToolsEnabled
		if req.ToolsEnabled != nil {
			encoded, err := json.Marshal(*req.ToolsEnabled)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("marshal toolsEnabled for %q: %w", id, err)
			}
			toolsEnabled = string(encoded)
		}

		if err := queries.UpdateAgentConfig(r.Context(), db.UpdateAgentConfigParams{
			Name:                  row.Name,
			Icon:                  row.Icon,
			SoulPersonality:       row.SoulPersonality,
			SoulIdentity:          row.SoulIdentity,
			UserContext:           userContext,
			OperatingInstructions: row.OperatingInstructions,
			ToolsContext:          row.ToolsContext,
			ToolsEnabled:          toolsEnabled,
			ID:                    id,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("update agent %q: %w", id, err)
		}

		bundle, found, err := loadBundle(r.Context(), queries, id)
		if err != nil {
			return err
		}
		if !found {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		}
		return api.WriteJSON(w, http.StatusOK, bundle)
	}
}

// defaultHeartbeat is the bundle's stand-in for agents with no
// heartbeat row yet: disabled, default routing.
var defaultHeartbeat = apitypes.HeartbeatConfig{
	Model:    nil,
	Enabled:  false,
	Duration: apitypes.HeartbeatDuration1hr,
	Content:  "",
}

// entryFromDB reads the model-pair columns: either NULL reads as nil
// (default routing) — a deleted provider nulls the owner via FK.
func entryFromDB(model, providerID sql.NullString) *apitypes.ModelEntry {
	if !model.Valid || !providerID.Valid {
		return nil
	}
	return &apitypes.ModelEntry{ID: model.String, ProviderID: providerID.String}
}

// entryToDB writes the pair: nil or partial entries store NULL/NULL
// (default routing); a concrete selection stores both columns.
func entryToDB(entry *apitypes.ModelEntry) (sql.NullString, sql.NullString) {
	if entry == nil || entry.ID == "" || entry.ProviderID == "" {
		return sql.NullString{}, sql.NullString{}
	}
	return sql.NullString{String: entry.ID, Valid: true},
		sql.NullString{String: entry.ProviderID, Valid: true}
}

// stringPtrNil reads a nullable text column into the bundle's pointer
// shape; NULL stays absent.
func stringPtrNil(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

// loadBundle assembles the full agent bundle: the agent row (system
// rows included) and its heartbeat row or the disabled default.
// found is false only for a missing agent. Cron jobs are fetched
// lazily via the cron-jobs endpoints, not here.
func loadBundle(ctx context.Context, queries *db.Queries, id string) (apitypes.AgentConfigWithIdentity, bool, error) {
	row, err := queries.GetAgentConfig(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return apitypes.AgentConfigWithIdentity{}, false, nil
	}
	if err != nil {
		return apitypes.AgentConfigWithIdentity{}, false, fmt.Errorf("get agent %q: %w", id, err)
	}

	toolsEnabled := []string{}
	if err := json.Unmarshal([]byte(row.ToolsEnabled), &toolsEnabled); err != nil {
		// NOT NULL DEFAULT '[]'; only hand-edited data can miss.
		toolsEnabled = []string{}
	}

	heartbeat := defaultHeartbeat
	if hb, err := queries.GetHeartbeat(ctx, id); err == nil {
		heartbeat = apitypes.HeartbeatConfig{
			Model:    entryFromDB(hb.Model, hb.ProviderID),
			Enabled:  hb.Enabled != 0,
			Duration: apitypes.HeartbeatDuration(hb.Duration),
			Content:  hb.Content,
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return apitypes.AgentConfigWithIdentity{}, false, fmt.Errorf("get heartbeat for %q: %w", id, err)
	}

	return apitypes.AgentConfigWithIdentity{
		AgentIdentity: apitypes.AgentIdentity{
			ID:   row.ID,
			Name: row.Name,
			Icon: row.Icon,
		},
		AgentConfig: apitypes.AgentConfig{
			AgentEditableFields: apitypes.AgentEditableFields{
				Soul:                  &apitypes.SoulConfig{Personality: row.SoulPersonality, Identity: row.SoulIdentity},
				UserContext:           stringPtrNil(row.UserContext),
				OperatingInstructions: &row.OperatingInstructions,
				ToolsContext:          &row.ToolsContext,
				ToolsEnabled:          &toolsEnabled,
			},
			Heartbeat: heartbeat,
		},
	}, true, nil
}
