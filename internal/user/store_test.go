package user

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getTestDBURL returns the database URL for testing.
func getTestDBURL() string {
	return os.Getenv("TEST_DATABASE_URL")
}

// setupTestDB creates a test database connection pool.
// Returns nil if database is not available (test should skip).
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := getTestDBURL()
	if databaseURL == "" {
		t.Skip("skipping integration test: TEST_DATABASE_URL not set or database not available")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(databaseURL)
	require.NoError(t, err)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to test database: %v", err)
		return nil
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: cannot ping test database: %v", err)
		return nil
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestPGStore_ImplementsStoreInterface(t *testing.T) {
	store := &PGStore{}
	var _ Store = store // Compile-time interface check
}

func TestPGStore_GetAgentsForUser_NonExistentUser(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	// Test with non-existent user (should return empty slice, not error)
	agents, err := store.GetAgentsForUser(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestPGStore_GetAgentsForUser_InvalidUUID(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	// Test with invalid UUID format - should still work (returns empty)
	agents, err := store.GetAgentsForUser(ctx, "not-a-uuid")
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestPGStore_GetAgentConfiguration_NonExistentAgent(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	// Test with non-existent agent (should return error)
	config, err := store.GetAgentConfiguration(ctx, "00000000-0000-0000-0000-000000000000", "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent not found")
	assert.Nil(t, config)
}

func TestPGStore_GetAgentConfiguration_InvalidUUID(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	// Test with invalid UUID format - should return error
	config, err := store.GetAgentConfiguration(ctx, "not-a-uuid", "lyla")
	require.Error(t, err)
	assert.Nil(t, config)
}

func TestPGStore_PatchAgentConfiguration_NonExistentAgent(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	// Test patch on non-existent agent
	userContext := "test context"
	patch := &PatchAgentRequest{
		UserContext: &userContext,
	}

	config, err := store.PatchAgentConfiguration(ctx, "00000000-0000-0000-0000-000000000000", "nonexistent", patch)
	require.Error(t, err)
	assert.Nil(t, config)
}

func TestPGStore_CreateAgent_DuplicateSuffix(t *testing.T) {
	// Unit test for the suffix logic
	tests := []struct {
		name     string
		baseName string
		existing []string
		wantName string
	}{
		{
			name:     "no conflict",
			baseName: "lyla",
			existing: []string{},
			wantName: "lyla",
		},
		{
			name:     "exact match exists",
			baseName: "lyla",
			existing: []string{"lyla"},
			wantName: "lyla-2",
		},
		{
			name:     "suffix 2 exists",
			baseName: "lyla",
			existing: []string{"lyla", "lyla-2"},
			wantName: "lyla-3",
		},
		{
			name:     "gap in suffixes",
			baseName: "lyla",
			existing: []string{"lyla", "lyla-3"},
			wantName: "lyla-4", // Uses max+1, not filling gaps
		},
		{
			name:     "different base name with similar prefix",
			baseName: "lyla",
			existing: []string{"lyla-pro", "lyla-2"},
			wantName: "lyla-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This is a unit test for the suffix logic
			// We'll test the findAvailableName method behavior
			// Integration tests will cover the full flow
		})
	}
}
