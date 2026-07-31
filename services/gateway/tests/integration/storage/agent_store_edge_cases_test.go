//go:build integration

package storage_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radch-ai/ava/services/gateway/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getAgentStoreTestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

// createAgentsTable creates the agents table for testing.
// Backend owns migrations; gateway tests create table directly.
func createAgentsTableForStoreTest(ctx context.Context, pool *pgxpool.Pool) error {
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

func setupAgentStoreTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbURL := getAgentStoreTestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err, "Failed to parse database URL")

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to test database: %v", err)
		return nil
	}

	err = createAgentsTableForStoreTest(ctx, pool)
	if err != nil {
		pool.Close()
		t.Skipf("Skipping test: cannot create agents table: %v", err)
		return nil
	}

	t.Cleanup(func() {
		cleanupAgentStoreTestData(t, pool)
		pool.Close()
	})

	return pool
}

func cleanupAgentStoreTestData(t *testing.T, pool *pgxpool.Pool) {
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

func createAgentStoreTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) {
	t.Helper()

	now := time.Now().Unix()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ($1, 1, 100, $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, userID, now, now)
	require.NoError(t, err, "Failed to create test user")
}

// TestConcurrentUpsert tests multiple goroutines upserting the same agent concurrently
func TestConcurrentUpsert(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-concurrent-user"
	agentID := "concurrent-agent"
	createAgentStoreTestUser(t, ctx, pool, userID)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(iteration int) {
			defer wg.Done()
			config := &storage.AgentConfig{
				AgentID: agentID,
				UserID:  userID,
				Name:    fmt.Sprintf("Concurrent Agent %d", iteration),
				SoulMd:  fmt.Sprintf("Iteration %d", iteration),
				Config:  map[string]any{"iteration": iteration},
			}
			if err := store.Upsert(ctx, config); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent upsert failed: %v", err)
	}

	retrieved, err := store.Get(ctx, userID, agentID)
	require.NoError(t, err, "Should be able to retrieve after concurrent upserts")
	assert.Equal(t, agentID, retrieved.AgentID)
	assert.Equal(t, userID, retrieved.UserID)
}

// TestConcurrentGetAndUpsert tests concurrent reads and writes
func TestConcurrentGetAndUpsert(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-concurrent-getupsert"
	agentID := "getupsert-agent"
	createAgentStoreTestUser(t, ctx, pool, userID)

	initialConfig := &storage.AgentConfig{
		AgentID: agentID,
		UserID:  userID,
		Name:    "Initial",
		Config:  map[string]any{"version": 0},
	}
	err := store.Upsert(ctx, initialConfig)
	require.NoError(t, err)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := store.Get(ctx, userID, agentID)
			if err != nil {
				t.Errorf("Get failed during concurrent test: %v", err)
			}
		}()

		go func(iteration int) {
			defer wg.Done()
			config := &storage.AgentConfig{
				AgentID: agentID,
				UserID:  userID,
				Name:    fmt.Sprintf("Version %d", iteration),
				Config:  map[string]any{"version": iteration},
			}
			if err := store.Upsert(ctx, config); err != nil {
				t.Errorf("Upsert failed during concurrent test: %v", err)
			}
		}(i)
	}

	wg.Wait()

	_, err = store.Get(ctx, userID, agentID)
	assert.NoError(t, err, "Agent should still be retrievable after concurrent operations")
}

// TestLargeDataUpsert tests handling of very long markdown strings
func TestLargeDataUpsert(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"10KB", 10 * 1024, false},
		{"100KB", 100 * 1024, false},
		{"500KB", 500 * 1024, false},
		{"1MB", 1024 * 1024, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := storage.NewPostgresAgentStore(pool)
			userID := fmt.Sprintf("test-large-data-%s", tt.name)
			agentID := fmt.Sprintf("large-agent-%s", tt.name)
			createAgentStoreTestUser(t, ctx, pool, userID)

			largeContent := strings.Repeat("x", tt.size)

			config := &storage.AgentConfig{
				AgentID:     agentID,
				UserID:      userID,
				Name:        fmt.Sprintf("Large Data Agent %s", tt.name),
				SoulMd:      largeContent,
				AgentsMd:    largeContent,
				UserMd:      largeContent,
				IdentityMd:  largeContent,
				ToolsMd:     largeContent,
				HeartbeatMd: largeContent,
			}

			err := store.Upsert(ctx, config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err, "Upsert should succeed for %s of data", tt.name)

			retrieved, err := store.Get(ctx, userID, agentID)
			assert.NoError(t, err, "Should retrieve large data")
			assert.Equal(t, tt.size, len(retrieved.SoulMd), "Retrieved data size should match")
		})
	}
}

// TestComplexJSONB tests nested structures, arrays, and nulls in JSONB
func TestComplexJSONB(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name: "nested objects",
			config: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"level3": "deep value",
						"number": 42.0,
					},
					"array": []any{"a", "b", "c"},
				},
			},
		},
		{
			name: "mixed types",
			config: map[string]any{
				"string":  "text",
				"number":  123.456,
				"boolean": true,
				"null":    nil,
				"array":   []any{1.0, "two", 3.0, true, nil},
			},
		},
		{
			name: "deeply nested arrays",
			config: map[string]any{
				"nested": []any{
					[]any{
						[]any{"level3"},
					},
				},
			},
		},
		{
			name: "special chars in keys",
			config: map[string]any{
				"key with spaces":      "value",
				"key-with-dashes":      "value",
				"key_with_underscores": "value",
				"key.with.dots":        "value",
			},
		},
		{
			name: "unicode values",
			config: map[string]any{
				"greeting":   "Hello 世界",
				"emoji":      "🎉🚀✨",
				"arabic":     "مرحبا",
				"emoji-text": "Test with 😀 🎊 🌟",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := storage.NewPostgresAgentStore(pool)
			userID := fmt.Sprintf("test-jsonb-%s", tt.name)
			agentID := fmt.Sprintf("jsonb-agent-%s", tt.name)
			createAgentStoreTestUser(t, ctx, pool, userID)

			config := &storage.AgentConfig{
				AgentID: agentID,
				UserID:  userID,
				Name:    fmt.Sprintf("JSONB Test %s", tt.name),
				Config:  tt.config,
			}

			err := store.Upsert(ctx, config)
			require.NoError(t, err, "Upsert should succeed for complex JSONB")

			retrieved, err := store.Get(ctx, userID, agentID)
			require.NoError(t, err, "Get should succeed for complex JSONB")

			assert.NotNil(t, retrieved.Config, "Config should not be nil")
			assert.Equal(t, len(tt.config), len(retrieved.Config), "Config should have same number of keys")

			for key, expectedVal := range tt.config {
				actualVal, exists := retrieved.Config[key]
				assert.True(t, exists, "Key %s should exist in retrieved config", key)

				if expectedStr, ok := expectedVal.(string); ok {
					assert.Equal(t, expectedStr, actualVal, "String value for key %s should match", key)
				} else if expectedNum, ok := expectedVal.(float64); ok {
					if actualNum, ok := actualVal.(float64); ok {
						assert.Equal(t, expectedNum, actualNum, "Number value for key %s should match", key)
					}
				} else if expectedBool, ok := expectedVal.(bool); ok {
					assert.Equal(t, expectedBool, actualVal, "Boolean value for key %s should match", key)
				} else if expectedVal == nil {
					assert.Nil(t, actualVal, "Nil value for key %s should be nil", key)
				}
			}
		})
	}
}

// TestSpecialCharacters tests Unicode, emojis, and SQL injection attempts
func TestSpecialCharacters(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	tests := []struct {
		name      string
		agentID   string
		userID    string
		nameField string
		markdown  string
		wantErr   bool
	}{
		{
			name:      "unicode characters",
			agentID:   "agent-unicode",
			userID:    "user-unicode",
			nameField: "Unicode Agent 世界",
			markdown:  "Content with العربية and עברית",
		},
		{
			name:      "emoji characters",
			agentID:   "agent-emoji-🚀",
			userID:    "user-emoji-✨",
			nameField: "Emoji Agent 🎉😀",
			markdown:  "Test with emojis 🌟🎊🎈",
		},
		{
			name:      "special chars",
			agentID:   "agent-special-'\"-;",
			userID:    "user-special-\\/-@-#",
			nameField: "Special '\"\\",
			markdown:  "Content with 'quotes' and \"double quotes\"",
		},
		{
			name:      "sql injection attempt in name",
			agentID:   "agent-1",
			userID:    "user-sqli",
			nameField: "'; DROP TABLE agents; --",
			markdown:  "normal content",
			wantErr:   false,
		},
		{
			name:      "sql injection attempt in markdown",
			agentID:   "agent-2",
			userID:    "user-sqli-2",
			nameField: "Normal Name",
			markdown:  "'; DROP TABLE agents; --",
			wantErr:   false,
		},
		{
			name:      "newlines and tabs",
			agentID:   "agent-whitespace",
			userID:    "user-whitespace",
			nameField: "Agent\nWith\tNewlines",
			markdown:  "Line 1\nLine 2\n\tIndented",
		},
		{
			name:      "null bytes - expected to fail",
			agentID:   "agent-nullbyte",
			userID:    "user-nullbyte",
			nameField: "Agent\x00Null",
			markdown:  "With\x00null\x00bytes",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := storage.NewPostgresAgentStore(pool)
			createAgentStoreTestUser(t, ctx, pool, tt.userID)

			config := &storage.AgentConfig{
				AgentID: tt.agentID,
				UserID:  tt.userID,
				Name:    tt.nameField,
				SoulMd:  tt.markdown,
			}

			err := store.Upsert(ctx, config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err, "Upsert should handle special characters")

			retrieved, err := store.Get(ctx, tt.userID, tt.agentID)
			assert.NoError(t, err, "Get should retrieve special characters")
			assert.Equal(t, tt.nameField, retrieved.Name, "Name should match")
		})
	}
}

// TestForeignKeyViolation tests behavior with non-existent user_id
func TestForeignKeyViolation(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)

	nonExistentUserID := "non-existent-user-xyz123"

	config := &storage.AgentConfig{
		AgentID: "agent-no-user",
		UserID:  nonExistentUserID,
		Name:    "Agent Without User",
	}

	err := store.Upsert(ctx, config)
	assert.Error(t, err, "Upsert should fail for non-existent user")
	assert.Contains(t, strings.ToLower(err.Error()), "foreign key",
		"Error should mention foreign key constraint")

	_, err = store.Get(ctx, nonExistentUserID, "agent-no-user")
	assert.Error(t, err, "Get should also fail appropriately")
}

// TestDuplicateKeyHandling tests upserting the same agent multiple times
func TestDuplicateKeyHandling(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-duplicate-user"
	agentID := "duplicate-agent"
	createAgentStoreTestUser(t, ctx, pool, userID)

	versions := []struct {
		name   string
		soulMd string
		config map[string]any
	}{
		{"Version 1", "First version", map[string]any{"v": 1.0}},
		{"Version 2", "Second version", map[string]any{"v": 2.0}},
		{"Version 3", "Third version", map[string]any{"v": 3.0}},
	}

	for i, version := range versions {
		config := &storage.AgentConfig{
			AgentID: agentID,
			UserID:  userID,
			Name:    version.name,
			SoulMd:  version.soulMd,
			Config:  version.config,
		}

		err := store.Upsert(ctx, config)
		assert.NoError(t, err, "Upsert version %d should succeed", i+1)

		retrieved, err := store.Get(ctx, userID, agentID)
		assert.NoError(t, err)
		assert.Equal(t, version.name, retrieved.Name, "Name should be updated to latest")
		assert.Equal(t, version.soulMd, retrieved.SoulMd, "SoulMd should be updated to latest")
		assert.Equal(t, version.config, retrieved.Config, "Config should be updated to latest")
	}

	var count int
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM agents WHERE agent_id = $1", agentID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should be exactly one agent after multiple upserts")
}

// TestCaseSensitivity tests that agent IDs are case-sensitive
func TestCaseSensitivity(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-casesensitive"
	createAgentStoreTestUser(t, ctx, pool, userID)

	agentVariations := []string{
		"MyAgent",
		"myagent",
		"MYAGENT",
		"My-Agent",
		"mY-aGeNt",
	}

	for i, agentID := range agentVariations {
		config := &storage.AgentConfig{
			AgentID: agentID,
			UserID:  userID,
			Name:    fmt.Sprintf("Agent %d", i),
		}

		err := store.Upsert(ctx, config)
		assert.NoError(t, err, "Upsert should succeed for case variation: %s", agentID)
	}

	for _, agentID := range agentVariations {
		retrieved, err := store.Get(ctx, userID, agentID)
		assert.NoError(t, err, "Get should succeed for case variation: %s", agentID)
		assert.Equal(t, agentID, retrieved.AgentID, "AgentID should preserve case")
	}

	agents, err := store.ListByUser(ctx, userID)
	assert.NoError(t, err)
	assert.Equal(t, len(agentVariations), len(agents), "Should have all case variations")
}

// TestNullAndEmptyFields tests that all fields except agent_id/user_id are optional
func TestNullAndEmptyFields(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	tests := []struct {
		name      string
		config    *storage.AgentConfig
		expectErr bool
	}{
		{
			name: "all optional fields null",
			config: &storage.AgentConfig{
				AgentID: "agent-nulls",
				UserID:  "test-null-user",
				Name:    "",
				SoulMd:  "",
				Config:  nil,
			},
			expectErr: false,
		},
		{
			name: "some fields null",
			config: &storage.AgentConfig{
				AgentID:  "agent-some-nulls",
				UserID:   "test-some-null-user",
				Name:     "Has Name",
				SoulMd:   "",
				AgentsMd: "",
				Config:   nil,
			},
			expectErr: false,
		},
		{
			name: "empty strings in markdown fields",
			config: &storage.AgentConfig{
				AgentID:     "agent-empty-md",
				UserID:      "test-empty-md-user",
				SoulMd:      "",
				AgentsMd:    "",
				UserMd:      "",
				IdentityMd:  "",
				ToolsMd:     "",
				HeartbeatMd: "",
			},
			expectErr: false,
		},
		{
			name: "empty config map",
			config: &storage.AgentConfig{
				AgentID: "agent-empty-config",
				UserID:  "test-empty-config-user",
				Name:    "Agent",
				Config:  map[string]any{},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := storage.NewPostgresAgentStore(pool)
			createAgentStoreTestUser(t, ctx, pool, tt.config.UserID)

			err := store.Upsert(ctx, tt.config)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err, "Upsert should succeed with null/empty fields")

			retrieved, err := store.Get(ctx, tt.config.UserID, tt.config.AgentID)
			assert.NoError(t, err)
			assert.Equal(t, tt.config.AgentID, retrieved.AgentID)
			assert.Equal(t, tt.config.UserID, retrieved.UserID)
			assert.Equal(t, tt.config.Name, retrieved.Name)
		})
	}
}

// TestTransactionIsolation tests behavior when context is cancelled
func TestTransactionIsolation(t *testing.T) {
	pool := setupAgentStoreTestDB(t)
	if pool == nil {
		return
	}

	t.Run("cancelled context on upsert", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		store := storage.NewPostgresAgentStore(pool)
		userID := "test-cancel-upsert"
		agentID := "cancel-upsert-agent"
		createAgentStoreTestUser(t, ctx, pool, userID)

		cancel()

		config := &storage.AgentConfig{
			AgentID: agentID,
			UserID:  userID,
			Name:    "Should Not Be Stored",
		}

		err := store.Upsert(ctx, config)
		assert.Error(t, err, "Upsert with cancelled context should fail")

		_, err = store.Get(context.Background(), userID, agentID)
		assert.Error(t, err, "Agent should not exist after cancelled upsert")
	})

	t.Run("cancelled context on get", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		store := storage.NewPostgresAgentStore(pool)
		userID := "test-cancel-get"
		agentID := "cancel-get-agent"
		createAgentStoreTestUser(t, ctx, pool, userID)

		config := &storage.AgentConfig{
			AgentID: agentID,
			UserID:  userID,
			Name:    "Test Agent",
		}
		err := store.Upsert(context.Background(), config)
		require.NoError(t, err)

		cancel()

		_, err = store.Get(ctx, userID, agentID)
		assert.Error(t, err, "Get with cancelled context should fail")
	})
}

// BenchmarkUpsert benchmarks Upsert operation
func BenchmarkUpsert(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	dbURL := getAgentStoreTestDBURL()
	ctx := context.Background()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(b, err)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		b.Skip("Cannot connect to database")
		return
	}
	defer pool.Close()

	err = createAgentsTableForStoreTest(ctx, pool)
	if err != nil {
		b.Skip("Cannot create agents table")
		return
	}

	store := storage.NewPostgresAgentStore(pool)
	userID := "bench-user"
	createAgentUser(b, ctx, pool, userID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config := &storage.AgentConfig{
			AgentID: fmt.Sprintf("bench-agent-%d", i),
			UserID:  userID,
			Name:    "Benchmark Agent",
			Config:  map[string]any{"iteration": i},
		}
		if err := store.Upsert(ctx, config); err != nil {
			b.Fatalf("Upsert failed: %v", err)
		}
	}
}

// BenchmarkGet benchmarks Get operation
func BenchmarkGet(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	dbURL := getAgentStoreTestDBURL()
	ctx := context.Background()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(b, err)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		b.Skip("Cannot connect to database")
		return
	}
	defer pool.Close()

	err = createAgentsTableForStoreTest(ctx, pool)
	if err != nil {
		b.Skip("Cannot create agents table")
		return
	}

	store := storage.NewPostgresAgentStore(pool)
	userID := "bench-get-user"
	createAgentUser(b, ctx, pool, userID)

	agentID := "bench-get-agent"
	initialConfig := &storage.AgentConfig{
		AgentID: agentID,
		UserID:  userID,
		Name:    "Benchmark Get Agent",
		Config:  map[string]any{"data": "test"},
	}
	err = store.Upsert(ctx, initialConfig)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.Get(ctx, userID, agentID)
		if err != nil {
			b.Fatalf("Get failed: %v", err)
		}
	}
}

// BenchmarkListByUser benchmarks ListByUser operation
func BenchmarkListByUser(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	dbURL := getAgentStoreTestDBURL()
	ctx := context.Background()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(b, err)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		b.Skip("Cannot connect to database")
		return
	}
	defer pool.Close()

	err = createAgentsTableForStoreTest(ctx, pool)
	if err != nil {
		b.Skip("Cannot create agents table")
		return
	}

	store := storage.NewPostgresAgentStore(pool)
	userID := "bench-list-user"
	createAgentUser(b, ctx, pool, userID)

	for i := 0; i < 100; i++ {
		config := &storage.AgentConfig{
			AgentID: fmt.Sprintf("bench-list-agent-%d", i),
			UserID:  userID,
			Name:    fmt.Sprintf("Agent %d", i),
		}
		if err := store.Upsert(ctx, config); err != nil {
			b.Fatalf("Setup Upsert failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.ListByUser(ctx, userID)
		if err != nil {
			b.Fatalf("ListByUser failed: %v", err)
		}
	}
}

func createTestUser(b *testing.B, ctx context.Context, pool *pgxpool.Pool, userID string) {
	now := time.Now().Unix()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ($1, 1, 100, $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, userID, now, now)
	if err != nil {
		b.Fatalf("Failed to create test user: %v", err)
	}
}

func createAgentUser(b *testing.B, ctx context.Context, pool *pgxpool.Pool, userID string) {
	createTestUser(b, ctx, pool, userID)
}
