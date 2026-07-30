// Package user provides API-layer user agent list storage operations.
package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/radch-ai/ava/services/backend/internal/api/shared"
	"github.com/radch-ai/ava/services/backend/internal/db"
)

type Store interface {
	// GetAgentsForUser retrieves all agents (name and icon) for a given user.
	GetAgentsForUser(ctx context.Context, userID string) ([]AgentSummary, error)
	// GetThreadMessages retrieves thread messages for a user.
	GetThreadMessages(ctx context.Context, userID string, params ThreadParams) ([]ThreadMessage, error)
}

type ThreadParams struct {
	Limit  int
	Cursor *shared.Cursor
}

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{
		pool:    pool,
		queries: db.New(pool),
	}
}

// GetAgentsForUser returns all agents (name and icon) for a user.
func (s *PGStore) GetAgentsForUser(ctx context.Context, userID string) ([]AgentSummary, error) {
	rows, err := s.queries.GetAgentsForUser(ctx, shared.ParseUserID(userID))
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}

	agents := make([]AgentSummary, 0, len(rows))
	for _, row := range rows {
		agent := AgentSummary{
			Name: row.Name,
		}
		if row.Icon.Valid {
			agent.Icon = row.Icon.String
		}
		agents = append(agents, agent)
	}

	return agents, nil
}

func (s *PGStore) GetThreadMessages(ctx context.Context, userID string, params ThreadParams) ([]ThreadMessage, error) {
	parsedUserID := shared.ParseUserID(userID)

	if params.Cursor == nil {
		rows, err := s.queries.ListThreadFirstPage(ctx, db.ListThreadFirstPageParams{
			UserID: parsedUserID,
			Limit:  int32(params.Limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("query first page: %w", err)
		}
		return mapRowsToMessages(rows), nil
	}

	cursorID := shared.UUIDFromString(params.Cursor.ID)

	rows, err := s.queries.ListThreadAfterCursor(ctx, db.ListThreadAfterCursorParams{
		UserID:    parsedUserID,
		CreatedAt: pgtype.Timestamptz{Time: params.Cursor.CreatedAt, Valid: true},
		ID:        cursorID,
		Limit:     int32(params.Limit + 1),
	})
	if err != nil {
		if errors.Is(err, shared.ErrInvalidCursor) {
			return nil, err
		}
		return nil, fmt.Errorf("query cursor page: %w", err)
	}
	return mapCursorRowsToMessages(rows), nil
}

// mapRowsToMessages converts first page rows to messages.
// Duplicate of mapCursorRowsToMessages is intentional - sqlc generates separate types for each query.
func mapRowsToMessages(rows []db.ListThreadFirstPageRow) []ThreadMessage {
	messages := make([]ThreadMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, ThreadMessage{
			ID:        row.ID.String(),
			Role:      row.Role,
			Content:   row.Content,
			CreatedAt: row.CreatedAt.Time,
		})
	}
	return messages
}

// mapCursorRowsToMessages converts cursor page rows to messages.
// Duplicate of mapRowsToMessages is intentional - sqlc generates separate types for each query.
func mapCursorRowsToMessages(rows []db.ListThreadAfterCursorRow) []ThreadMessage {
	messages := make([]ThreadMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, ThreadMessage{
			ID:        row.ID.String(),
			Role:      row.Role,
			Content:   row.Content,
			CreatedAt: row.CreatedAt.Time,
		})
	}
	return messages
}
