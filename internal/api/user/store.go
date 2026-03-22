// Package user provides API-layer user agent list storage operations.
package user

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nevaria-ai/backend/internal/api/shared"
	"github.com/nevaria-ai/backend/internal/db"
)

type Store interface {
	// GetAgentsForUser retrieves all agents (name and icon) for a given user.
	GetAgentsForUser(ctx context.Context, userID string) ([]AgentSummary, error)
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
