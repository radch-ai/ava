//go:build integration
// +build integration

package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nevaria-ai/backend/internal/api/shared"
	"github.com/nevaria-ai/backend/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPGStore_ImplementsStoreInterface(t *testing.T) {
	store := &PGStore{}
	var _ Store = store
}

func TestPGStore_GetAgentConfiguration_NonExistentAgent(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	config, err := store.GetAgentConfiguration(ctx, "999999999", "nonexistent-agent")
	require.Error(t, err)
	assert.Nil(t, config)
}

func TestPGStore_PatchAgentConfiguration_NonExistentAgent(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	req := &PatchAgentRequest{Icon: ptr("star")}
	config, err := store.PatchAgentConfiguration(ctx, "999999999", "nonexistent-agent", req)
	require.Error(t, err)
	assert.Nil(t, config)
}

func TestPGStore_CreateAgent_InvalidUserID(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	req := &CreateAgentRequest{
		Name: "test-agent",
		Icon: "robot",
	}
	resp, err := store.CreateAgent(ctx, "not-a-valid-id", req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestPGStore_GetAgentActivityRuns_FirstPageOrdersOldestFirst(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)
	userID := uniqueTestUserID(t)
	agentName := "activity-ordering"
	seedTestAgent(t, testDB.Pool, userID, agentName)

	older := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	newer := older.Add(10 * time.Minute)
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-000000000001", older, "user", "older")
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-000000000002", newer, "cron", "newer")

	items, err := store.GetAgentActivityRuns(ctx, userID, agentName, ActivityParams{Limit: 10})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "00000000-0000-0000-0000-000000000001", items[0].ID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000002", items[1].ID)
}

func TestPGStore_GetAgentActivityRuns_SameCreatedAtOrdersByIDAscending(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)
	userID := uniqueTestUserID(t)
	agentName := "activity-same-created-at"
	seedTestAgent(t, testDB.Pool, userID, agentName)

	ts := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-00000000000a", ts, "user", "lower")
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-00000000000f", ts, "user", "higher")

	items, err := store.GetAgentActivityRuns(ctx, userID, agentName, ActivityParams{Limit: 10})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000a", items[0].ID)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000f", items[1].ID)
}

func TestPGStore_GetAgentActivityRuns_CursorPageReturnsStrictlyNewerRows(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)
	userID := uniqueTestUserID(t)
	agentName := "activity-cursor-ordering"
	seedTestAgent(t, testDB.Pool, userID, agentName)

	base := time.Date(2026, 3, 22, 13, 0, 0, 0, time.UTC)
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-00000000000d", base.Add(-2*time.Minute), "user", "older-time")
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-00000000000e", base, "user", "same-time-lower-id")
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-00000000000f", base, "user", "cursor-row")
	seedAgentRun(t, testDB.Pool, userID, agentName, "00000000-0000-0000-0000-000000000010", base.Add(2*time.Minute), "user", "newest")

	items, err := store.GetAgentActivityRuns(ctx, userID, agentName, ActivityParams{
		Limit:  10,
		Cursor: &shared.Cursor{CreatedAt: base, ID: "00000000-0000-0000-0000-00000000000e"},
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000f", items[0].ID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000010", items[1].ID)
}

func TestPGStore_GetAgentActivityRuns_DoesNotDuplicateRowsAcrossCursorPages(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)
	userID := uniqueTestUserID(t)
	agentName := "activity-no-duplicates"
	seedTestAgent(t, testDB.Pool, userID, agentName)

	base := time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)
	ids := []string{
		"00000000-0000-0000-0000-000000000101",
		"00000000-0000-0000-0000-000000000102",
		"00000000-0000-0000-0000-000000000103",
		"00000000-0000-0000-0000-000000000104",
		"00000000-0000-0000-0000-000000000105",
	}
	for i, id := range ids {
		seedAgentRun(t, testDB.Pool, userID, agentName, id, base.Add(time.Duration(i)*time.Minute), "user", fmt.Sprintf("item-%d", i))
	}

	firstPage, err := store.GetAgentActivityRuns(ctx, userID, agentName, ActivityParams{Limit: 2})
	require.NoError(t, err)
	require.Len(t, firstPage, 3)

	secondPage, err := store.GetAgentActivityRuns(ctx, userID, agentName, ActivityParams{
		Limit:  2,
		Cursor: &shared.Cursor{CreatedAt: firstPage[1].CreatedAt, ID: firstPage[1].ID},
	})
	require.NoError(t, err)
	require.Len(t, secondPage, 3)

	seen := map[string]bool{}
	for _, item := range append(firstPage[:2], secondPage[:2]...) {
		assert.False(t, seen[item.ID], "duplicate row %s returned across pages", item.ID)
		seen[item.ID] = true
	}
}

func TestPGStore_GetAgentActivityRuns_ExistingAgentWithNoActivityReturnsEmptySlice(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)
	userID := uniqueTestUserID(t)
	agentName := "activity-empty"
	seedTestAgent(t, testDB.Pool, userID, agentName)

	items, err := store.GetAgentActivityRuns(ctx, userID, agentName, ActivityParams{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, items)
}

func uniqueTestUserID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("9%08d", time.Now().UnixNano()%100000000)
}

func seedTestAgent(t *testing.T, pool *pgxpool.Pool, userID, agentName string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
		ON CONFLICT (user_id, name) DO NOTHING
	`, userID, agentName)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE user_id = $1`, userID)
	})
}

func seedAgentRun(t *testing.T, pool *pgxpool.Pool, userID, agentName, id string, createdAt time.Time, triggeredBy, message string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO agent_runs (id, user_id, agent_name, triggered_by, action, status, message, created_at)
		VALUES ($1::uuid, $2, $3, $4, 'test action', 'completed', $5, $6)
	`, id, userID, agentName, triggeredBy, message, createdAt)
	require.NoError(t, err)
}
