//go:build integration

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nevaria-ai/gateway/internal/billing"
	"github.com/nevaria-ai/gateway/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getE2ETestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

func setupE2ETestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	dbURL := getE2ETestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err, "Failed to parse database URL")

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to test database: %v", err)
		return nil
	}

	err = billing.RunMigrationsWithPool(ctx, pool)
	if err != nil {
		pool.Close()
		t.Skipf("Skipping test: cannot run migrations: %v", err)
		return nil
	}

	t.Cleanup(func() {
		cleanupE2ETestData(t, pool)
		pool.Close()
	})

	return pool
}

func cleanupE2ETestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	tables := []string{"usage_logs", "processed_webhooks", "agents", "users"}

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Logf("Warning: failed to clean table %s: %v", table, err)
		}
	}
}

func createTestUserForE2E(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) {
	t.Helper()

	now := time.Now().Unix()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, tier, credits_remaining, created_at, updated_at)
		VALUES ($1, $2, 1, 1000.00, $3, $4)
		ON CONFLICT (id) DO NOTHING
	`, userID, userID+"@example.com", now, now)
	require.NoError(t, err, "Failed to create test user")
}

func TestE2E_FullAgentConfigWorkflow(t *testing.T) {
	t.Parallel()

	pool := setupE2ETestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	userID := "e2e-full-flow-user"
	agentID := "agent-full-flow-001"

	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	createTestUserForE2E(t, ctx, pool, userID)

	originalConfig := &storage.AgentConfig{
		AgentID: agentID,
		UserID:  userID,
		Name:    "Full Workflow Test Agent",
		SoulMd: `# Agent Soul

You are a helpful AI assistant designed to assist users with complex tasks.
Your primary purpose is to provide accurate, timely, and relevant information.

## Core Principles
- Be concise and clear
- Always verify information when uncertain
- Maintain a friendly and professional tone
- Respect user privacy and confidentiality`,
		AgentsMd: `# Agent Guidelines

## Communication Style
- Use clear, straightforward language
- Avoid jargon unless necessary
- Explain complex concepts in simple terms

## Task Execution
1. Understand the user's request
2. Gather necessary information
3. Provide a well-structured response
4. Offer to help with follow-up questions`,
		UserMd: `# User Profile

## Basic Information
- Name: Alex Johnson
- Role: Software Developer
- Experience: 5 years in full-stack development

## Preferences
- Prefers code examples
- Likes detailed explanations
- Values accuracy over speed

## Technical Background
- Primary languages: Go, Python, JavaScript
- Familiar with cloud platforms (AWS, GCP)
- Experience with microservices architecture`,
		IdentityMd: `# Agent Identity

Name: Claude
Version: 4.0
Created by: Anthropic

## Purpose
To be a helpful, harmless, and honest AI assistant that can engage in a wide range of conversations and assist with various tasks.

## Capabilities
- Natural language understanding and generation
- Code generation and debugging
- Creative writing and brainstorming
- Analysis and problem-solving`,
		ToolsMd: `# Tool Usage Guidelines

## Available Tools
- Code execution environment
- File system access (read/write)
- Web browsing capabilities
- Calculator and math functions

## Tool Selection Principles
1. Choose the most appropriate tool for the task
2. Use tools to verify information when possible
3. Always explain tool usage to the user
4. Handle tool errors gracefully

## Best Practices
- Check tool availability before use
- Validate tool results
- Provide context about tool outputs
- Fall back to reasoning when tools are unavailable`,
		HeartbeatMd: `# System Health Check

## Monitoring Metrics
- Response time: < 2 seconds
- Error rate: < 1%
- Uptime: 99.9%

## Self-Check Procedures
1. Verify database connectivity
2. Check API endpoint availability
3. Validate configuration
4. Test core functionality

## Alert Conditions
- Response time > 5 seconds
- Error rate > 5%
- Unusual traffic patterns`,
		Config: map[string]any{
			"temperature":       0.7,
			"max_tokens":        4096,
			"top_p":             0.9,
			"frequency_penalty": 0.0,
			"presence_penalty":  0.0,
			"model":             "claude-4-sonnet",
			"stream":            true,
		},
	}

	err := store.Upsert(ctx, originalConfig)
	require.NoError(t, err, "Failed to create initial agent config")

	retrievedConfig, err := store.Get(ctx, userID, agentID)
	require.NoError(t, err, "Failed to retrieve agent config")

	assert.Equal(t, originalConfig.AgentID, retrievedConfig.AgentID)
	assert.Equal(t, originalConfig.UserID, retrievedConfig.UserID)
	assert.Equal(t, originalConfig.Name, retrievedConfig.Name)
	assert.Equal(t, originalConfig.SoulMd, retrievedConfig.SoulMd)
	assert.Equal(t, originalConfig.AgentsMd, retrievedConfig.AgentsMd)
	assert.Equal(t, originalConfig.UserMd, retrievedConfig.UserMd)
	assert.Equal(t, originalConfig.IdentityMd, retrievedConfig.IdentityMd)
	assert.Equal(t, originalConfig.ToolsMd, retrievedConfig.ToolsMd)
	assert.Equal(t, originalConfig.HeartbeatMd, retrievedConfig.HeartbeatMd)
	assert.Equal(t, 0.7, retrievedConfig.Config["temperature"])
	assert.Equal(t, float64(4096), retrievedConfig.Config["max_tokens"])

	workspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err, "Failed to load workspace")
	require.True(t, workspace.Loaded, "Workspace should be marked as loaded")

	assert.Equal(t, originalConfig.SoulMd, workspace.Soul)
	assert.Equal(t, originalConfig.AgentsMd, workspace.Agents)
	assert.Equal(t, originalConfig.UserMd, workspace.User)
	assert.Equal(t, originalConfig.IdentityMd, workspace.Identity)
	assert.Equal(t, originalConfig.ToolsMd, workspace.Tools)

	formattedPrompt := workspace.FormatSystemPrompt()
	assert.Contains(t, formattedPrompt, "Agent Soul")
	assert.Contains(t, formattedPrompt, "Agent Guidelines")
	assert.Contains(t, formattedPrompt, "User Profile")
	assert.Contains(t, formattedPrompt, "Agent Identity")
	assert.Contains(t, formattedPrompt, "Tool Usage Guidelines")
	assert.Contains(t, formattedPrompt, "Alex Johnson")
	assert.Contains(t, formattedPrompt, "Software Developer")

	updatedConfig := &storage.AgentConfig{
		AgentID:     agentID,
		UserID:      userID,
		Name:        "Updated Agent Name",
		SoulMd:      originalConfig.SoulMd + "\n\n## Updated Section\nThis content was added during the update.",
		AgentsMd:    originalConfig.AgentsMd,
		UserMd:      originalConfig.UserMd,
		IdentityMd:  originalConfig.IdentityMd,
		ToolsMd:     originalConfig.ToolsMd,
		HeartbeatMd: originalConfig.HeartbeatMd,
		Config: map[string]any{
			"temperature":       0.8,
			"max_tokens":        8192,
			"top_p":             0.95,
			"frequency_penalty": 0.1,
			"presence_penalty":  0.1,
			"model":             "claude-4-opus",
			"stream":            false,
		},
	}

	err = store.Upsert(ctx, updatedConfig)
	require.NoError(t, err, "Failed to update agent config")

	finalConfig, err := store.Get(ctx, userID, agentID)
	require.NoError(t, err, "Failed to retrieve updated config")

	assert.Equal(t, "Updated Agent Name", finalConfig.Name)
	assert.Contains(t, finalConfig.SoulMd, "Updated Section")
	assert.Equal(t, 0.8, finalConfig.Config["temperature"])
	assert.Equal(t, float64(8192), finalConfig.Config["max_tokens"])

	finalWorkspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err)
	assert.True(t, finalWorkspace.Loaded)
	assert.Contains(t, finalWorkspace.Soul, "Updated Section")

	err = store.Delete(ctx, userID, agentID)
	require.NoError(t, err, "Failed to delete agent")

	_, err = store.Get(ctx, userID, agentID)
	assert.Error(t, err, "Should return error after deletion")
	assert.Contains(t, err.Error(), "agent not found")

	deletedWorkspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err, "Load should not error for deleted agent")
	assert.False(t, deletedWorkspace.Loaded, "Workspace should not be loaded after deletion")
}

func TestE2E_MultipleUserIsolation(t *testing.T) {
	t.Parallel()

	pool := setupE2ETestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()

	users := []string{
		"user-isolation-alice",
		"user-isolation-bob",
		"user-isolation-charlie",
	}

	for _, userID := range users {
		createTestUserForE2E(t, ctx, pool, userID)
	}

	store := storage.NewPostgresAgentStore(pool)

	aliceConfig := &storage.AgentConfig{
		AgentID: "agent-isolation-001",
		UserID:  users[0],
		Name:    "Alice's Personal Assistant",
		SoulMd:  "I am Alice's personal assistant. I only help Alice.",
		Config:  map[string]any{"owner": "alice"},
	}

	bobConfig := &storage.AgentConfig{
		AgentID: "agent-isolation-002",
		UserID:  users[1],
		Name:    "Bob's Work Assistant",
		SoulMd:  "I am Bob's work assistant. I focus on productivity.",
		Config:  map[string]any{"owner": "bob"},
	}

	charlieConfig := &storage.AgentConfig{
		AgentID: "agent-isolation-003",
		UserID:  users[2],
		Name:    "Charlie's Research Assistant",
		SoulMd:  "I am Charlie's research assistant. I help with academic work.",
		Config:  map[string]any{"owner": "charlie"},
	}

	err := store.Upsert(ctx, aliceConfig)
	require.NoError(t, err)
	err = store.Upsert(ctx, bobConfig)
	require.NoError(t, err)
	err = store.Upsert(ctx, charlieConfig)
	require.NoError(t, err)

	aliceAgent, err := store.Get(ctx, users[0], "agent-isolation-001")
	require.NoError(t, err)
	assert.Equal(t, "Alice's Personal Assistant", aliceAgent.Name)
	assert.Equal(t, users[0], aliceAgent.UserID)

	_, err = store.Get(ctx, users[1], "agent-isolation-001")
	assert.Error(t, err, "Bob should not be able to access Alice's agent")

	_, err = store.Get(ctx, users[2], "agent-isolation-001")
	assert.Error(t, err, "Charlie should not be able to access Alice's agent")

	bobAgent, err := store.Get(ctx, users[1], "agent-isolation-002")
	require.NoError(t, err)
	assert.Equal(t, "Bob's Work Assistant", bobAgent.Name)

	_, err = store.Get(ctx, users[0], "agent-isolation-002")
	assert.Error(t, err, "Alice should not be able to access Bob's agent")

	aliceThirdAgent := &storage.AgentConfig{
		AgentID: "agent-isolation-alice-003",
		UserID:  users[0],
		Name:    "Alice's Third Agent",
		SoulMd:  "This is Alice's third agent.",
	}

	err = store.Upsert(ctx, aliceThirdAgent)
	require.NoError(t, err)

	bobThirdAgent := &storage.AgentConfig{
		AgentID: "agent-isolation-bob-003",
		UserID:  users[1],
		Name:    "Bob's Third Agent",
		SoulMd:  "This is Bob's third agent.",
	}

	err = store.Upsert(ctx, bobThirdAgent)
	require.NoError(t, err)

	aliceThirdVersion, err := store.Get(ctx, users[0], "agent-isolation-alice-003")
	require.NoError(t, err)
	assert.Equal(t, "Alice's Third Agent", aliceThirdVersion.Name)
	assert.Equal(t, "This is Alice's third agent.", aliceThirdVersion.SoulMd)

	bobThirdVersion, err := store.Get(ctx, users[1], "agent-isolation-bob-003")
	require.NoError(t, err)
	assert.Equal(t, "Bob's Third Agent", bobThirdVersion.Name)
	assert.Equal(t, "This is Bob's third agent.", bobThirdVersion.SoulMd)

	err = store.Delete(ctx, users[0], "agent-isolation-alice-003")
	require.NoError(t, err)

	_, err = store.Get(ctx, users[0], "agent-isolation-alice-003")
	assert.Error(t, err, "Alice's third agent should be deleted")

	bobThirdAfterDelete, err := store.Get(ctx, users[1], "agent-isolation-bob-003")
	require.NoError(t, err, "Bob's third agent should still exist")
	assert.Equal(t, "Bob's Third Agent", bobThirdAfterDelete.Name)

	for i, userID := range users {
		agents, err := store.ListByUser(ctx, userID)
		require.NoError(t, err)
		expectedCount := 2
		if i == 0 {
			expectedCount = 1
		} else if i == 2 {
			expectedCount = 1
		}
		assert.Len(t, agents, expectedCount, "User %s should have %d agents", userID, expectedCount)

		for _, agent := range agents {
			assert.Equal(t, userID, agent.UserID, "Agent should belong to correct user")
		}
	}
}

func TestE2E_WorkspaceFormattingAndStructure(t *testing.T) {
	t.Parallel()

	pool := setupE2ETestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	userID := "e2e-workspace-user"
	agentID := "agent-workspace-001"

	store := storage.NewPostgresAgentStore(pool)
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	createTestUserForE2E(t, ctx, pool, userID)

	config := &storage.AgentConfig{
		AgentID:    agentID,
		UserID:     userID,
		Name:       "Workspace Formatting Test",
		SoulMd:     "# Soul\n\nCore identity and purpose.",
		AgentsMd:   "# Agent Guidelines\n\nInstructions for behavior.",
		UserMd:     "User information and preferences.",
		IdentityMd: "# Identity\n\nName: TestBot\nRole: Assistant",
		ToolsMd:    "# Tools\n\nAvailable tools and usage guidelines.",
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err)

	workspace, err := loader.Load(ctx, userID, agentID)
	require.NoError(t, err)
	assert.True(t, workspace.Loaded)

	prompt := workspace.FormatSystemPrompt()

	parts := strings.Split(prompt, "\n\n")
	assert.GreaterOrEqual(t, len(parts), 1, "Prompt should have multiple sections")

	if workspace.Soul != "" {
		assert.Contains(t, prompt, workspace.Soul, "Prompt should contain Soul content")
	}

	if workspace.Agents != "" {
		assert.Contains(t, prompt, workspace.Agents, "Prompt should contain Agents content")
	}

	if workspace.User != "" {
		assert.Contains(t, prompt, "User Information", "Prompt should have User section header")
		assert.Contains(t, prompt, workspace.User, "Prompt should contain User content")
	}

	if workspace.Identity != "" {
		assert.Contains(t, prompt, "Identity", "Prompt should have Identity section header")
		assert.Contains(t, prompt, workspace.Identity, "Prompt should contain Identity content")
	}

	if workspace.Tools != "" {
		assert.Contains(t, prompt, "Tool Guidance", "Prompt should have Tools section header")
		assert.Contains(t, prompt, workspace.Tools, "Prompt should contain Tools content")
	}

	workspaceWithEmpty := &storage.Workspace{
		Soul:     "",
		Agents:   "Some content",
		User:     "",
		Identity: "Identity info",
		Tools:    "",
		Loaded:   true,
	}

	emptyPrompt := workspaceWithEmpty.FormatSystemPrompt()
	assert.NotContains(t, emptyPrompt, "User Information", "Should not have User section for empty User")
	assert.NotContains(t, emptyPrompt, "Tool Guidance", "Should not have Tools section for empty Tools")
	assert.Contains(t, emptyPrompt, "Some content", "Should have Agents content")
	assert.Contains(t, emptyPrompt, "Identity info", "Should have Identity content")

	workspaceEmpty := &storage.Workspace{
		Soul:     "",
		Agents:   "",
		User:     "",
		Identity: "",
		Tools:    "",
		Loaded:   true,
	}

	emptyWorkspacePrompt := workspaceEmpty.FormatSystemPrompt()
	assert.Empty(t, strings.TrimSpace(emptyWorkspacePrompt), "Empty workspace should produce empty prompt")

	complexConfig := &storage.AgentConfig{
		AgentID: "agent-workspace-complex",
		UserID:  userID,
		Name:    "Complex Workspace",
		SoulMd: `# Multi-section Soul

## Section 1
Content here.

### Subsection
More content.

## Section 2
Even more content with **markdown** and *formatting*.`,
		AgentsMd:   "Simple agents text.",
		UserMd:     "User profile data.",
		IdentityMd: "Identity information.",
		ToolsMd:    "Tool documentation.",
	}

	err = store.Upsert(ctx, complexConfig)
	require.NoError(t, err)

	complexWorkspace, err := loader.Load(ctx, userID, "agent-workspace-complex")
	require.NoError(t, err)

	complexPrompt := complexWorkspace.FormatSystemPrompt()
	assert.Contains(t, complexPrompt, "Section 1")
	assert.Contains(t, complexPrompt, "Subsection")
	assert.Contains(t, complexPrompt, "**markdown**")
	assert.Contains(t, complexPrompt, "*formatting*")
}

func TestE2E_ConfigPersistenceAndMigrationSimulation(t *testing.T) {
	t.Parallel()

	pool := setupE2ETestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	userID := "e2e-migration-user"

	createTestUserForE2E(t, ctx, pool, userID)

	store := storage.NewPostgresAgentStore(pool)

	s3SimulatedConfig := map[string]any{
		"agent_id": "agent-migration-001",
		"user_id":  userID,
		"name":     "Legacy S3 Agent",
		"soul_md": `# Legacy Soul

This content was originally stored in S3.
The migration process preserves all markdown formatting
and nested data structures.`,
		"agents_md":    "Legacy agent instructions.",
		"user_md":      "Legacy user data.",
		"identity_md":  "# Legacy Identity\n\nName: LegacyBot",
		"tools_md":     "Legacy tool configurations.",
		"heartbeat_md": "# Legacy Heartbeat\n\nCheck status every 5 minutes.",
		"config": map[string]any{
			"nested_settings": map[string]any{
				"api_keys": map[string]string{
					"openai":    "sk-legacy-123",
					"anthropic": "sk-ant-legacy-456",
				},
				"rate_limits": map[string]any{
					"requests_per_minute": 100,
					"tokens_per_minute":   60000,
				},
			},
			"model_preferences": []string{
				"gpt-4",
				"claude-3-opus",
				"claude-3-sonnet",
			},
			"feature_flags": map[string]bool{
				"streaming":        true,
				"function_calling": true,
				"image_generation": false,
			},
		},
	}

	config := &storage.AgentConfig{
		AgentID:     s3SimulatedConfig["agent_id"].(string),
		UserID:      s3SimulatedConfig["user_id"].(string),
		Name:        s3SimulatedConfig["name"].(string),
		SoulMd:      s3SimulatedConfig["soul_md"].(string),
		AgentsMd:    s3SimulatedConfig["agents_md"].(string),
		UserMd:      s3SimulatedConfig["user_md"].(string),
		IdentityMd:  s3SimulatedConfig["identity_md"].(string),
		ToolsMd:     s3SimulatedConfig["tools_md"].(string),
		HeartbeatMd: s3SimulatedConfig["heartbeat_md"].(string),
		Config:      s3SimulatedConfig["config"].(map[string]any),
	}

	err := store.Upsert(ctx, config)
	require.NoError(t, err, "Migration simulation: Failed to insert config")

	retrieved, err := store.Get(ctx, userID, "agent-migration-001")
	require.NoError(t, err, "Migration simulation: Failed to retrieve config")

	assert.Equal(t, "Legacy S3 Agent", retrieved.Name)
	assert.Contains(t, retrieved.SoulMd, "originally stored in S3")
	assert.Contains(t, retrieved.IdentityMd, "LegacyBot")

	nestedSettings, ok := retrieved.Config["nested_settings"].(map[string]any)
	require.True(t, ok, "Nested settings should be preserved")

	apiKeys, ok := nestedSettings["api_keys"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sk-legacy-123", apiKeys["openai"])
	assert.Equal(t, "sk-ant-legacy-456", apiKeys["anthropic"])

	rateLimits, ok := nestedSettings["rate_limits"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(100), rateLimits["requests_per_minute"])
	assert.Equal(t, float64(60000), rateLimits["tokens_per_minute"])

	modelPrefs, ok := retrieved.Config["model_preferences"].([]any)
	require.True(t, ok)
	assert.Len(t, modelPrefs, 3)

	featureFlags, ok := retrieved.Config["feature_flags"].(map[string]any)
	require.True(t, ok)
	assert.True(t, featureFlags["streaming"].(bool))
	assert.True(t, featureFlags["function_calling"].(bool))
	assert.False(t, featureFlags["image_generation"].(bool))

	batchConfigs := []*storage.AgentConfig{
		{
			AgentID: fmt.Sprintf("agent-batch-%03d", 1),
			UserID:  userID,
			Name:    fmt.Sprintf("Batch Agent %d", 1),
			SoulMd:  fmt.Sprintf("Soul for batch agent %d", 1),
		},
		{
			AgentID: fmt.Sprintf("agent-batch-%03d", 2),
			UserID:  userID,
			Name:    fmt.Sprintf("Batch Agent %d", 2),
			SoulMd:  fmt.Sprintf("Soul for batch agent %d", 2),
		},
		{
			AgentID: fmt.Sprintf("agent-batch-%03d", 3),
			UserID:  userID,
			Name:    fmt.Sprintf("Batch Agent %d", 3),
			SoulMd:  fmt.Sprintf("Soul for batch agent %d", 3),
		},
	}

	for _, cfg := range batchConfigs {
		err := store.Upsert(ctx, cfg)
		require.NoError(t, err, "Batch migration: Failed to insert agent %s", cfg.AgentID)
	}

	allAgents, err := store.ListByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, allAgents, 4, "Should have 4 agents after batch migration (1 original + 3 batch)")

	for _, agent := range allAgents {
		assert.Equal(t, userID, agent.UserID, "All agents should belong to migration user")
		assert.NotEmpty(t, agent.Name, "All agents should have names")
	}

	emptyConfig := &storage.AgentConfig{
		AgentID:     "agent-empty-migration",
		UserID:      userID,
		Name:        "Empty Fields Agent",
		SoulMd:      "",
		AgentsMd:    "",
		UserMd:      "",
		IdentityMd:  "",
		ToolsMd:     "",
		HeartbeatMd: "",
		Config:      nil,
	}

	err = store.Upsert(ctx, emptyConfig)
	require.NoError(t, err)

	retrievedEmpty, err := store.Get(ctx, userID, "agent-empty-migration")
	require.NoError(t, err)
	assert.Empty(t, retrievedEmpty.SoulMd)
	assert.Empty(t, retrievedEmpty.AgentsMd)
	assert.Nil(t, retrievedEmpty.Config)
}

func TestE2E_RealisticDataPersistence(t *testing.T) {
	t.Parallel()

	pool := setupE2ETestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	userID := "e2e-realistic-user"

	createTestUserForE2E(t, ctx, pool, userID)

	store := storage.NewPostgresAgentStore(pool)

	realisticConfig := &storage.AgentConfig{
		AgentID: "agent-realistic-001",
		UserID:  userID,
		Name:    "Code Review Assistant",
		SoulMd: `# Code Review Assistant

You are an expert code reviewer with deep knowledge across multiple programming paradigms and best practices.

## Philosophy
Code review is not just about catching bugs—it's about mentoring, knowledge sharing, and improving team velocity through better code quality.

## Approach
1. **Positive Reinforcement**: Start with what's working well
2. **Actionable Feedback**: Provide specific, implementable suggestions
3. **Context Awareness**: Consider the code's purpose and constraints
4. **Educational Value**: Explain the "why" behind suggestions
5. **Prioritization**: Distinguish between critical issues and nice-to-haves

## Review Categories
- **Correctness**: Bugs, edge cases, logic errors
- **Performance**: Algorithmic efficiency, resource usage
- **Security**: Vulnerabilities, injection risks, data exposure
- **Maintainability**: Code organization, naming, documentation
- **Best Practices**: Language idioms, design patterns
- **Testing**: Test coverage, test quality, edge cases
- **Documentation**: Comments, READMEs, API docs`,
		AgentsMd: `# Review Guidelines

## Code Style
- Follow project conventions
- Use meaningful names for variables and functions
- Keep functions focused and single-purpose
- Prefer composition over inheritance where appropriate
- Write self-documenting code

## Feedback Format
Use the following structure for each comment:

**Issue**: [Clear description]
**Severity**: [Critical/High/Medium/Low]
**Suggestion**: [Specific improvement]
**Example**: [Code snippet if applicable]

## When to Comment
- Always: Security vulnerabilities, critical bugs
- Usually: Performance issues, maintainability concerns
- Sometimes: Style preferences, alternative approaches
- Rarely: Minor optimizations, personal preferences

## Response Behavior
- Be constructive, not critical
- Acknowledge good code explicitly
- Offer to pair program for complex issues
- Reference relevant documentation or resources
- Stay open to discussion and alternative views`,
		UserMd: `# Developer Profile

## Primary User
- **Name**: Sarah Chen
- **Role**: Senior Full-Stack Engineer
- **Experience**: 8 years
- **Primary Languages**: Go, TypeScript, Rust
- **Team Size**: 5 developers

## Code Preferences
- Prefers functional programming patterns
- Values type safety and compile-time checks
- Likes comprehensive error handling
- Prefers explicit over implicit
- Values automated testing

## Project Context
- Building a microservices API gateway
- High-traffic system (10M+ requests/day)
- Strict latency requirements (p95 < 100ms)
- Focus on reliability and observability

## Review Style Preferences
- Wants detailed explanations for security issues
- Prefers code examples over abstract advice
- Values performance optimization suggestions
- Interested in learning new patterns
- Appreciates references to RFCs or standards`,
		IdentityMd: `# Agent Identity

**Name**: ReviewBot
**Version**: 2.1.0
**Created**: 2024-01-15
**Maintained by**: Developer Experience Team

## Purpose
To provide consistent, high-quality code reviews that help developers write better code while learning and growing in their craft.

## Capabilities
- Multi-language code analysis
- Security vulnerability detection
- Performance bottleneck identification
- Best practice enforcement
- Test coverage analysis
- Documentation quality assessment

## Limitations
- Cannot run code in sandboxed environment
- Cannot test external API integrations
- May miss business logic bugs without context
- Not a replacement for human judgment
- Does not have access to production metrics

## Personality Traits
- Encouraging and supportive
- Detail-oriented but not pedantic
- Knowledgeable but humble
- Practical and pragmatic
- Respectful of developer autonomy`,
		ToolsMd: `# Available Tools

## Code Analysis Tools
1. **Static Analysis**: linters, formatters, type checkers
2. **Security Scanning**: dependency vulnerability scanners, SAST tools
3. **Performance Profiling**: benchmarking, profiling tools
4. **Testing**: test runners, coverage reporters, mutation testing

## Documentation Tools
1. **API Docs**: OpenAPI/Swagger generators
2. **Code Docs**: documentation generators (godoc, rustdoc)
3. **README Templates**: standard project documentation
4. **Changelog Generators**: automated changelog from commits

## Code Quality Metrics
1. **Complexity Analysis**: cyclomatic complexity, cognitive load
2. **Duplication Detection**: copy-paste detection
3. **Code Smell Detection**: anti-patterns, code smells
4. **Tech Debt Tracking**: identified issues, suggested refactors

## Integration Points
- Git repository access (read-only)
- CI/CD pipeline integration
- Issue tracker integration (GitHub Issues, Jira)
- Team communication (Slack, Teams notifications)

## Tool Selection Strategy
Choose tools based on:
1. Language ecosystem compatibility
2. Team familiarity and preference
3. Integration capabilities
4. Maintenance and support status
5. Performance impact`,
		HeartbeatMd: `# Health Check Configuration

## Monitoring Parameters
- **Check Interval**: 5 minutes
- **Timeout Threshold**: 30 seconds
- **Retry Attempts**: 3
- **Failure Threshold**: 2 consecutive failures

## Health Checks
1. **Database Connectivity**: Verify connection pool health
2. **API Accessibility**: Check endpoint responsiveness
3. **External Services**: Verify downstream service availability
4. **Resource Usage**: Monitor memory and CPU utilization
5. **Queue Status**: Check message queue depth (if applicable)

## Alerting Rules
- **Critical**: Database unavailable, API down > 2 min
- **High**: Queue depth > threshold, response time > 5s
- **Medium**: High resource usage, degraded performance
- **Low**: Occasional timeouts, minor performance dip

## Recovery Procedures
1. Automatic retry with exponential backoff
2. Circuit breaker pattern for external dependencies
3. Graceful degradation of non-critical features
4. Notify on-call engineer for critical issues
5. Log detailed diagnostics for post-mortem analysis`,
		Config: map[string]any{
			"review_settings": map[string]any{
				"max_review_time_minutes":  30,
				"min_comment_length":       10,
				"max_comment_length":       500,
				"require_comment_examples": true,
				"check_test_coverage":      true,
				"check_documentation":      true,
				"check_security":           true,
			},
			"severity_weights": map[string]float64{
				"critical": 10.0,
				"high":     5.0,
				"medium":   2.0,
				"low":      0.5,
			},
			"language_profiles": []map[string]any{
				{
					"language":      "go",
					"linter":        "golangci-lint",
					"formatter":     "gofmt",
					"test_runner":   "go test",
					"coverage_tool": "go tool cover",
				},
				{
					"language":      "typescript",
					"linter":        "eslint",
					"formatter":     "prettier",
					"test_runner":   "jest",
					"coverage_tool": "istanbul",
				},
			},
			"integration_settings": map[string]any{
				"github_token":        "ghp_xxxxxxxxxxxx",
				"slack_webhook":       "https://hooks.slack.com/services/XXX/YYY/ZZZ",
				"jira_project_key":    "CODE",
				"auto_assign_pr":      true,
				"block_merge_on_fail": false,
			},
		},
	}

	err := store.Upsert(ctx, realisticConfig)
	require.NoError(t, err, "Failed to insert realistic config")

	retrieved, err := store.Get(ctx, userID, "agent-realistic-001")
	require.NoError(t, err, "Failed to retrieve realistic config")

	assert.Equal(t, "Code Review Assistant", retrieved.Name)
	assert.Contains(t, retrieved.SoulMd, "Code review is not just about catching bugs")
	assert.Contains(t, retrieved.SoulMd, "Positive Reinforcement")
	assert.Contains(t, retrieved.AgentsMd, "Feedback Format")
	assert.Contains(t, retrieved.UserMd, "Sarah Chen")
	assert.Contains(t, retrieved.IdentityMd, "ReviewBot")
	assert.Contains(t, retrieved.ToolsMd, "Static Analysis")
	assert.Contains(t, retrieved.HeartbeatMd, "Monitoring Parameters")

	reviewSettings, ok := retrieved.Config["review_settings"].(map[string]any)
	require.True(t, ok, "review_settings should be preserved")
	assert.Equal(t, float64(30), reviewSettings["max_review_time_minutes"])
	assert.Equal(t, float64(10), reviewSettings["min_comment_length"])

	severityWeights, ok := retrieved.Config["severity_weights"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 10.0, severityWeights["critical"])
	assert.Equal(t, 0.5, severityWeights["low"])

	langProfiles, ok := retrieved.Config["language_profiles"].([]any)
	require.True(t, ok)
	assert.Len(t, langProfiles, 2)

	profile1 := langProfiles[0].(map[string]any)
	assert.Equal(t, "go", profile1["language"])

	profile2 := langProfiles[1].(map[string]any)
	assert.Equal(t, "typescript", profile2["language"])

	integrationSettings, ok := retrieved.Config["integration_settings"].(map[string]any)
	require.True(t, ok)
	assert.True(t, integrationSettings["auto_assign_pr"].(bool))
	assert.False(t, integrationSettings["block_merge_on_fail"].(bool))

	loader := storage.NewWorkspaceLoaderFromAgentStore(store)
	workspace, err := loader.Load(ctx, userID, "agent-realistic-001")
	require.NoError(t, err)
	assert.True(t, workspace.Loaded)

	formattedPrompt := workspace.FormatSystemPrompt()
	assert.Contains(t, formattedPrompt, "Code Review Assistant")
	assert.Contains(t, formattedPrompt, "Sarah Chen")
	assert.Contains(t, formattedPrompt, "ReviewBot")
	assert.Contains(t, formattedPrompt, "Available Tools")
}

func TestE2E_ListFunctionalityWithMultipleAgents(t *testing.T) {
	t.Parallel()

	pool := setupE2ETestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()

	userID := "e2e-list-user"
	otherUserID := "e2e-list-other-user"

	createTestUserForE2E(t, ctx, pool, userID)
	createTestUserForE2E(t, ctx, pool, otherUserID)

	store := storage.NewPostgresAgentStore(pool)

	agentNames := []string{
		"Customer Support Bot",
		"Sales Assistant",
		"Technical Support",
		"FAQ Bot",
		"Order Processing",
		"Inventory Manager",
		"Shipping Tracker",
		"Returns Handler",
		"Product Recommender",
		"Payment Processor",
	}

	for i, name := range agentNames {
		config := &storage.AgentConfig{
			AgentID: fmt.Sprintf("agent-list-%03d", i+1),
			UserID:  userID,
			Name:    name,
			SoulMd:  fmt.Sprintf("# %s\n\nPurpose: %s", name, strings.ToLower(strings.ReplaceAll(name, " ", "_"))),
			Config: map[string]any{
				"priority": i + 1,
				"active":   i%2 == 0,
			},
		}
		err := store.Upsert(ctx, config)
		require.NoError(t, err, "Failed to create agent %d: %s", i+1, name)
	}

	for i := 1; i <= 5; i++ {
		config := &storage.AgentConfig{
			AgentID: fmt.Sprintf("agent-other-%03d", i),
			UserID:  otherUserID,
			Name:    fmt.Sprintf("Other User Agent %d", i),
			SoulMd:  fmt.Sprintf("Other user agent %d", i),
		}
		err := store.Upsert(ctx, config)
		require.NoError(t, err)
	}

	agents, err := store.ListByUser(ctx, userID)
	require.NoError(t, err, "Failed to list agents")
	assert.Len(t, agents, 10, "Should have exactly 10 agents")

	agentIDs := make(map[string]bool)
	for _, agent := range agents {
		agentIDs[agent.AgentID] = true
		assert.Equal(t, userID, agent.UserID, "All agents should belong to correct user")
		assert.NotEmpty(t, agent.Name, "All agents should have names")
		assert.NotEmpty(t, agent.SoulMd, "All agents should have soul_md")
	}

	for i := 1; i <= 10; i++ {
		expectedID := fmt.Sprintf("agent-list-%03d", i)
		assert.True(t, agentIDs[expectedID], "Agent ID %s should be in the list", expectedID)
	}

	otherAgents, err := store.ListByUser(ctx, otherUserID)
	require.NoError(t, err)
	assert.Len(t, otherAgents, 5, "Other user should have 5 agents")

	for _, agent := range otherAgents {
		assert.Equal(t, otherUserID, agent.UserID)
	}

	emptyListAgents, err := store.ListByUser(ctx, "non-existent-user")
	require.NoError(t, err)
	assert.Empty(t, emptyListAgents, "Non-existent user should have empty agent list")

	for i, agent := range agents {
		expectedName := agentNames[i]
		if agent.Name != expectedName {
			t.Logf("Agent %d: Expected name '%s', got '%s'", i, expectedName, agent.Name)
		}
	}

	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	for i, agent := range agents {
		workspace, err := loader.Load(ctx, userID, agent.AgentID)
		require.NoError(t, err, "Failed to load workspace for agent %s", agent.AgentID)
		assert.True(t, workspace.Loaded, "Workspace should be loaded for agent %s", agent.AgentID)
		assert.NotEmpty(t, workspace.Soul, "Soul should not be empty for agent %s", agent.AgentID)

		formattedPrompt := workspace.FormatSystemPrompt()
		assert.NotEmpty(t, formattedPrompt, "Formatted prompt should not be empty for agent %s", agent.AgentID)
		assert.Contains(t, formattedPrompt, agentNames[i], "Prompt should contain agent name")
	}

	err = store.Delete(ctx, userID, "agent-list-001")
	require.NoError(t, err)

	agentsAfterDelete, err := store.ListByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, agentsAfterDelete, 9, "Should have 9 agents after deletion")

	var deletedAgentExists bool
	for _, agent := range agentsAfterDelete {
		if agent.AgentID == "agent-list-001" {
			deletedAgentExists = true
			break
		}
	}
	assert.False(t, deletedAgentExists, "Deleted agent should not be in list")

	newAgent := &storage.AgentConfig{
		AgentID: "agent-list-011",
		UserID:  userID,
		Name:    "Eleventh Agent",
		SoulMd:  "# Eleventh Agent\n\nThe newest addition.",
	}
	err = store.Upsert(ctx, newAgent)
	require.NoError(t, err)

	agentsAfterAdd, err := store.ListByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, agentsAfterAdd, 10, "Should have 10 agents after adding one")

	var foundNewAgent bool
	for _, agent := range agentsAfterAdd {
		if agent.AgentID == "agent-list-011" {
			foundNewAgent = true
			assert.Equal(t, "Eleventh Agent", agent.Name)
			break
		}
	}
	assert.True(t, foundNewAgent, "Newly added agent should be in list")
}
