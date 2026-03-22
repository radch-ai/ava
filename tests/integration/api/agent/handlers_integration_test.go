//go:build integration
// +build integration

package agent_test

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

	"github.com/nevaria-ai/backend/internal/api/agent"
	internalctx "github.com/nevaria-ai/backend/internal/ctx"
)

const (
	testUserID    = "00000000-0000-0000-0000-000000000001"
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
	pool.Exec(ctx, "DELETE FROM agent_tools WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM cron_jobs WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM heartbeat WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM agents WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1::uuid", testUserID)
}

func TestGetAgentConfiguration_Success(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, icon, config)
		VALUES ($1::uuid, $2, 'lyla', 'robot', '{"soul": {"personality": "friendly", "identity": "test"}, "userContext": "test context"}'::jsonb)
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

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, $2, 'lyla', '{"soul": {"personality": "old", "identity": "test"}}'::jsonb)
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

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, $2, 'lyla', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
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

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, $2, 'lyla', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
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

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, icon, config)
		VALUES ($1::uuid, $2, 'lyla', 'old-icon', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
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

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
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

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, 'duplicate', 'lyla', '{}'::jsonb)
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
