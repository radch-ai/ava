package backend

import (
	"context"
	"fmt"

	"github.com/radch-ai/ava/backend/apitypes"
)

// GetAgentConfig reads an agent's editable config — the prompt texts
// the lane's system prompt builds from. A missing agent fails with
// sql.ErrNoRows (wrapped); the lane reads that as a persona-less
// turn, not a failed one. Tool availability is not carried here: the
// turn's effective set is resolved per turn through the tools API.
func (s *TurnService) GetAgentConfig(ctx context.Context, agentID string) (*apitypes.AgentConfig, error) {
	row, err := s.queries.GetAgentConfig(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent config %q: %w", agentID, err)
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
		},
	}, nil
}
