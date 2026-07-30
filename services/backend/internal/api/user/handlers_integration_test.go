//go:build integration
// +build integration

package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radch-ai/ava/services/backend/internal/testhelpers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalctx "github.com/radch-ai/ava/services/backend/internal/ctx"
)

const (
	itTestUserID = "123456789"
	itAgentName1 = "lyla"
	itAgentName2 = "worker"
	itAgentName3 = "assistant"
)

func itCleanupTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM agent_runs WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM lyla_thread WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM agent_tools WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM cron_jobs WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM heartbeat WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM agents WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1", itTestUserID)
}

func itSetupTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err, "Failed to insert test user")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'robot', '{"soul": {"personality": "friendly", "identity": "primary"}}'::jsonb)
	`, itTestUserID, itAgentName1)
	require.NoError(t, err, "Failed to insert test agent 1")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'gear', '{"soul": {"personality": "efficient", "identity": "background"}}'::jsonb)
	`, itTestUserID, itAgentName2)
	require.NoError(t, err, "Failed to insert test agent 2")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'star', '{"soul": {"personality": "helpful", "identity": "assistant"}}'::jsonb)
	`, itTestUserID, itAgentName3)
	require.NoError(t, err, "Failed to insert test agent 3")
}

func TestGetUserAgents_Integration_Success(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	itSetupTestData(t, ctx, testDB.Pool)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response GetAgentsResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.Agents, 2)

	var agentNames []string
	for _, agent := range response.Agents {
		agentNames = append(agentNames, agent.Name)
	}
	assert.ElementsMatch(t, agentNames, []string{itAgentName2, itAgentName3})
}

func TestGetUserAgents_Integration_NoAgents(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)
	noAgentUserID := "987654321"
	reqCtx := internalctx.WithUserID(context.Background(), noAgentUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response GetAgentsResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.Agents, 0)
	assert.Empty(t, response.Agents)
}

func TestGetUserAgents_Integration_MissingUserID(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

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
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content)
		VALUES ($1, 'assistant', 'hello')
	`, itTestUserID)
	require.NoError(t, err)

	var count int
	err = testDB.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM lyla_thread WHERE user_id = $1`, itTestUserID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestLylaThreadRoleCheck_Integration(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content)
		VALUES ($1, 'user', 'hi there')
	`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO lyla_thread (user_id, role, content)
		VALUES ($1, 'invalid_role', 'should fail')
	`, itTestUserID)
	require.Error(t, err)
}

func TestGetLylaThread_Integration_FirstPage_NewestFirst(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `DELETE FROM lyla_thread WHERE user_id = $1`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'assistant', 'first', NOW() - INTERVAL '2 seconds')`, itTestUserID)
	require.NoError(t, err)
	_, err = testDB.Pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'user', 'second', NOW() - INTERVAL '1 second')`, itTestUserID)
	require.NoError(t, err)
	_, err = testDB.Pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'assistant', 'third', NOW())`, itTestUserID)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=10", nil)
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response LylaThreadPage
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	require.Len(t, response.Items, 3)
	assert.Equal(t, "first", response.Items[0].Content)
	assert.Equal(t, "second", response.Items[1].Content)
	assert.Equal(t, "third", response.Items[2].Content)
}

func TestGetLylaThread_Integration_CursorPage(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `DELETE FROM lyla_thread WHERE user_id = $1`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = testDB.Pool.Exec(ctx, `INSERT INTO lyla_thread (user_id, role, content, created_at) VALUES ($1, 'assistant', $2, NOW() - INTERVAL '1 second' * $3)`, itTestUserID, string(rune('a'+i)), 5-i)
		require.NoError(t, err)
	}

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=2", nil)
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var firstPage LylaThreadPage
	err = json.NewDecoder(w.Body).Decode(&firstPage)
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 2)
	require.NotNil(t, firstPage.NextCursor)

	cursorReq := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=2&cursor="+*firstPage.NextCursor, nil)
	cursorReq = cursorReq.WithContext(reqCtx)

	w2 := httptest.NewRecorder()
	handler.GetLylaThread(w2, cursorReq)

	require.Equal(t, http.StatusOK, w2.Code)

	var secondPage LylaThreadPage
	err = json.NewDecoder(w2.Body).Decode(&secondPage)
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 2)

	for _, item := range secondPage.Items {
		isStrictlyNewer := item.CreatedAt.After(firstPage.Items[1].CreatedAt) ||
			(item.CreatedAt.Equal(firstPage.Items[1].CreatedAt) && item.ID > firstPage.Items[1].ID)
		assert.True(t, isStrictlyNewer, "expected second-page row to be strictly newer than cursor tuple")
	}
}

func TestGetLylaThread_Integration_EmptyThread(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread", nil)
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response LylaThreadPage
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Items)
	assert.Nil(t, response.NextCursor)
}

func TestGetLylaThread_Integration_InvalidCursor(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?cursor=invalid-cursor", nil)
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "invalid cursor", response["error"])
}
