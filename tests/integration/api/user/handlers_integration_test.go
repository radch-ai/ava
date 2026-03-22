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

	"github.com/nevaria-ai/backend/internal/api/user"
	internalctx "github.com/nevaria-ai/backend/internal/ctx"
)

const (
	testUserID = "00000000-0000-0000-0000-000000000001"
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
	pool.Exec(ctx, "DELETE FROM agent_tools WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM cron_jobs WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM heartbeat WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM agents WHERE user_id = $1::uuid", testUserID)
	pool.Exec(ctx, "DELETE FROM users WHERE user_id = $1::uuid", testUserID)
}

func setupTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `INSERT INTO users (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, testUserID)
	require.NoError(t, err, "Failed to insert test user")

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

	var response user.AgentsResponse
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
	noAgentUserID := uuid.New().String()
	reqCtx := internalctx.WithUserID(context.Background(), noAgentUserID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	handler.GetUserAgents(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response user.AgentsResponse
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
