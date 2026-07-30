package zitadel

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/radch-ai/ava/services/backend/internal/db"
)

// =============================================================================
// Store Interface
// =============================================================================

// Store defines the interface for user lifecycle storage operations.
type Store interface {
	// CreateUser creates a user record. Idempotent - safe if user already exists.
	CreateUser(ctx context.Context, userID string) error
	// DeleteUser deletes a user record. CASCADE removes all related data.
	DeleteUser(ctx context.Context, userID string) error
}

// =============================================================================
// PostgreSQL Store
// =============================================================================

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPGStore creates a new PostgreSQL store for user lifecycle operations.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{
		pool:    pool,
		queries: db.New(pool),
	}
}

// CreateUser creates a user record. Idempotent - safe if user already exists.
func (s *PGStore) CreateUser(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	return s.queries.CreateUser(ctx, parseUserID(userID))
}

// DeleteUser deletes a user record. CASCADE removes all related data.
func (s *PGStore) DeleteUser(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	return s.queries.DeleteUser(ctx, parseUserID(userID))
}

// =============================================================================
// Helpers
// =============================================================================

// parseUserID converts a string user ID (Zitadel Sonyflake) to int64.
func parseUserID(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return id
}
