package zitadel

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nevaria-ai/backend/internal/db"
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
	return s.queries.CreateUser(ctx, uuidFromString(userID))
}

// DeleteUser deletes a user record. CASCADE removes all related data.
func (s *PGStore) DeleteUser(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	return s.queries.DeleteUser(ctx, uuidFromString(userID))
}

// =============================================================================
// Helpers
// =============================================================================

// uuidFromString converts a string UUID to pgtype.UUID.
func uuidFromString(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}
