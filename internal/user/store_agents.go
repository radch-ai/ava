// Package user provides agent list storage operations.
package user

import (
	"context"
	"fmt"
)

// =============================================================================
// Agent List Operations
// =============================================================================

// GetAgentsForUser returns all agents (name and icon) for a user.
func (s *PGStore) GetAgentsForUser(ctx context.Context, userID string) ([]Agent, error) {
	userUUID := uuidFromString(userID)

	rows, err := s.queries.GetAgentsForUser(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agent := Agent{
			Name: row.Name,
		}
		if row.Icon.Valid {
			agent.Icon = row.Icon.String
		}
		agents = append(agents, agent)
	}

	return agents, nil
}
