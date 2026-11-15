package backend

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

// Store handles GetAgentConfig runs over; New binds it at boot,
// before the gateway serves.
var agentConfigQueries *db.Queries

// bindAgentConfig arms GetAgentConfig with the handle New already
// holds.
func bindAgentConfig(queries *db.Queries) {
	agentConfigQueries = queries
}

// GetAgentConfig reads an agent's editable config — the prompt texts
// and tool list a turn needs. A missing agent fails with
// sql.ErrNoRows (wrapped); the lane reads that as a persona-less
// turn, not a failed one. The heartbeat stays zero: it belongs to the
// cron runner, not the lane.
func GetAgentConfig(ctx context.Context, agentID string) (*apitypes.AgentConfig, error) {
	row, err := agentConfigQueries.GetAgentConfig(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent config %q: %w", agentID, err)
	}
	toolsEnabled := []string{}
	if err := json.Unmarshal([]byte(row.ToolsEnabled), &toolsEnabled); err != nil {
		// NOT NULL DEFAULT '[]'; only hand-edited data can miss.
		toolsEnabled = []string{}
	}
	var userContext *string
	if row.UserContext.Valid {
		userContext = &row.UserContext.String
	}
	return &apitypes.AgentConfig{
		AgentEditableFields: apitypes.AgentEditableFields{
			Soul:                  &apitypes.SoulConfig{Personality: row.SoulPersonality, Identity: row.SoulIdentity},
			UserContext:           userContext,
			OperatingInstructions: &row.OperatingInstructions,
			ToolsContext:          &row.ToolsContext,
			ToolsEnabled:          &toolsEnabled,
		},
	}, nil
}
