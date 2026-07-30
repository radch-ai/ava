// Package user provides API-layer business logic for user agent listing.
package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/radch-ai/ava/services/backend/internal/api/shared"
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

// GetThread retrieves paginated messages from the user's thread.
func (s *Service) GetThread(ctx context.Context, userID, rawLimit, rawCursor string) (*ThreadPage, error) {
	limit := shared.NormalizeLimit(rawLimit)

	cursor, err := shared.DecodeCursor(rawCursor)
	if err != nil {
		return nil, err
	}
	if err := shared.ValidateCursor(cursor); err != nil {
		return nil, err
	}

	messages, err := s.store.GetThreadMessages(ctx, userID, ThreadParams{
		Limit:  limit,
		Cursor: cursor,
	})
	if err != nil {
		if errors.Is(err, shared.ErrInvalidCursor) {
			return nil, shared.ErrInvalidCursor
		}
		return nil, fmt.Errorf("get thread: %w", err)
	}

	if len(messages) == 0 {
		return &ThreadPage{Items: []ThreadMessage{}, NextCursor: nil}, nil
	}

	var nextCursor *string
	items := messages
	if len(messages) > limit {
		items = messages[:limit]
		encoded, err := shared.EncodeCursor(items[len(items)-1].CreatedAt, items[len(items)-1].ID)
		if err != nil {
			return nil, fmt.Errorf("encode cursor: %w", err)
		}
		nextCursor = &encoded
	}

	return &ThreadPage{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}
