//go:build integration
// +build integration

package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radch-ai/ava/services/backend/internal/testhelpers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalctx "github.com/radch-ai/ava/services/backend/internal/ctx"
)

const (
	itTestUserID    = "123456789"
	itTestAgentName = "test-agent"
)

func itCleanupTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM agent_runs WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM agent_tools WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM cron_jobs WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM heartbeat WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM agents WHERE user_id = $1", itTestUserID)
	pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1", itTestUserID)
}

func TestGetAgentConfiguration_Integration_Success(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'robot', '{"soul": {"personality": "friendly", "identity": "test"}, "userContext": "test context"}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+itTestAgentName, nil)
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, itTestAgentName, config.Name)
	assert.Equal(t, "robot", config.Icon)
	assert.Equal(t, "test context", config.UserContext)
}

func TestGetAgentConfiguration_Integration_NotFound(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agent/nonexistent", nil)
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPatchAgentConfiguration_Integration_Success(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{"soul": {"personality": "old", "identity": "test"}}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	patchBody := `{"soul": {"personality": "updated", "identity": "test"}}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+itTestAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, "updated", config.Soul.Personality)
}

func TestPatchAgentConfiguration_Integration_WithHeartbeat(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	patchBody := `{
		"soul": {"personality": "friendly", "identity": "test"},
		"heartbeat": {"model": "gpt-4", "enabled": true, "duration": "1hr", "content": "Check status"}
	}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+itTestAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Heartbeat)
	assert.Equal(t, "gpt-4", config.Heartbeat.Model)
	assert.True(t, config.Heartbeat.Enabled)
	assert.Equal(t, "1hr", config.Heartbeat.Duration)
	assert.Equal(t, "Check status", config.Heartbeat.Content)
}

func TestPatchAgentConfiguration_Integration_WithCronJobs(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	patchBody := `{
		"soul": {"personality": "friendly", "identity": "test"},
		"cronJobs": [
			{"time": "09:00", "frequency": "daily", "prompt": "Good morning!"},
			{"time": "18:00", "frequency": "weekly", "prompt": "Weekly report"}
		]
	}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+itTestAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Len(t, config.CronJobs, 2)
}

func TestPatchAgentConfiguration_Integration_WithIcon(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'old-icon', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	patchBody := `{"icon": "new-icon"}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+itTestAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, "new-icon", config.Icon)
}

func TestCreateAgent_Integration_Success(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	createBody := `{
		"icon": "star",
		"soul": {"personality": "friendly", "identity": "new"},
		"userContext": "test context"
	}`
	req := httptest.NewRequest(http.MethodPost, "/user/agent/new-agent", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response CreateAgentResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "new-agent", response.Name)
}

func TestCreateAgent_Integration_DuplicateNameSuffix(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, 'duplicate', '{}'::jsonb)
	`, itTestUserID)
	require.NoError(t, err)

	store := NewPGStore(testDB.Pool)
	service := NewService(store)
	handler := NewHandler(service, zerolog.Nop())

	createBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/user/agent/duplicate", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), itTestUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response CreateAgentResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "duplicate-2", response.Name)
}

func TestAgentRunsTable_Integration(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'cron', 'daily digest', 'completed', 'done')
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	var count int
	err = testDB.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE user_id = $1 AND agent_name = $2`, itTestUserID, itTestAgentName).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestAgentRunsStatusCheck_Integration(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'user', 'test action', 'running', 'in progress')
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'user', 'test action', 'invalid_status', 'should fail')
	`, itTestUserID, itTestAgentName)
	require.Error(t, err)
}

func TestAgentRunsTriggeredByAllowsArbitraryMetadata_Integration(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	_, err := testDB.Pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, itTestUserID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'main', 'startup', 'completed', 'started')
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'future_ui_metadata', 'test', 'completed', 'should pass')
	`, itTestUserID, itTestAgentName)
	require.NoError(t, err)

	var count int
	err = testDB.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE user_id = $1 AND agent_name = $2`, itTestUserID, itTestAgentName).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestGetActivity_Integration_OrderingAndCursorPagination(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	itSeedIntegrationAgent(t, testDB.Pool, itTestUserID, itTestAgentName)

	sameTime := time.Date(2026, 3, 22, 15, 0, 0, 0, time.UTC)
	itInsertIntegrationRun(t, testDB.Pool, itTestUserID, itTestAgentName, "00000000-0000-0000-0000-00000000000d", sameTime.Add(-2*time.Minute), "self", "oldest")
	itInsertIntegrationRun(t, testDB.Pool, itTestUserID, itTestAgentName, "00000000-0000-0000-0000-00000000000e", sameTime, "user", "same-time-low")
	itInsertIntegrationRun(t, testDB.Pool, itTestUserID, itTestAgentName, "00000000-0000-0000-0000-00000000000f", sameTime, "user", "same-time-high")
	itInsertIntegrationRun(t, testDB.Pool, itTestUserID, itTestAgentName, "00000000-0000-0000-0000-000000000010", sameTime.Add(2*time.Minute), "cron", "newest")

	handler := itNewIntegrationHandler(testDB.Pool)
	firstReq := httptest.NewRequest(http.MethodGet, "/user/agent/"+itTestAgentName+"/activity?limit=2", nil)
	firstReq = firstReq.WithContext(internalctx.WithUserID(ctx, itTestUserID))
	firstW := httptest.NewRecorder()
	handler.GetActivity(firstW, firstReq)

	require.Equal(t, http.StatusOK, firstW.Code)
	var firstPage ActivityPage
	err := json.NewDecoder(firstW.Body).Decode(&firstPage)
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 2)
	require.NotNil(t, firstPage.NextCursor)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000d", firstPage.Items[0].ID)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000e", firstPage.Items[1].ID)

	secondReq := httptest.NewRequest(http.MethodGet, "/user/agent/"+itTestAgentName+"/activity?limit=2&cursor="+*firstPage.NextCursor, nil)
	secondReq = secondReq.WithContext(internalctx.WithUserID(ctx, itTestUserID))
	secondW := httptest.NewRecorder()
	handler.GetActivity(secondW, secondReq)

	require.Equal(t, http.StatusOK, secondW.Code)
	var secondPage ActivityPage
	err = json.NewDecoder(secondW.Body).Decode(&secondPage)
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 2)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000f", secondPage.Items[0].ID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000010", secondPage.Items[1].ID)
}

func TestGetActivity_Integration_ExistingAgentWithNoActivity(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	itSeedIntegrationAgent(t, testDB.Pool, itTestUserID, itTestAgentName)

	handler := itNewIntegrationHandler(testDB.Pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+itTestAgentName+"/activity", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), itTestUserID))
	w := httptest.NewRecorder()
	handler.GetActivity(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var page ActivityPage
	err := json.NewDecoder(w.Body).Decode(&page)
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.Nil(t, page.NextCursor)
}

func TestGetActivity_Integration_MissingAgentReturnsEmptyPage(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	handler := itNewIntegrationHandler(testDB.Pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/missing/activity", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), itTestUserID))
	w := httptest.NewRecorder()
	handler.GetActivity(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var page ActivityPage
	err := json.NewDecoder(w.Body).Decode(&page)
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.Nil(t, page.NextCursor)
}

func TestGetActivity_Integration_InvalidCursorReturnsBadRequest(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	itSeedIntegrationAgent(t, testDB.Pool, itTestUserID, itTestAgentName)

	handler := itNewIntegrationHandler(testDB.Pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+itTestAgentName+"/activity?cursor=invalid-cursor", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), itTestUserID))
	w := httptest.NewRecorder()
	handler.GetActivity(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid cursor", itDecodeIntegrationError(t, w.Body))
}

func TestGetActivity_Integration_NonExactVariantRoutesToActivity(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	itSeedIntegrationAgent(t, testDB.Pool, itTestUserID, itTestAgentName)

	handler := itNewIntegrationHandler(testDB.Pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+itTestAgentName+"/activity/extra", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), itTestUserID))
	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetActivity_Integration_TrailingSlashRoutesToActivity(t *testing.T) {
	ctx := context.Background()
	testDB := testhelpers.SetupTestDB(ctx, t)
	defer itCleanupTestData(t, testDB.Pool)

	itSeedIntegrationAgent(t, testDB.Pool, itTestUserID, itTestAgentName)

	handler := itNewIntegrationHandler(testDB.Pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+itTestAgentName+"/activity/", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), itTestUserID))
	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func itNewIntegrationHandler(pool *pgxpool.Pool) *Handler {
	store := NewPGStore(pool)
	service := NewService(store)
	return NewHandler(service, zerolog.Nop())
}

func itSeedIntegrationAgent(t *testing.T, pool *pgxpool.Pool, userID, agentName string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
	`, userID, agentName)
	require.NoError(t, err)
}

func itInsertIntegrationRun(t *testing.T, pool *pgxpool.Pool, userID, agentName, id string, createdAt time.Time, triggeredBy, message string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO agent_runs (id, user_id, agent_name, triggered_by, action, status, message, created_at)
		VALUES ($1::uuid, $2, $3, $4, 'integration action', 'completed', $5, $6)
	`, id, userID, agentName, triggeredBy, message, createdAt)
	require.NoError(t, err)
}

func itDecodeIntegrationError(t *testing.T, body io.Reader) string {
	t.Helper()
	var response map[string]string
	err := json.NewDecoder(body).Decode(&response)
	require.NoError(t, err)
	return response["error"]
}
