// Package agent provides API-layer business logic for agent configuration endpoints.
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/nevaria-ai/backend/internal/api/shared"
)

// Service orchestrates business logic for agent configuration operations.
type Service struct {
	store Store
}

// NewService creates a new agent service.
func NewService(store Store) *Service {
	return &Service{store: store}
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

func (s *Service) GetActivity(ctx context.Context, userID, agentName, rawLimit, rawCursor string) (*ActivityPage, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if agentName == "" {
		return nil, fmt.Errorf("agent name is required")
	}

	limit := shared.NormalizeLimit(rawLimit)

	cursor, err := shared.DecodeCursor(rawCursor)
	if err != nil {
		return nil, err
	}
	if err := shared.ValidateCursor(cursor); err != nil {
		return nil, err
	}

	items, err := s.store.GetAgentActivityRuns(ctx, userID, agentName, ActivityParams{
		Limit:  limit,
		Cursor: cursor,
	})
	if err != nil {
		if errors.Is(err, shared.ErrInvalidCursor) {
			return nil, shared.ErrInvalidCursor
		}
		return nil, fmt.Errorf("get agent activity: %w", err)
	}

	if len(items) == 0 {
		return &ActivityPage{Items: []ActivityItem{}, NextCursor: nil}, nil
	}

	pageItems := items
	var nextCursor *string
	if len(items) > limit {
		pageItems = items[:limit]
		encoded, err := shared.EncodeCursor(pageItems[len(pageItems)-1].CreatedAt, pageItems[len(pageItems)-1].ID)
		if err != nil {
			return nil, fmt.Errorf("encode cursor: %w", err)
		}
		nextCursor = &encoded
	}

	return &ActivityPage{Items: pageItems, NextCursor: nextCursor}, nil
}
