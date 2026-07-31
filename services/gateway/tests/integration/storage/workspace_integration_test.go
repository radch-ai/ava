//go:build integration

package storage_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radch-ai/ava/services/gateway/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getWorkspaceTestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

// createAgentsTable creates the agents table for testing.
// Backend owns migrations; gateway tests create table directly.
func createAgentsTableForWorkspaceTest(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS agents (
			agent_id     TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL,
			name         TEXT,
			soul_md      TEXT,
			agents_md    TEXT,
			user_md      TEXT,
			identity_md  TEXT,
			tools_md     TEXT,
			heartbeat_md TEXT,
			config       JSONB,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_agents_user_id ON agents(user_id);
		CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at);
	`)
	return err
}

func setupWorkspaceTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbURL := getWorkspaceTestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err, "Failed to parse database URL")

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to test database: %v", err)
		return nil
	}

	err = createAgentsTableForWorkspaceTest(ctx, pool)
	if err != nil {
		pool.Close()
		t.Skipf("Skipping test: cannot create agents table: %v", err)
		return nil
	}

	t.Cleanup(func() {
		cleanupWorkspaceTestData(t, pool)
		pool.Close()
	})

	return pool
}

func cleanupWorkspaceTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	tables := []string{"agents", "usage_logs", "processed_webhooks", "users"}

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Logf("Warning: failed to clean table %s: %v", table, err)
		}
	}
}

func createWorkspaceTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) {
	t.Helper()

	now := time.Now().Unix()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ($1, 1, 100, $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, userID, now, now)
	require.NoError(t, err, "Failed to create test user")
}

func TestWorkspaceLoader_Load_AllFieldsPopulated(t *testing.T) {
	pool := setupWorkspaceTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	userID := "test-user-all-fields"
	agentID := "agent-all-fields"
	createWorkspaceTestUser(t, ctx, pool, userID)

	config := &storage.AgentConfig{
		AgentID:    agentID,
		UserID:     userID,
		Name:       "All Fields Agent",
		SoulMd:     "# Soul\nYou are a helpful AI assistant.",
		AgentsMd:   "# Agents\nWork together with other agents.",
		UserMd:     "The user is a software engineer.",
		IdentityMd: "Name: WorkspaceBot\nVersion: 1.0",
		ToolsMd:    "## Tool Guidelines\n- Be precise\n- Verify results",
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err, "Failed to create agent")

	workspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err, "Load should succeed")

	assert.Equal(t, config.SoulMd, workspace.Soul, "Soul should match")
	assert.Equal(t, config.AgentsMd, workspace.Agents, "Agents should match")
	assert.Equal(t, config.UserMd, workspace.User, "User should match")
	assert.Equal(t, config.IdentityMd, workspace.Identity, "Identity should match")
	assert.Equal(t, config.ToolsMd, workspace.Tools, "Tools should match")
	assert.True(t, workspace.Loaded, "Loaded should be true")
}

func TestWorkspaceLoader_Load_PartialFieldsPopulated(t *testing.T) {
	tests := []struct {
		name          string
		config        *storage.AgentConfig
		expectedSoul  string
		expectedAgnts string
		expectedUser  string
		expectedId    string
		expectedTools string
	}{
		{
			name: "Only Soul and User populated",
			config: &storage.AgentConfig{
				SoulMd: "# Soul\nYou are helpful",
				UserMd: "User is admin",
			},
			expectedSoul:  "# Soul\nYou are helpful",
			expectedAgnts: "",
			expectedUser:  "User is admin",
			expectedId:    "",
			expectedTools: "",
		},
		{
			name: "Only Identity and Tools populated",
			config: &storage.AgentConfig{
				IdentityMd: "Name: Bot",
				ToolsMd:    "Use carefully",
			},
			expectedSoul:  "",
			expectedAgnts: "",
			expectedUser:  "",
			expectedId:    "Name: Bot",
			expectedTools: "Use carefully",
		},
		{
			name: "Only Agents populated",
			config: &storage.AgentConfig{
				AgentsMd: "# Instructions\nBe concise",
			},
			expectedSoul:  "",
			expectedAgnts: "# Instructions\nBe concise",
			expectedUser:  "",
			expectedId:    "",
			expectedTools: "",
		},
		{
			name:          "No fields populated",
			config:        &storage.AgentConfig{},
			expectedSoul:  "",
			expectedAgnts: "",
			expectedUser:  "",
			expectedId:    "",
			expectedTools: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := setupWorkspaceTestDB(t)
			if pool == nil {
				return
			}

			ctx := context.Background()
			store := storage.NewPostgresAgentStore(pool)
			loader := storage.NewWorkspaceLoaderFromAgentStore(store)

			userID := fmt.Sprintf("test-user-partial-%d", time.Now().UnixNano())
			agentID := fmt.Sprintf("agent-partial-%d", time.Now().UnixNano())
			createWorkspaceTestUser(t, ctx, pool, userID)

			tt.config.AgentID = agentID
			tt.config.UserID = userID
			tt.config.Name = "Partial Fields Agent"

			err := store.Upsert(ctx, tt.config)
			require.NoError(t, err, "Failed to create agent")

			workspace, err := loader.Load(ctx, userID, agentID)
			require.NoError(t, err, "Load should succeed")

			assert.Equal(t, tt.expectedSoul, workspace.Soul, "Soul should match")
			assert.Equal(t, tt.expectedAgnts, workspace.Agents, "Agents should match")
			assert.Equal(t, tt.expectedUser, workspace.User, "User should match")
			assert.Equal(t, tt.expectedId, workspace.Identity, "Identity should match")
			assert.Equal(t, tt.expectedTools, workspace.Tools, "Tools should match")
			assert.True(t, workspace.Loaded, "Loaded should be true when agent exists")
		})
	}
}

func TestWorkspaceLoader_Load_MissingAgent(t *testing.T) {
	pool := setupWorkspaceTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	userID := "test-user-missing"
	createWorkspaceTestUser(t, ctx, pool, userID)

	workspace, err := loader.Load(ctx, userID, "non-existent-agent")
	require.NoError(t, err, "Load should not error for missing agent")

	assert.Equal(t, "", workspace.Soul, "Soul should be empty")
	assert.Equal(t, "", workspace.Agents, "Agents should be empty")
	assert.Equal(t, "", workspace.User, "User should be empty")
	assert.Equal(t, "", workspace.Identity, "Identity should be empty")
	assert.Equal(t, "", workspace.Tools, "Tools should be empty")
	assert.False(t, workspace.Loaded, "Loaded should be false for missing agent")
}

func TestWorkspaceLoader_Load_ConcurrentSameAgent(t *testing.T) {
	pool := setupWorkspaceTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	userID := "test-user-concurrent-same"
	agentID := "agent-concurrent-same"
	createWorkspaceTestUser(t, ctx, pool, userID)

	config := &storage.AgentConfig{
		AgentID:    agentID,
		UserID:     userID,
		Name:       "Concurrent Agent",
		SoulMd:     "# Soul\nConcurrent test",
		AgentsMd:   "# Agents\nTest",
		UserMd:     "User data",
		IdentityMd: "Identity data",
		ToolsMd:    "Tools data",
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err)

	numGoroutines := 10
	var wg sync.WaitGroup
	workspaces := make([]*storage.Workspace, numGoroutines)
	errors := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			workspace, err := loader.Load(ctx, userID, agentID)
			workspaces[idx] = workspace
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	for i := 0; i < numGoroutines; i++ {
		require.NoError(t, errors[i], "Concurrent load %d should succeed", i)
		require.NotNil(t, workspaces[i], "Concurrent load %d should return workspace", i)
		assert.Equal(t, config.SoulMd, workspaces[i].Soul, "Concurrent load %d Soul should match", i)
		assert.Equal(t, config.AgentsMd, workspaces[i].Agents, "Concurrent load %d Agents should match", i)
		assert.True(t, workspaces[i].Loaded, "Concurrent load %d Loaded should be true", i)
	}
}

func TestWorkspaceLoader_Load_ConcurrentDifferentAgents(t *testing.T) {
	pool := setupWorkspaceTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	userID := "test-user-concurrent-diff"
	createWorkspaceTestUser(t, ctx, pool, userID)

	numAgents := 5
	agentIDs := make([]string, numAgents)
	for i := 0; i < numAgents; i++ {
		agentID := fmt.Sprintf("agent-concurrent-diff-%d", i)
		agentIDs[i] = agentID

		config := &storage.AgentConfig{
			AgentID:    agentID,
			UserID:     userID,
			Name:       fmt.Sprintf("Agent %d", i),
			SoulMd:     fmt.Sprintf("# Soul\nAgent %d soul", i),
			AgentsMd:   fmt.Sprintf("# Agents\nAgent %d agents", i),
			UserMd:     fmt.Sprintf("User %d data", i),
			IdentityMd: fmt.Sprintf("Identity %d data", i),
			ToolsMd:    fmt.Sprintf("Tools %d data", i),
		}

		err := store.Upsert(ctx, config)
		require.NoError(t, err, "Failed to create agent %d", i)
	}

	var wg sync.WaitGroup
	workspaces := make(map[string]*storage.Workspace)
	errors := make(map[string]error)
	var mu sync.Mutex

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			workspace, err := loader.Load(ctx, userID, agentIDs[idx])

			mu.Lock()
			defer mu.Unlock()
			workspaces[agentIDs[idx]] = workspace
			errors[agentIDs[idx]] = err
		}(i)
	}

	wg.Wait()

	for i := 0; i < numAgents; i++ {
		agentID := agentIDs[i]
		require.NoError(t, errors[agentID], "Load for agent %s should succeed", agentID)
		require.NotNil(t, workspaces[agentID], "Load for agent %s should return workspace", agentID)

		expectedSoul := fmt.Sprintf("# Soul\nAgent %d soul", i)
		expectedAgents := fmt.Sprintf("# Agents\nAgent %d agents", i)
		expectedUser := fmt.Sprintf("User %d data", i)
		expectedIdentity := fmt.Sprintf("Identity %d data", i)
		expectedTools := fmt.Sprintf("Tools %d data", i)

		assert.Equal(t, expectedSoul, workspaces[agentID].Soul, "Soul for agent %s should match", agentID)
		assert.Equal(t, expectedAgents, workspaces[agentID].Agents, "Agents for agent %s should match", agentID)
		assert.Equal(t, expectedUser, workspaces[agentID].User, "User for agent %s should match", agentID)
		assert.Equal(t, expectedIdentity, workspaces[agentID].Identity, "Identity for agent %s should match", agentID)
		assert.Equal(t, expectedTools, workspaces[agentID].Tools, "Tools for agent %s should match", agentID)
		assert.True(t, workspaces[agentID].Loaded, "Loaded for agent %s should be true", agentID)
	}
}

func TestWorkspace_FormatSystemPrompt_AllSections(t *testing.T) {
	pool := setupWorkspaceTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	userID := "test-user-prompt-all"
	agentID := "agent-prompt-all"
	createWorkspaceTestUser(t, ctx, pool, userID)

	config := &storage.AgentConfig{
		AgentID:    agentID,
		UserID:     userID,
		Name:       "Prompt Test Agent",
		SoulMd:     "# Soul\nYou are a helpful assistant.",
		AgentsMd:   "# Agents\nCollaborate with others.",
		UserMd:     "The user is a Python developer.",
		IdentityMd: "Name: PromptBot\nRole: Assistant",
		ToolsMd:    "## Tool Guidelines\n- Use Python tools\n- Verify syntax",
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err)

	workspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err)

	prompt := workspace.FormatSystemPrompt()

	assert.Contains(t, prompt, config.SoulMd, "Prompt should contain Soul content")
	assert.Contains(t, prompt, config.AgentsMd, "Prompt should contain Agents content")
	assert.Contains(t, prompt, "## User Information", "Prompt should contain User Information section header")
	assert.Contains(t, prompt, config.UserMd, "Prompt should contain User content")
	assert.Contains(t, prompt, "## Identity", "Prompt should contain Identity section header")
	assert.Contains(t, prompt, config.IdentityMd, "Prompt should contain Identity content")
	assert.Contains(t, prompt, "## Tool Guidance", "Prompt should contain Tool Guidance section header")
	assert.Contains(t, prompt, config.ToolsMd, "Prompt should contain Tools content")
}

func TestWorkspace_FormatSystemPrompt_PartialSections(t *testing.T) {
	tests := []struct {
		name                  string
		config                *storage.AgentConfig
		shouldContainSoul     bool
		shouldContainAgents   bool
		shouldContainUser     bool
		shouldContainIdentity bool
		shouldContainTools    bool
	}{
		{
			name: "Soul and Identity only",
			config: &storage.AgentConfig{
				SoulMd:     "# Soul\nHelpful",
				IdentityMd: "Name: Bot",
			},
			shouldContainSoul:     true,
			shouldContainAgents:   false,
			shouldContainUser:     false,
			shouldContainIdentity: true,
			shouldContainTools:    false,
		},
		{
			name: "Agents and Tools only",
			config: &storage.AgentConfig{
				AgentsMd: "# Instructions\nBe concise",
				ToolsMd:  "Use wisely",
			},
			shouldContainSoul:     false,
			shouldContainAgents:   true,
			shouldContainUser:     false,
			shouldContainIdentity: false,
			shouldContainTools:    true,
		},
		{
			name: "User only",
			config: &storage.AgentConfig{
				UserMd: "User is admin",
			},
			shouldContainSoul:     false,
			shouldContainAgents:   false,
			shouldContainUser:     true,
			shouldContainIdentity: false,
			shouldContainTools:    false,
		},
		{
			name:                  "No sections",
			config:                &storage.AgentConfig{},
			shouldContainSoul:     false,
			shouldContainAgents:   false,
			shouldContainUser:     false,
			shouldContainIdentity: false,
			shouldContainTools:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := setupWorkspaceTestDB(t)
			if pool == nil {
				return
			}

			ctx := context.Background()
			store := storage.NewPostgresAgentStore(pool)
			loader := storage.NewWorkspaceLoaderFromAgentStore(store)

			userID := fmt.Sprintf("test-user-prompt-%d", time.Now().UnixNano())
			agentID := fmt.Sprintf("agent-prompt-%d", time.Now().UnixNano())
			createWorkspaceTestUser(t, ctx, pool, userID)

			tt.config.AgentID = agentID
			tt.config.UserID = userID
			tt.config.Name = "Prompt Test Agent"

			err := store.Upsert(ctx, tt.config)
			require.NoError(t, err)

			workspace, err := loader.Load(ctx, userID, agentID)
			require.NoError(t, err)

			prompt := workspace.FormatSystemPrompt()

			if tt.shouldContainSoul {
				assert.Contains(t, prompt, tt.config.SoulMd, "Prompt should contain Soul")
			} else if tt.config.SoulMd != "" {
				assert.NotContains(t, prompt, tt.config.SoulMd, "Prompt should not contain Soul")
			}

			if tt.shouldContainAgents {
				assert.Contains(t, prompt, tt.config.AgentsMd, "Prompt should contain Agents")
			} else if tt.config.AgentsMd != "" {
				assert.NotContains(t, prompt, tt.config.AgentsMd, "Prompt should not contain Agents")
			}

			if tt.shouldContainUser {
				assert.Contains(t, prompt, "## User Information", "Prompt should contain User header")
				assert.Contains(t, prompt, tt.config.UserMd, "Prompt should contain User content")
			} else {
				assert.NotContains(t, prompt, "## User Information", "Prompt should not contain User header")
			}

			if tt.shouldContainIdentity {
				assert.Contains(t, prompt, "## Identity", "Prompt should contain Identity header")
				assert.Contains(t, prompt, tt.config.IdentityMd, "Prompt should contain Identity content")
			} else {
				assert.NotContains(t, prompt, "## Identity", "Prompt should not contain Identity header")
			}

			if tt.shouldContainTools {
				assert.Contains(t, prompt, "## Tool Guidance", "Prompt should contain Tools header")
				assert.Contains(t, prompt, tt.config.ToolsMd, "Prompt should contain Tools content")
			} else {
				assert.NotContains(t, prompt, "## Tool Guidance", "Prompt should not contain Tools header")
			}
		})
	}
}

func TestWorkspaceLoader_IntegrationWithPostgresAgentStore(t *testing.T) {
	pool := setupWorkspaceTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	userID := "test-user-integration"
	createWorkspaceTestUser(t, ctx, pool, userID)

	agentID := "agent-integration"

	config := &storage.AgentConfig{
		AgentID:    agentID,
		UserID:     userID,
		Name:       "Integration Agent",
		SoulMd:     "# Soul\nOriginal",
		AgentsMd:   "# Agents\nOriginal",
		UserMd:     "Original user",
		IdentityMd: "Original identity",
		ToolsMd:    "Original tools",
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err)

	workspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err)

	assert.Equal(t, "# Soul\nOriginal", workspace.Soul, "Initial Soul should match")
	assert.Equal(t, "# Agents\nOriginal", workspace.Agents, "Initial Agents should match")
	assert.True(t, workspace.Loaded, "Initial Loaded should be true")

	updatedConfig := &storage.AgentConfig{
		AgentID:    agentID,
		UserID:     userID,
		Name:       "Integration Agent Updated",
		SoulMd:     "# Soul\nUpdated",
		AgentsMd:   "# Agents\nUpdated",
		UserMd:     "Updated user",
		IdentityMd: "Updated identity",
		ToolsMd:    "Updated tools",
	}

	err = store.Upsert(ctx, updatedConfig)
	require.NoError(t, err)

	updatedWorkspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err)

	assert.Equal(t, "# Soul\nUpdated", updatedWorkspace.Soul, "Updated Soul should match")
	assert.Equal(t, "# Agents\nUpdated", updatedWorkspace.Agents, "Updated Agents should match")
	assert.Equal(t, "Updated user", updatedWorkspace.User, "Updated User should match")
	assert.Equal(t, "Updated identity", updatedWorkspace.Identity, "Updated Identity should match")
	assert.Equal(t, "Updated tools", updatedWorkspace.Tools, "Updated Tools should match")
	assert.True(t, updatedWorkspace.Loaded, "Updated Loaded should be true")

	err = store.Delete(ctx, userID, agentID)
	require.NoError(t, err)

	deletedWorkspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err)

	assert.False(t, deletedWorkspace.Loaded, "Deleted agent should have Loaded=false")
	assert.Equal(t, "", deletedWorkspace.Soul, "Deleted agent should have empty Soul")
	assert.Equal(t, "", deletedWorkspace.Agents, "Deleted agent should have empty Agents")
}
