//go:build integration
// +build integration

package user_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nevaria-ai/backend/internal/api/user"
	internalctx "github.com/nevaria-ai/backend/internal/ctx"
)

const (
	testUserID = "123456789"
	agentName1 = "lyla"
	agentName2 = "worker"
	agentName3 = "assistant"
)

func getTestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

func runMigrations(t *testing.T) {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")
	migrationsPath := filepath.Join(wd, "..", "..", "..", "..", "schema", "migrations")

	downCmd := exec.Command("migrate", "-database", getTestDBURL(), "-path", migrationsPath, "down")
	_ = downCmd.Run()

	upCmd := exec.Command("migrate", "-database", getTestDBURL(), "-path", migrationsPath, "up")
	output, err := upCmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "no change") {
			require.NoError(t, err, "Failed to run migrations: %s", string(output))
		}
	}
}

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(getTestDBURL())
	require.NoError(t, err)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to test database: %v", err)
		return nil
	}

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Skipping test: cannot connect to test database: %v", err)
		return nil
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

func cleanupTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM agent_runs WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM lyla_thread WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM agent_tools WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM cron_jobs WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM heartbeat WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM agents WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1", testUserID)
}

func setupTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err, "Failed to insert test user")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'robot', '{"soul": {"personality": "friendly", "identity": "primary"}}'::jsonb)
	`, testUserID, agentName1)
	require.NoError(t, err, "Failed to insert test agent 1")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'gear', '{"soul": {"personality": "efficient", "identity": "background"}}'::jsonb)
	`, testUserID, agentName2)
	require.NoError(t, err, "Failed to insert test agent 2")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'star', '{"soul": {"personality": "helpful", "identity": "assistant"}}'::jsonb)
	`, testUserID, agentName3)
	require.NoError(t, err, "Failed to insert test agent 3")
}

func TestGetUserAgents_Integration_Success(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	setupTestData(t, ctx, pool)

	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response user.GetAgentsResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.Agents, 3)

	var agentNames []string
	for _, agent := range response.Agents {
		agentNames = append(agentNames, agent.Name)
	}
	assert.ElementsMatch(t, agentNames, []string{agentName1, agentName2, agentName3})
}

func TestGetUserAgents_Integration_NoAgents(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	_ = context.Background()
	runMigrations(t)

	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)
	noAgentUserID := "987654321"
	reqCtx := internalctx.WithUserID(context.Background(), noAgentUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response user.GetAgentsResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.Agents, 0)
	assert.Empty(t, response.Agents)
}

func TestGetUserAgents_Integration_MissingUserID(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	_ = context.Background()
	runMigrations(t)

	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)

	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal error", response["error"])
}

func TestLylaThreadTable_Integration(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content)
		VALUES ($1, 'assistant', 'hello')
	`, testUserID)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM lyla_thread WHERE user_id = $1`, testUserID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestLylaThreadRoleCheck_Integration(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content)
		VALUES ($1, 'user', 'hi there')
	`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content)
		VALUES ($1, 'invalid_role', 'should fail')
	`, testUserID)
	require.Error(t, err)
}

func TestGetLylaThread_Integration_FirstPage_NewestFirst(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	_, err := pool.Exec(ctx, `DELETE FROM lyla_thread WHERE user_id = $1`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'assistant', 'first', NOW() - INTERVAL '2 seconds')`, testUserID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'user', 'second', NOW() - INTERVAL '1 second')`, testUserID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'assistant', 'third', NOW())`, testUserID)
	require.NoError(t, err)

	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=10", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response user.LylaThreadPage
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	require.Len(t, response.Items, 3)
	assert.Equal(t, "third", response.Items[0].Content)
	assert.Equal(t, "second", response.Items[1].Content)
	assert.Equal(t, "first", response.Items[2].Content)
}

func TestGetLylaThread_Integration_CursorPage(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	_, err := pool.Exec(ctx, `DELETE FROM lyla_thread WHERE user_id = $1`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'assistant', $2, NOW() - INTERVAL '1 second' * $3)`, testUserID, string(rune('a'+i)), 5-i)
		require.NoError(t, err)
	}

	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=2", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var firstPage user.LylaThreadPage
	err = json.NewDecoder(w.Body).Decode(&firstPage)
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 2)
	require.NotNil(t, firstPage.NextCursor)

	cursorReq := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=2&cursor="+*firstPage.NextCursor, nil)
	cursorReq = cursorReq.WithContext(reqCtx)

	w2 := httptest.NewRecorder()
	handler.GetLylaThread(w2, cursorReq)

	require.Equal(t, http.StatusOK, w2.Code)

	var secondPage user.LylaThreadPage
	err = json.NewDecoder(w2.Body).Decode(&secondPage)
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 2)

	for _, item := range secondPage.Items {
		isStrictlyOlder := item.CreatedAt.Before(firstPage.Items[1].CreatedAt) ||
			(item.CreatedAt.Equal(firstPage.Items[1].CreatedAt) && item.ID < firstPage.Items[1].ID)
		assert.True(t, isStrictlyOlder, "expected second-page row to be strictly older than cursor tuple")
	}
}

func TestGetLylaThread_Integration_EmptyThread(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response user.LylaThreadPage
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Items)
	assert.Nil(t, response.NextCursor)
}

func TestGetLylaThread_Integration_InvalidCursor(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	runMigrations(t)
	defer cleanupTestData(t, pool)

	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?cursor=invalid-cursor", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "invalid cursor", response["error"])
}
