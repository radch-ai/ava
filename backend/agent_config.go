package backend

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/radch-ai/ava/backend/apitypes"
)

// GetAgentConfig reads an agent's editable config — the prompt texts
// and tool list a turn needs. A missing agent fails with
// sql.ErrNoRows (wrapped); the lane reads that as a persona-less
// turn, not a failed one. The heartbeat stays zero: it belongs to the
// cron runner, not the lane.
func (s *TurnService) GetAgentConfig(ctx context.Context, agentID string) (*apitypes.AgentConfig, error) {
	row, err := s.queries.GetAgentConfig(ctx, agentID)
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
