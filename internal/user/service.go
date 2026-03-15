// Package user provides user-related business logic.
package user

import (
	"context"
	"fmt"
)

// Service provides user-related business logic.
type Service struct {
	store Store
}

// NewService creates a new user service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// GetAgentIDs returns all agent IDs for a given user.
func (s *Service) GetAgentIDs(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return []string{}, nil
	}

	agentIDs, err := s.store.GetAgentIDsByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get agent IDs: %w", err)
	}

	return agentIDs, nil
}

// GetAgentConfiguration returns the configuration for a specific agent.
func (s *Service) GetAgentConfiguration(ctx context.Context, agentID string) (*AgentConfiguration, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}

	config, err := s.store.GetAgentConfiguration(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent configuration: %w", err)
	}

	return config, nil
}

// PatchAgentConfiguration performs a partial update of agent configuration.
func (s *Service) PatchAgentConfiguration(ctx context.Context, agentID string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}

	config, err := s.store.PatchAgentConfiguration(ctx, agentID, req)
	if err != nil {
		return nil, fmt.Errorf("patch agent configuration: %w", err)
	}

	return config, nil
}
