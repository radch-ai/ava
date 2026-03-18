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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalctx "github.com/nevaria-ai/backend/internal/ctx"
	"github.com/nevaria-ai/backend/internal/user"
)

const (
	testUserID    = "00000000-0000-0000-0000-000000000001"
	agentName1    = "lyla"
	agentName2    = "worker"
	agentName3    = "assistant"
	testAgentName = "test-agent"
)

// getTestDBURL returns the database URL for testing.
func getTestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

// runMigrations runs database migrations using the migrate CLI.
func runMigrations(t *testing.T) {
	t.Helper()

	// Get absolute path to migrations directory
	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")
	migrationsPath := filepath.Join(wd, "..", "..", "..", "schema", "migrations")

	// Reset database by going down then up
	downCmd := exec.Command("migrate", "-database", getTestDBURL(), "-path", migrationsPath, "down")
	_ = downCmd.Run() // Ignore error if already down

	upCmd := exec.Command("migrate", "-database", getTestDBURL(), "-path", migrationsPath, "up")
	output, err := upCmd.CombinedOutput()
	if err != nil {
		// Check if it's just "no change" which is fine
		if !strings.Contains(string(output), "no change") {
			require.NoError(t, err, "Failed to run migrations: %s", string(output))
		}
	}
}

// setupTestDB creates a test database connection pool.
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

// cleanupTestData removes test data from the database.
func cleanupTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM agent_tools WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM cron_jobs WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM heartbeat WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM agents WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1::uuid", testUserID)
}

// setupTestData creates test user and agents.
func setupTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	// Create test user
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err, "Failed to insert test user")

	// Create test agents with new schema (agent_name, icon columns)
	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, icon, config)
		VALUES ($1::uuid, $2, 'lyla', 'robot', '{"soul": {"personality": "friendly", "identity": "primary"}}'::jsonb)
	`, testUserID, agentName1)
	require.NoError(t, err, "Failed to insert test agent 1")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, icon, config)
		VALUES ($1::uuid, $2, 'worker', 'gear', '{"soul": {"personality": "efficient", "identity": "background"}}'::jsonb)
	`, testUserID, agentName2)
	require.NoError(t, err, "Failed to insert test agent 2")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, icon, config)
		VALUES ($1::uuid, $2, 'lyla', 'star', '{"soul": {"personality": "helpful", "identity": "assistant"}}'::jsonb)
	`, testUserID, agentName3)
	require.NoError(t, err, "Failed to insert test agent 3")
}

// TestGetUserAgents_Integration_Success tests the full request/response cycle.
func TestGetUserAgents_Integration_Success(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test data
	setupTestData(t, ctx, pool)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Create request with mocked auth
	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)

	// Record response
	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var response user.AgentsResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.AgentNames, 3)
	assert.ElementsMatch(t, response.AgentNames, []string{agentName1, agentName2, agentName3})
}

// TestGetUserAgents_Integration_NoAgents tests empty result.
func TestGetUserAgents_Integration_NoAgents(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	_ = context.Background()
	runMigrations(t)

	// Create handler (no test data setup - user has no agents)
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Create request with mocked auth (user with no agents)
	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)
	// Use a different UUID that doesn't have agents
	noAgentUserID := uuid.New().String()
	reqCtx := internalctx.WithUserID(context.Background(), noAgentUserID)
	req = req.WithContext(reqCtx)

	// Record response
	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var response user.AgentsResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Len(t, response.AgentNames, 0)
	assert.Empty(t, response.AgentNames)
}

// TestGetUserAgents_Integration_MissingUserID tests authentication.
func TestGetUserAgents_Integration_MissingUserID(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	_ = context.Background()
	runMigrations(t)

	// Create handler without setting up user ID in context
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Create request without user ID in context
	req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)

	// Record response
	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	// Verify response
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal error", response["error"])
}

// TestGetAgentConfiguration_Success tests successful retrieval of agent configuration.
func TestGetAgentConfiguration_Success(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test data
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, icon, config)
		VALUES ($1::uuid, $2, 'lyla', 'robot', '{"soul": {"personality": "friendly", "identity": "test"}, "userContext": "test context"}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Make request with user context
	req := httptest.NewRequest(http.MethodGet, "/user/agents/"+testAgentName, nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.GetAgentConfiguration(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var config user.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, testAgentName, config.Name)
	assert.Equal(t, "robot", config.Icon)
	assert.Equal(t, "test context", config.UserContext)
}

// TestPatchAgentConfiguration_Success tests successful update of agent configuration.
func TestPatchAgentConfiguration_Success(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test data
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, $2, 'lyla', '{"soul": {"personality": "old", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Make patch request
	patchBody := `{"soul": {"personality": "updated", "identity": "test"}}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agents/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var config user.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, "updated", config.Soul.Personality)
}

// TestGetAgentConfiguration_NotFound tests retrieval of non-existent agent.
func TestGetAgentConfiguration_NotFound(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	_ = context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Make request for non-existent agent
	req := httptest.NewRequest(http.MethodGet, "/user/agents/nonexistent", nil)
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.GetAgentConfiguration(w, req)

	// Verify response
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestPatchAgentConfiguration_WithHeartbeat tests patch with heartbeat configuration.
func TestPatchAgentConfiguration_WithHeartbeat(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test data
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, $2, 'lyla', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Make patch request with heartbeat
	patchBody := `{
		"soul": {"personality": "friendly", "identity": "test"},
		"heartbeat": {"model": "gpt-4", "enabled": true, "duration": "1hr", "content": "Check status"}
	}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agents/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var config user.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	require.NotNil(t, config.Heartbeat)
	assert.Equal(t, "gpt-4", config.Heartbeat.Model)
	assert.True(t, config.Heartbeat.Enabled)
	assert.Equal(t, "1hr", config.Heartbeat.Duration)
	assert.Equal(t, "Check status", config.Heartbeat.Content)
}

// TestPatchAgentConfiguration_WithCronJobs tests patch with cron jobs.
func TestPatchAgentConfiguration_WithCronJobs(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test data
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, $2, 'lyla', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Make patch request with cron jobs
	patchBody := `{
		"soul": {"personality": "friendly", "identity": "test"},
		"cronJobs": [
			{"time": "09:00", "frequency": "daily", "prompt": "Good morning!"},
			{"time": "18:00", "frequency": "weekly", "prompt": "Weekly report"}
		]
	}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agents/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var config user.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Len(t, config.CronJobs, 2)
}

// TestCreateAgent_Success tests creating a new agent.
func TestCreateAgent_Success(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test user
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Make create request
	createBody := `{
		"name": "new-agent",
		"icon": "star",
		"agentType": "lyla",
		"soul": {"personality": "friendly", "identity": "new"},
		"userContext": "test context"
	}`
	req := httptest.NewRequest(http.MethodPost, "/user/agents", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.CreateAgent(w, req)

	// Verify response
	assert.Equal(t, http.StatusCreated, w.Code)

	var response user.CreateAgentResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "new-agent", response.Name)
}

// TestCreateAgent_DuplicateNameSuffix tests that duplicate names get suffixed.
func TestCreateAgent_DuplicateNameSuffix(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test data with existing agent
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, config)
		VALUES ($1::uuid, 'duplicate', 'lyla', '{}'::jsonb)
	`, testUserID)
	require.NoError(t, err)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Try to create agent with same name
	createBody := `{"name": "duplicate"}`
	req := httptest.NewRequest(http.MethodPost, "/user/agents", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.CreateAgent(w, req)

	// Verify response - should get suffixed name
	assert.Equal(t, http.StatusCreated, w.Code)

	var response user.CreateAgentResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "duplicate-2", response.Name)
}

// TestPatchAgentConfiguration_WithIcon tests updating icon field.
func TestPatchAgentConfiguration_WithIcon(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	runMigrations(t)
	defer cleanupTestData(t, pool)

	// Setup test data
	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (user_id, agent_name, agent_type, icon, config)
		VALUES ($1::uuid, $2, 'lyla', 'old-icon', '{"soul": {"personality": "friendly", "identity": "test"}}'::jsonb)
	`, testUserID, testAgentName)
	require.NoError(t, err)

	// Create handler
	store := user.NewPGStore(pool)
	service := user.NewService(store)
	handler := user.NewHandler(service, zerolog.Nop())

	// Make patch request to update icon
	patchBody := `{"icon": "new-icon"}`
	req := httptest.NewRequest(http.MethodPatch, "/user/agents/"+testAgentName, strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := internalctx.WithUserID(context.Background(), testUserID)
	req = req.WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.PatchAgentConfiguration(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var config user.AgentConfiguration
	err = json.NewDecoder(w.Body).Decode(&config)
	require.NoError(t, err)
	assert.Equal(t, "new-icon", config.Icon)
}
