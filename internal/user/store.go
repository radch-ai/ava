// Package user provides user-related data storage.
package user

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nevaria-ai/backend/internal/db"
)

// =============================================================================
// Store Interface
// =============================================================================

// Store defines the interface for user data storage operations.
type Store interface {
	// GetAgentsForUser retrieves all agents (name and icon) for a given user.
	GetAgentsForUser(ctx context.Context, userID string) ([]Agent, error)
	// GetAgentConfiguration retrieves the full configuration for an agent.
	GetAgentConfiguration(ctx context.Context, userID, agentName string) (*AgentConfiguration, error)
	// PatchAgentConfiguration performs a partial update of agent configuration in a transaction.
	PatchAgentConfiguration(ctx context.Context, userID, agentName string, req *PatchAgentRequest) (*AgentConfiguration, error)
	// CreateAgent creates a new agent with automatic name suffixing.
	CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error)
}

// =============================================================================
// PostgreSQL Store
// =============================================================================

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPGStore creates a new PostgreSQL store.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{
		pool:    pool,
		queries: db.New(pool),
	}
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

// pgBool creates a pgtype.Bool from a bool value.
func pgBool(v bool) pgtype.Bool {
	return pgtype.Bool{Bool: v, Valid: true}
}

// pgText creates a pgtype.Text from a string, handling null with empty string.
func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}
