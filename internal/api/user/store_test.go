package user

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nevaria-ai/backend/internal/api/shared"
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

func TestPGStore_GetAgentsForUser_NonExistentUser(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	agents, err := store.GetAgentsForUser(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestPGStore_GetAgentsForUser_InvalidUserID(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	agents, err := store.GetAgentsForUser(ctx, "not-a-valid-id")
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestPGStore_GetLylaThreadMessages_FirstPage_OldestFirst(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	cleanupLylaThreadTestData(t, ctx, pool)
	insertLylaThreadTestData(t, ctx, pool)

	params := LylaThreadParams{Limit: 3, Cursor: nil}
	messages, err := store.GetLylaThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	assert.Equal(t, "first message", messages[0].Content)
	assert.Equal(t, "second message", messages[1].Content)
	assert.Equal(t, "third message", messages[2].Content)
}

func TestPGStore_GetLylaThreadMessages_SameCreatedAt_OrdersByID(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	cleanupLylaThreadTestData(t, ctx, pool)

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, int64(123456789))
	require.NoError(t, err)

	now := time.Now()
	for i := 3; i >= 1; i-- {
		_, err := pool.Exec(ctx, `
			INSERT INTO lyla_thread (user_id, role, content, created_at)
			VALUES ($1, 'user', $2, $3)
		`, int64(123456789), fmt.Sprintf("same-time-%d", i), now)
		require.NoError(t, err)
	}

	params := LylaThreadParams{Limit: 10, Cursor: nil}
	messages, err := store.GetLylaThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	assert.True(t, messages[0].ID < messages[1].ID, "IDs should be in ascending order")
	assert.True(t, messages[1].ID < messages[2].ID, "IDs should be in ascending order")
}

func TestPGStore_GetLylaThreadMessages_CursorPage_ReturnsNewerRows(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	cleanupLylaThreadTestData(t, ctx, pool)
	insertLylaThreadTestData(t, ctx, pool)

	params := LylaThreadParams{Limit: 2, Cursor: nil}
	firstPage, err := store.GetLylaThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, firstPage, 3)

	cursor := &shared.Cursor{
		CreatedAt: firstPage[1].CreatedAt,
		ID:        firstPage[1].ID,
	}

	params = LylaThreadParams{Limit: 2, Cursor: cursor}
	secondPage, err := store.GetLylaThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)

	assert.Equal(t, "third message", secondPage[0].Content)
}

func TestPGStore_GetLylaThreadMessages_CursorPage_UsesIDTieBreakForEqualTimestamp(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	cleanupLylaThreadTestData(t, ctx, pool)

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, int64(123456789))
	require.NoError(t, err)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 3; i++ {
		_, err = pool.Exec(ctx, `
			INSERT INTO lyla_thread (user_id, role, content, created_at)
			VALUES ($1, 'assistant', $2, $3)
		`, int64(123456789), fmt.Sprintf("same-ts-%d", i), createdAt)
		require.NoError(t, err)
	}

	firstPage, err := store.GetLylaThreadMessages(ctx, "123456789", LylaThreadParams{Limit: 2})
	require.NoError(t, err)
	require.Len(t, firstPage, 3)

	cursor := &shared.Cursor{CreatedAt: firstPage[1].CreatedAt, ID: firstPage[1].ID}
	secondPage, err := store.GetLylaThreadMessages(ctx, "123456789", LylaThreadParams{Limit: 2, Cursor: cursor})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)

	assert.True(t, secondPage[0].CreatedAt.Equal(createdAt))
	assert.True(t, secondPage[0].ID > cursor.ID, "expected returned row to be strictly newer by id tie-break")
	assert.NotEqual(t, cursor.ID, secondPage[0].ID)
}

func TestPGStore_GetLylaThreadMessages_NoDuplicatesAcrossPages(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := NewPGStore(pool)

	cleanupLylaThreadTestData(t, ctx, pool)
	insertLylaThreadTestData(t, ctx, pool)

	allIDs := make(map[string]bool)

	params := LylaThreadParams{Limit: 2, Cursor: nil}
	firstPage, err := store.GetLylaThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)

	for _, msg := range firstPage[:2] {
		allIDs[msg.ID] = true
	}

	cursor := &shared.Cursor{
		CreatedAt: firstPage[1].CreatedAt,
		ID:        firstPage[1].ID,
	}

	params = LylaThreadParams{Limit: 2, Cursor: cursor}
	secondPage, err := store.GetLylaThreadMessages(ctx, "123456789", params)
	require.NoError(t, err)

	for _, msg := range secondPage {
		assert.False(t, allIDs[msg.ID], "duplicate ID found: %s", msg.ID)
		allIDs[msg.ID] = true
	}
}

func cleanupLylaThreadTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, "DELETE FROM lyla_thread WHERE user_id = $1", int64(123456789))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1", int64(123456789))
	require.NoError(t, err)
}

func insertLylaThreadTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, int64(123456789))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content, created_at)
		VALUES ($1, 'assistant', 'first message', NOW() - INTERVAL '2 seconds')
	`, int64(123456789))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content, created_at)
		VALUES ($1, 'user', 'second message', NOW() - INTERVAL '1 second')
	`, int64(123456789))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content, created_at)
		VALUES ($1, 'assistant', 'third message', NOW())
	`, int64(123456789))
	require.NoError(t, err)
}
