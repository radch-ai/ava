// Package user provides user-related business logic.
package user

import (
	"context"
	"fmt"
)

// =============================================================================
// Service
// =============================================================================

// Service orchestrates business logic for user operations.
// Validates inputs before delegating to the store.
type Service struct {
	store Store
}

// NewService creates a new user service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// =============================================================================
// Agent Operations
// =============================================================================

// GetAgents returns all agents (name and icon) for a user.
func (s *Service) GetAgents(ctx context.Context, userID string) ([]Agent, error) {
	if userID == "" {
		return []Agent{}, nil
	}
	agents, err := s.store.GetAgentsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get agents: %w", err)
	}
	return agents, nil
}

// GetAgentConfiguration returns the full configuration for an agent.
func (s *Service) GetAgentConfiguration(ctx context.Context, userID, agentName string) (*AgentConfiguration, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if agentName == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	config, err := s.store.GetAgentConfiguration(ctx, userID, agentName)
	if err != nil {
		return nil, fmt.Errorf("get agent configuration: %w", err)
	}
	return config, nil
}

// PatchAgentConfiguration performs a partial update of agent configuration.
func (s *Service) PatchAgentConfiguration(ctx context.Context, userID, agentName string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if agentName == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	config, err := s.store.PatchAgentConfiguration(ctx, userID, agentName, req)
	if err != nil {
		return nil, fmt.Errorf("patch agent configuration: %w", err)
	}
	return config, nil
}

// CreateAgent creates a new agent. Name is auto-suffixed if duplicate.
func (s *Service) CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	resp, err := s.store.CreateAgent(ctx, userID, req)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	return resp, nil
}
