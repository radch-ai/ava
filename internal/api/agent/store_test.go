package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestDBURL() string {
	return os.Getenv("TEST_DATABASE_URL")
}

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
	var _ Store = store
}

func TestPGStore_GetAgentConfiguration_NonExistentAgent(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	config, err := store.GetAgentConfiguration(ctx, "999999999", "nonexistent-agent")
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

	req := &PatchAgentRequest{Icon: ptr("star")}
	config, err := store.PatchAgentConfiguration(ctx, "999999999", "nonexistent-agent", req)
	require.Error(t, err)
	assert.Nil(t, config)
}

func TestPGStore_CreateAgent_InvalidUserID(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	req := &CreateAgentRequest{
		Name: "test-agent",
		Icon: "robot",
	}
	resp, err := store.CreateAgent(ctx, "not-a-valid-id", req)
	require.Error(t, err)
	assert.Nil(t, resp)
}
