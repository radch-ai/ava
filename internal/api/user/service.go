// Package user provides API-layer business logic for user agent listing.
package user

import (
	"context"
	"fmt"
)

// Service orchestrates business logic for user agent list operations.
type Service struct {
	store Store
}

// NewService creates a new user service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// GetAgents returns all agents (name and icon) for a user.
func (s *Service) GetAgents(ctx context.Context, userID string) ([]AgentSummary, error) {
	if userID == "" {
		return []AgentSummary{}, nil
	}
	agents, err := s.store.GetAgentsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get agents: %w", err)
	}
	return agents, nil
}
