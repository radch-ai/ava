//go:build integration

package agent_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/services/gateway/internal/storage"
)

func getAgentTestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

// createAgentsTable creates the agents table for testing.
// Backend owns migrations; gateway tests create table directly.
func createAgentsTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS agents (
			agent_id     TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL,
			name         TEXT,
			soul_md      TEXT,
			agents_md    TEXT,
			user_md      TEXT,
			identity_md  TEXT,
			tools_md     TEXT,
			heartbeat_md TEXT,
			config       JSONB,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_agents_user_id ON agents(user_id);
		CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at);
	`)
	return err
}

func setupAgentTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbURL := getAgentTestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err, "Failed to parse database URL")

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to test database: %v", err)
		return nil
	}

	err = createAgentsTable(ctx, pool)
	if err != nil {
		pool.Close()
		t.Skipf("Skipping test: cannot create agents table: %v", err)
		return nil
	}

	t.Cleanup(func() {
		cleanupAgentTestData(t, pool)
		pool.Close()
	})

	return pool
}

func cleanupAgentTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	tables := []string{"agents", "usage_logs", "processed_webhooks", "users"}

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Logf("Warning: failed to clean table %s: %v", table, err)
		}
	}
}

func createTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) {
	t.Helper()

	now := time.Now().Unix()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ($1, 1, 100, $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, userID, now, now)
	require.NoError(t, err, "Failed to create test user")
}

func TestPostgresAgentStore_Upsert(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-upsert"
	createTestUser(t, ctx, pool, userID)

	config := &storage.AgentConfig{
		AgentID:     "agent-1",
		UserID:      userID,
		Name:        "Test Agent",
		SoulMd:      "soul content",
		AgentsMd:    "agents content",
		UserMd:      "user content",
		IdentityMd:  "identity content",
		ToolsMd:     "tools content",
		HeartbeatMd: "heartbeat content",
		Config:      map[string]any{"key": "value"},
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err, "Upsert should succeed")

	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM agents WHERE agent_id = $1 AND user_id = $2", config.AgentID, userID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Agent should be stored")
}

func TestPostgresAgentStore_Get(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-get"
	createTestUser(t, ctx, pool, userID)

	original := &storage.AgentConfig{
		AgentID:     "agent-get",
		UserID:      userID,
		Name:        "Get Test Agent",
		SoulMd:      "soul",
		AgentsMd:    "agents",
		UserMd:      "user",
		IdentityMd:  "identity",
		ToolsMd:     "tools",
		HeartbeatMd: "heartbeat",
		Config:      map[string]any{"test": 123},
	}

	err := store.Upsert(ctx, original)
	require.NoError(t, err)

	retrieved, err := store.Get(ctx, userID, original.AgentID)
	require.NoError(t, err, "Get should succeed")

	assert.Equal(t, original.AgentID, retrieved.AgentID)
	assert.Equal(t, original.UserID, retrieved.UserID)
	assert.Equal(t, original.Name, retrieved.Name)
	assert.Equal(t, original.SoulMd, retrieved.SoulMd)
	assert.Equal(t, original.AgentsMd, retrieved.AgentsMd)
	assert.Equal(t, original.UserMd, retrieved.UserMd)
	assert.Equal(t, original.IdentityMd, retrieved.IdentityMd)
	assert.Equal(t, original.ToolsMd, retrieved.ToolsMd)
	assert.Equal(t, original.HeartbeatMd, retrieved.HeartbeatMd)
	assert.Equal(t, float64(123), retrieved.Config["test"])
}

func TestPostgresAgentStore_Get_NotFound(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-notfound"
	createTestUser(t, ctx, pool, userID)

	_, err := store.Get(ctx, userID, "nonexistent-agent")
	require.Error(t, err, "Get should return error for nonexistent agent")
	assert.Contains(t, err.Error(), "agent not found")
}

func TestPostgresAgentStore_Delete(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-delete"
	createTestUser(t, ctx, pool, userID)

	config := &storage.AgentConfig{
		AgentID: "agent-delete",
		UserID:  userID,
		Name:    "Delete Test",
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err)

	err = store.Delete(ctx, userID, config.AgentID)
	require.NoError(t, err, "Delete should succeed")

	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM agents WHERE agent_id = $1", config.AgentID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "Agent should be deleted")
}

func TestPostgresAgentStore_Delete_NotFound(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-delete-notfound"
	createTestUser(t, ctx, pool, userID)

	err := store.Delete(ctx, userID, "nonexistent-agent")
	require.Error(t, err, "Delete should return error for nonexistent agent")
	assert.Contains(t, err.Error(), "agent not found")
}

func TestPostgresAgentStore_ListByUser(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-list"
	createTestUser(t, ctx, pool, userID)

	for i := 1; i <= 3; i++ {
		config := &storage.AgentConfig{
			AgentID: fmt.Sprintf("agent-list-%d", i),
			UserID:  userID,
			Name:    fmt.Sprintf("Agent %d", i),
		}
		err := store.Upsert(ctx, config)
		require.NoError(t, err)
	}

	otherUserID := "test-user-list-other"
	createTestUser(t, ctx, pool, otherUserID)
	otherConfig := &storage.AgentConfig{
		AgentID: "agent-other",
		UserID:  otherUserID,
		Name:    "Other Agent",
	}
	err := store.Upsert(ctx, otherConfig)
	require.NoError(t, err)

	agents, err := store.ListByUser(ctx, userID)
	require.NoError(t, err, "ListByUser should succeed")
	assert.Len(t, agents, 3, "Should return 3 agents for user")

	agentIDs := make(map[string]bool)
	for _, a := range agents {
		agentIDs[a.AgentID] = true
		assert.Equal(t, userID, a.UserID)
	}
	assert.True(t, agentIDs["agent-list-1"])
	assert.True(t, agentIDs["agent-list-2"])
	assert.True(t, agentIDs["agent-list-3"])
}

func TestPostgresAgentStore_ListByUser_Empty(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-empty"
	createTestUser(t, ctx, pool, userID)

	agents, err := store.ListByUser(ctx, userID)
	require.NoError(t, err, "ListByUser should succeed")
	assert.Len(t, agents, 0, "Should return empty list for user with no agents")
}

func TestPostgresAgentStore_Upsert_Update(t *testing.T) {
	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-update"
	createTestUser(t, ctx, pool, userID)

	original := &storage.AgentConfig{
		AgentID: "agent-update",
		UserID:  userID,
		Name:    "Original Name",
		SoulMd:  "original soul",
	}

	err := store.Upsert(ctx, original)
	require.NoError(t, err)

	updated := &storage.AgentConfig{
		AgentID: "agent-update",
		UserID:  userID,
		Name:    "Updated Name",
		SoulMd:  "updated soul",
	}

	err = store.Upsert(ctx, updated)
	require.NoError(t, err, "Upsert update should succeed")

	retrieved, err := store.Get(ctx, userID, updated.AgentID)
	require.NoError(t, err)

	assert.Equal(t, "Updated Name", retrieved.Name)
	assert.Equal(t, "updated soul", retrieved.SoulMd)

	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM agents WHERE agent_id = $1", updated.AgentID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should still be only one agent after update")
}
