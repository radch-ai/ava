//go:build integration
// +build integration

package agent_test

import (
	"context"
	"encoding/json"
	"io"
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

	"github.com/nevaria-ai/backend/internal/api/agent"
	internalctx "github.com/nevaria-ai/backend/internal/ctx"
)

const (
	testUserID    = "123456789"
	testAgentName = "test-agent"
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
	pool.Exec(ctx, "DELETE FROM agent_tools WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM cron_jobs WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM heartbeat WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM agents WHERE user_id = $1", testUserID)
	pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1", testUserID)
}

func TestGetAgentConfiguration_Success(t *testing.T) {
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
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'robot', '{"soul": {"personality": "friendly", "identity": "test"}, "userContext": "test context"}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+testAgentName, nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config agent.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, testAgentName, config.Name)
	assert.Equal(t, "robot", config.Icon)
	assert.Equal(t, "test context", config.UserContext)
}

func TestGetAgentConfiguration_NotFound(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	_ = context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/user/agent/nonexistent", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPatchAgentConfiguration_Success(t *testing.T) {
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
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{"soul": {"personality": "old", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	patchBody := `{"soul": {"personality": "updated", "identity": "test"}}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config agent.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, "updated", config.Soul.Personality)
}

func TestPatchAgentConfiguration_WithHeartbeat(t *testing.T) {
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
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	patchBody := `{
		"soul": {"personality": "friendly", "identity": "test"},
		"heartbeat": {"model": "gpt-4", "enabled": true, "duration": "1hr", "content": "Check status"}
	}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config agent.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Heartbeat)
	assert.Equal(t, "gpt-4", config.Heartbeat.Model)
	assert.True(t, config.Heartbeat.Enabled)
	assert.Equal(t, "1hr", config.Heartbeat.Duration)
	assert.Equal(t, "Check status", config.Heartbeat.Content)
}

func TestPatchAgentConfiguration_WithCronJobs(t *testing.T) {
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
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	patchBody := `{
		"soul": {"personality": "friendly", "identity": "test"},
		"cronJobs": [
			{"time": "09:00", "frequency": "daily", "prompt": "Good morning!"},
			{"time": "18:00", "frequency": "weekly", "prompt": "Weekly report"}
		]
	}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config agent.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Len(t, config.CronJobs, 2)
}

func TestPatchAgentConfiguration_WithIcon(t *testing.T) {
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
		INSERT INTO agents (user_id, name, icon, config)
		VALUES ($1, $2, 'old-icon', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	patchBody := `{"icon": "new-icon"}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var config agent.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, "new-icon", config.Icon)
}

func TestCreateAgent_Success(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	createBody := `{
		"icon": "star",
		"soul": {"personality": "friendly", "identity": "new"},
		"userContext": "test context"
	}`
	req := httptest.NewRequest(http.MethodPost, "/user/agent/new-agent", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response agent.CreateAgentResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "new-agent", response.Name)
}

func TestCreateAgent_DuplicateNameSuffix(t *testing.T) {
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
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, 'duplicate', '{}'::jsonb)
	`, testUserID)
	require.NoError(t, err)

	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	handler := agent.NewHandler(service, zerolog.Nop())

	createBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/user/agent/duplicate", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response agent.CreateAgentResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "duplicate-2", response.Name)
}

func TestAgentRunsTable_Integration(t *testing.T) {
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
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'cron', 'daily digest', 'completed', 'done')
	`, testUserID, testAgentName)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE user_id = $1 AND agent_name = $2`, testUserID, testAgentName).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestAgentRunsStatusCheck_Integration(t *testing.T) {
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
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'user', 'test action', 'running', 'in progress')
	`, testUserID, testAgentName)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'user', 'test action', 'invalid_status', 'should fail')
	`, testUserID, testAgentName)
	require.Error(t, err)
}

func TestAgentRunsTriggeredByAllowsArbitraryMetadata_Integration(t *testing.T) {
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
		INSERT INTO agents (user_id, name, config)
		VALUES ($1, $2, '{}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'main', 'startup', 'completed', 'started')
	`, testUserID, testAgentName)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agent_runs (user_id, agent_name, triggered_by, action, status, message)
		VALUES ($1, $2, 'future_ui_metadata', 'test', 'completed', 'should pass')
	`, testUserID, testAgentName)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE user_id = $1 AND agent_name = $2`, testUserID, testAgentName).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestGetActivity_OrderingAndCursorPagination(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	seedIntegrationAgent(t, pool, testUserID, testAgentName)

	sameTime := time.Date(2026, 3, 22, 15, 0, 0, 0, time.UTC)
	insertIntegrationRun(t, pool, testUserID, testAgentName, "00000000-0000-0000-0000-00000000000d", sameTime.Add(-2*time.Minute), "self", "oldest")
	insertIntegrationRun(t, pool, testUserID, testAgentName, "00000000-0000-0000-0000-00000000000e", sameTime, "user", "same-time-low")
	insertIntegrationRun(t, pool, testUserID, testAgentName, "00000000-0000-0000-0000-00000000000f", sameTime, "user", "same-time-high")
	insertIntegrationRun(t, pool, testUserID, testAgentName, "00000000-0000-0000-0000-000000000010", sameTime.Add(2*time.Minute), "cron", "newest")

	handler := newIntegrationHandler(pool)
	firstReq := httptest.NewRequest(http.MethodGet, "/user/agent/"+testAgentName+"/activity?limit=2", nil)
	firstReq = firstReq.WithContext(internalctx.WithUserID(ctx, testUserID))
	firstW := httptest.NewRecorder()
	handler.GetActivity(firstW, firstReq)

	require.Equal(t, http.StatusOK, firstW.Code)
	var firstPage agent.ActivityPage
	err := json.NewDecoder(firstW.Body).Decode(&firstPage)
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 2)
	require.NotNil(t, firstPage.NextCursor)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000d", firstPage.Items[0].ID)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000e", firstPage.Items[1].ID)

	secondReq := httptest.NewRequest(http.MethodGet, "/user/agent/"+testAgentName+"/activity?limit=2&cursor="+*firstPage.NextCursor, nil)
	secondReq = secondReq.WithContext(internalctx.WithUserID(ctx, testUserID))
	secondW := httptest.NewRecorder()
	handler.GetActivity(secondW, secondReq)

	require.Equal(t, http.StatusOK, secondW.Code)
	var secondPage agent.ActivityPage
	err = json.NewDecoder(secondW.Body).Decode(&secondPage)
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 2)
	assert.Equal(t, "00000000-0000-0000-0000-00000000000f", secondPage.Items[0].ID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000010", secondPage.Items[1].ID)
}

func TestGetActivity_ExistingAgentWithNoActivity(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	runMigrations(t)
	defer cleanupTestData(t, pool)
	seedIntegrationAgent(t, pool, testUserID, testAgentName)

	handler := newIntegrationHandler(pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+testAgentName+"/activity", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), testUserID))
	w := httptest.NewRecorder()
	handler.GetActivity(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var page agent.ActivityPage
	err := json.NewDecoder(w.Body).Decode(&page)
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.Nil(t, page.NextCursor)
}

func TestGetActivity_MissingAgentReturnsNotFound(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	runMigrations(t)
	defer cleanupTestData(t, pool)

	handler := newIntegrationHandler(pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/missing/activity", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), testUserID))
	w := httptest.NewRecorder()
	handler.GetActivity(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "agent not found", decodeIntegrationError(t, w.Body))
}

func TestGetActivity_InvalidCursorReturnsBadRequest(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	runMigrations(t)
	defer cleanupTestData(t, pool)
	seedIntegrationAgent(t, pool, testUserID, testAgentName)

	handler := newIntegrationHandler(pool)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+testAgentName+"/activity?cursor=invalid-cursor", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), testUserID))
	w := httptest.NewRecorder()
	handler.GetActivity(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid cursor", decodeIntegrationError(t, w.Body))
}

func TestGetActivity_NonExactVariantRoutesToActivity(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	runMigrations(t)
	defer cleanupTestData(t, pool)
	seedIntegrationAgent(t, pool, testUserID, testAgentName)

	handler := newIntegrationHandler(pool)
	// Path with /activity/extra now routes to activity (agent name = testAgentName+"/activity/extra" with suffix stripped)
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+testAgentName+"/activity/extra", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), testUserID))
	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	// Now routes to activity since path ends with /activity
	// But agent "testAgentName/activity/extra" doesn't exist, so returns not found
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetActivity_TrailingSlashRoutesToActivity(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	runMigrations(t)
	defer cleanupTestData(t, pool)
	seedIntegrationAgent(t, pool, testUserID, testAgentName)

	handler := newIntegrationHandler(pool)
	// Trailing slash is stripped, then /activity suffix is stripped, leaving just testAgentName
	req := httptest.NewRequest(http.MethodGet, "/user/agent/"+testAgentName+"/activity/", nil)
	req = req.WithContext(internalctx.WithUserID(context.Background(), testUserID))
	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	// Routes to GetActivity with agent name = testAgentName (suffix stripped)
	require.Equal(t, http.StatusOK, w.Code)
}

func newIntegrationHandler(pool *pgxpool.Pool) *agent.Handler {
	store := agent.NewPGStore(pool)
	service := agent.NewService(store)
	return agent.NewHandler(service, zerolog.Nop())
}

func seedIntegrationAgent(t *testing.T, pool *pgxpool.Pool, userID, agentName string) {
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

func insertIntegrationRun(t *testing.T, pool *pgxpool.Pool, userID, agentName, id string, createdAt time.Time, triggeredBy, message string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO agent_runs (id, user_id, agent_name, triggered_by, action, status, message, created_at)
		VALUES ($1::uuid, $2, $3, $4, 'integration action', 'completed', $5, $6)
	`, id, userID, agentName, triggeredBy, message, createdAt)
	require.NoError(t, err)
}

func decodeIntegrationError(t *testing.T, body io.Reader) string {
	t.Helper()
	var response map[string]string
	err := json.NewDecoder(body).Decode(&response)
	require.NoError(t, err)
	return response["error"]
}
