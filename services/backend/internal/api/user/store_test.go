//go:build integration
// +build integration

package user

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radch-ai/ava/services/backend/internal/api/shared"
	"github.com/radch-ai/ava/services/backend/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPGStore_ImplementsStoreInterface(t *testing.T) {
	store := &PGStore{}
	var _ Store = store
}

func TestPGStore_GetAgentsForUser_NonExistentUser(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	agents, err := store.GetAgentsForUser(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestPGStore_GetAgentsForUser_InvalidUserID(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	agents, err := store.GetAgentsForUser(ctx, "not-a-valid-id")
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestPGStore_GetThreadMessages_FirstPage_OldestFirst(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	cleanupThreadTestData(t, ctx, testDB.Pool)
	insertThreadTestData(t, ctx, testDB.Pool)

	params := ThreadParams{Limit: 3, Cursor: nil}
	messages, err := store.GetThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	assert.Equal(t, "first message", messages[0].Content)
	assert.Equal(t, "second message", messages[1].Content)
	assert.Equal(t, "third message", messages[2].Content)
}

func TestPGStore_GetThreadMessages_SameCreatedAt_OrdersByID(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	cleanupThreadTestData(t, ctx, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, int64(123456789))
	require.NoError(t, err)

	now := time.Now()
	for i := 3; i >= 1; i-- {
		_, err := testDB.Pool.Exec(ctx, `
			INSERT INTO thread (user_id, role, content, created_at)
			VALUES ($1, 'user', $2, $3)
		`, int64(123456789), fmt.Sprintf("same-time-%d", i), now)
		require.NoError(t, err)
	}

	params := ThreadParams{Limit: 10, Cursor: nil}
	messages, err := store.GetThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	assert.True(t, messages[0].ID < messages[1].ID, "IDs should be in ascending order")
	assert.True(t, messages[1].ID < messages[2].ID, "IDs should be in ascending order")
}

func TestPGStore_GetThreadMessages_CursorPage_ReturnsNewerRows(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	cleanupThreadTestData(t, ctx, testDB.Pool)
	insertThreadTestData(t, ctx, testDB.Pool)

	params := ThreadParams{Limit: 2, Cursor: nil}
	firstPage, err := store.GetThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, firstPage, 3)

	cursor := &shared.Cursor{
		CreatedAt: firstPage[1].CreatedAt,
		ID:        firstPage[1].ID,
	}

	params = ThreadParams{Limit: 2, Cursor: cursor}
	secondPage, err := store.GetThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)

	assert.Equal(t, "third message", secondPage[0].Content)
}

func TestPGStore_GetThreadMessages_CursorPage_UsesIDTieBreakForEqualTimestamp(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	cleanupThreadTestData(t, ctx, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, int64(123456789))
	require.NoError(t, err)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 3; i++ {
		_, err = testDB.Pool.Exec(ctx, `
			INSERT INTO thread (user_id, role, content, created_at)
			VALUES ($1, 'assistant', $2, $3)
		`, int64(123456789), fmt.Sprintf("same-ts-%d", i), createdAt)
		require.NoError(t, err)
	}

	firstPage, err := store.GetThreadMessages(ctx, "123456789", ThreadParams{Limit: 2})
	require.NoError(t, err)
	require.Len(t, firstPage, 3)

	cursor := &shared.Cursor{CreatedAt: firstPage[1].CreatedAt, ID: firstPage[1].ID}
	secondPage, err := store.GetThreadMessages(ctx, "123456789", ThreadParams{Limit: 2, Cursor: cursor})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)

	assert.True(t, secondPage[0].CreatedAt.Equal(createdAt))
	assert.True(t, secondPage[0].ID > cursor.ID, "expected returned row to be strictly newer by id tie-break")
	assert.NotEqual(t, cursor.ID, secondPage[0].ID)
}

func TestPGStore_GetThreadMessages_NoDuplicatesAcrossPages(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	store := NewPGStore(testDB.Pool)

	cleanupThreadTestData(t, ctx, testDB.Pool)
	insertThreadTestData(t, ctx, testDB.Pool)

	allIDs := make(map[string]bool)

	params := ThreadParams{Limit: 2, Cursor: nil}
	firstPage, err := store.GetThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)

	for _, msg := range firstPage[:2] {
		allIDs[msg.ID] = true
	}

	cursor := &shared.Cursor{
		CreatedAt: firstPage[1].CreatedAt,
		ID:        firstPage[1].ID,
	}

	params = ThreadParams{Limit: 2, Cursor: cursor}
	secondPage, err := store.GetThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)

	for _, msg := range secondPage {
		assert.False(t, allIDs[msg.ID], "duplicate ID found: %s", msg.ID)
		allIDs[msg.ID] = true
	}
}

func cleanupThreadTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, "DELETE FROM thread WHERE user_id = $1", int64(123456789))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1", int64(123456789))
	require.NoError(t, err)
}

func insertThreadTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, int64(123456789))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO thread (user_id, role, content, created_at)
		VALUES ($1, 'assistant', 'first message', NOW() - INTERVAL '2 seconds')
	`, int64(123456789))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO thread (user_id, role, content, created_at)
		VALUES ($1, 'user', 'second message', NOW() - INTERVAL '1 second')
	`, int64(123456789))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO thread (user_id, role, content, created_at)
		VALUES ($1, 'assistant', 'third message', NOW())
	`, int64(123456789))
	require.NoError(t, err)
}
