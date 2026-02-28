//go:build integration
// +build integration

// Package billing provides tests for agents table migration.
package billing

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	billingpkg "github.com/nevaria-ai/gateway/internal/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getTestDBURL returns the database URL for testing.
// It checks the TEST_DATABASE_URL environment variable first,
// then falls back to a default localhost connection string.
func getTestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

// setupTestDB creates a test database connection and returns a cleanup function.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := getTestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err, "Failed to parse database URL")

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to test database: %v", err)
		return nil
	}

	cleanupTestDB(t, pool)

	t.Cleanup(func() {
		cleanupTestDB(t, pool)
		pool.Close()
	})

	return pool
}

// cleanupTestDB drops all billing-related tables.
func cleanupTestDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	tables := []string{"usage_logs", "agents", "processed_webhooks", "users"}

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		if err != nil {
			t.Logf("Warning: failed to drop table %s: %v", table, err)
		}
	}
}

// TestCreateAgentsTable_CreatesTableWithCorrectSchema tests that the agents table is created with correct schema.
func TestCreateAgentsTable_CreatesTableWithCorrectSchema(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err, "RunMigrations should not return an error")

	var exists bool
	dbRow := pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'agents')`)
	err = dbRow.Scan(&exists)
	require.NoError(t, err, "Failed to check if agents table exists")
	assert.True(t, exists, "agents table should exist")

	expectedColumns := []struct {
		name     string
		dataType string
		nullable bool
	}{
		{"agent_id", "text", false},
		{"user_id", "text", false},
		{"name", "text", true},
		{"soul_md", "text", true},
		{"agents_md", "text", true},
		{"user_md", "text", true},
		{"identity_md", "text", true},
		{"tools_md", "text", true},
		{"heartbeat_md", "text", true},
		{"config", "jsonb", true},
		{"created_at", "timestamp with time zone", false},
		{"updated_at", "timestamp with time zone", false},
	}

	for _, col := range expectedColumns {
		var colExists bool
		dbRow = pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns 
				WHERE table_name = 'agents' AND column_name = $1
			)
		`, col.name)
		err := dbRow.Scan(&colExists)
		require.NoError(t, err, "Failed to check column %s", col.name)
		assert.True(t, colExists, "Column %s should exist", col.name)

		var dataType string
		var isNullable string
		dbRow = pool.QueryRow(ctx, `
			SELECT data_type, is_nullable 
			FROM information_schema.columns 
			WHERE table_name = 'agents' AND column_name = $1
		`, col.name)
		err = dbRow.Scan(&dataType, &isNullable)
		require.NoError(t, err, "Failed to get column details for %s", col.name)
		assert.Equal(t, col.dataType, dataType, "Column %s should have type %s", col.name, col.dataType)
		assert.Equal(t, map[bool]string{true: "YES", false: "NO"}[col.nullable], isNullable,
			"Column %s nullable should be %v", col.name, col.nullable)
	}

	var pkExists bool
	dbRow = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE table_name = 'agents' AND constraint_type = 'PRIMARY KEY'
		)
	`)
	err = dbRow.Scan(&pkExists)
	require.NoError(t, err)
	assert.True(t, pkExists, "Primary key constraint should exist")

	var pkColumn string
	dbRow = pool.QueryRow(ctx, `
		SELECT column_name
		FROM information_schema.key_column_usage
		WHERE table_name = 'agents'
		LIMIT 1
	`)
	err = dbRow.Scan(&pkColumn)
	require.NoError(t, err)
	assert.Equal(t, "agent_id", pkColumn, "Primary key should be on agent_id column")
}

// TestCreateAgentsTable_CreatesIndexes tests that all required indexes are created.
func TestCreateAgentsTable_CreatesIndexes(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	expectedIndexes := []string{
		"idx_agents_user_id",
		"idx_agents_updated_at",
	}

	for _, indexName := range expectedIndexes {
		var indexExists bool
		dbRow := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)
		`, indexName)
		err := dbRow.Scan(&indexExists)
		require.NoError(t, err, "Failed to check index %s", indexName)
		assert.True(t, indexExists, "index %s should exist", indexName)
	}
}

// TestCreateAgentsTable_IsIdempotent tests that the agents migration can be run multiple times safely.
func TestCreateAgentsTable_IsIdempotent(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err, "First migration should succeed")

	err = billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err, "Second migration should succeed (idempotent)")

	var tableExists bool
	dbRow := pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'agents')`)
	err = dbRow.Scan(&tableExists)
	require.NoError(t, err)
	assert.True(t, tableExists, "agents table should still exist after re-running migrations")

	var indexCount int
	dbRow = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes WHERE indexname LIKE 'idx_agents_%'
	`)
	err = dbRow.Scan(&indexCount)
	require.NoError(t, err)
	assert.Equal(t, 2, indexCount, "All agent indexes should exist after re-running migrations")
}

// TestCreateAgentsTable_ForeignKeyToUsers tests that foreign key to users table exists and works.
func TestCreateAgentsTable_ForeignKeyToUsers(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	var fkExists bool
	dbRow := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE table_name = 'agents' AND constraint_name = 'agents_user_id_fkey'
		)
	`)
	err = dbRow.Scan(&fkExists)
	require.NoError(t, err)
	assert.True(t, fkExists, "foreign key constraint should exist")

	var fkDetails struct {
		referencedTable  string
		referencedColumn string
		deleteRule       string
	}
	dbRow = pool.QueryRow(ctx, `
		SELECT 
			ccu.table_name AS referenced_table,
			ccu.column_name AS referenced_column,
			rc.delete_rule
		FROM information_schema.table_constraints tc
		JOIN information_schema.referential_constraints rc
			ON tc.constraint_name = rc.constraint_name
		JOIN information_schema.constraint_column_usage ccu
			ON rc.unique_constraint_name = ccu.constraint_name
		WHERE tc.table_name = 'agents' AND tc.constraint_name = 'agents_user_id_fkey'
	`)
	err = dbRow.Scan(&fkDetails.referencedTable, &fkDetails.referencedColumn, &fkDetails.deleteRule)
	require.NoError(t, err)
	assert.Equal(t, "users", fkDetails.referencedTable, "Foreign key should reference users table")
	assert.Equal(t, "id", fkDetails.referencedColumn, "Foreign key should reference id column")
	assert.Equal(t, "CASCADE", fkDetails.deleteRule, "Foreign key should cascade on delete")

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user123', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err, "Failed to insert test user")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent123', 'user123', 'Test Agent', NOW(), NOW())
	`)
	require.NoError(t, err, "Should be able to insert agent with valid user_id")

	var agentCount int
	dbRow = pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE user_id = 'user123'`)
	err = dbRow.Scan(&agentCount)
	require.NoError(t, err)
	assert.Equal(t, 1, agentCount, "Agent should be inserted")

	_, err = pool.Exec(ctx, `DELETE FROM users WHERE id = 'user123'`)
	require.NoError(t, err, "Failed to delete user")

	dbRow = pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE user_id = 'user123'`)
	err = dbRow.Scan(&agentCount)
	require.NoError(t, err)
	assert.Equal(t, 0, agentCount, "Agent should be cascade deleted when user is deleted")
}

// TestCreateAgentsTable_ForeignKeyConstraintViolation tests that foreign key constraint violations are enforced.
func TestCreateAgentsTable_ForeignKeyConstraintViolation(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent999', 'nonexistent_user', 'Invalid Agent', NOW(), NOW())
	`)
	assert.Error(t, err, "Should fail to insert agent with non-existent user_id")
	assert.Contains(t, err.Error(), "foreign key", "Error should mention foreign key constraint")
}

// TestCreateAgentsTable_JSONBColumn tests that JSONB column works with complex nested structures.
func TestCreateAgentsTable_JSONBColumn(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user456', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	complexConfig := `{
		"model": "gpt-4",
		"temperature": 0.7,
		"max_tokens": 2000,
		"tools": [
			{"name": "calculator", "type": "function"},
			{"name": "search", "type": "function"}
		],
		"settings": {
			"timeout": 30,
			"retries": 3,
			"advanced": {
				"streaming": true,
				"parallel": false
			}
		}
	}`

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, NOW(), NOW())
	`, "agent456", "user456", "Complex Agent", complexConfig)
	require.NoError(t, err, "Should be able to insert agent with complex JSONB config")

	var retrievedConfig string
	dbRow := pool.QueryRow(ctx, `SELECT config FROM agents WHERE agent_id = 'agent456'`)
	err = dbRow.Scan(&retrievedConfig)
	require.NoError(t, err)
	assert.JSONEq(t, complexConfig, retrievedConfig, "Retrieved JSONB should match original")

	var temperature float64
	dbRow = pool.QueryRow(ctx, `SELECT (config->>'temperature')::float FROM agents WHERE agent_id = 'agent456'`)
	err = dbRow.Scan(&temperature)
	require.NoError(t, err)
	assert.Equal(t, 0.7, temperature, "Should be able to query nested JSONB values")

	var toolCount int
	dbRow = pool.QueryRow(ctx, `SELECT jsonb_array_length(config->'tools') FROM agents WHERE agent_id = 'agent456'`)
	err = dbRow.Scan(&toolCount)
	require.NoError(t, err)
	assert.Equal(t, 2, toolCount, "Should be able to query JSONB array length")
}

// TestCreateAgentsTable_UpdatedAtField tests that updated_at field is set correctly.
func TestCreateAgentsTable_UpdatedAtField(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user789', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	beforeInsert := time.Now()
	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent789', 'user789', 'Test Agent', NOW(), NOW())
	`)
	require.NoError(t, err)

	var createdAt, updatedAt time.Time
	dbRow := pool.QueryRow(ctx, `SELECT created_at, updated_at FROM agents WHERE agent_id = 'agent789'`)
	err = dbRow.Scan(&createdAt, &updatedAt)
	require.NoError(t, err)

	assert.WithinDuration(t, beforeInsert, createdAt, 5*time.Second, "created_at should be set to recent time")
	assert.WithinDuration(t, beforeInsert, updatedAt, 5*time.Second, "updated_at should be set to recent time")
	assert.True(t, updatedAt.After(createdAt.Add(-1*time.Second)) || updatedAt.Equal(createdAt),
		"updated_at should be >= created_at on initial insert")

	time.Sleep(10 * time.Millisecond)

	updateTime := time.Now()
	_, err = pool.Exec(ctx, `
		UPDATE agents SET name = 'Updated Agent', updated_at = NOW() WHERE agent_id = 'agent789'
	`)
	require.NoError(t, err)

	dbRow = pool.QueryRow(ctx, `SELECT name, updated_at FROM agents WHERE agent_id = 'agent789'`)
	var name string
	err = dbRow.Scan(&name, &updatedAt)
	require.NoError(t, err)

	assert.Equal(t, "Updated Agent", name, "name should be updated")
	assert.WithinDuration(t, updateTime, updatedAt, 5*time.Second, "updated_at should reflect update time")
	assert.True(t, updatedAt.After(createdAt.Add(5*time.Millisecond)),
		"updated_at should be greater than created_at after update")
}

// TestCreateAgentsTable_InsertUpdateDelete tests inserting, updating, and deleting agents through raw SQL.
func TestCreateAgentsTable_InsertUpdateDelete(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user101', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	result, err := pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, soul_md, created_at, updated_at)
		VALUES ('agent101', 'user101', 'Test Agent', 'soul_hash', NOW(), NOW())
	`)
	require.NoError(t, err, "Insert should succeed")
	assert.Equal(t, int64(1), result.RowsAffected(), "One row should be inserted")

	var count int
	dbRow := pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE agent_id = 'agent101'`)
	err = dbRow.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Agent should exist after insert")

	result, err = pool.Exec(ctx, `
		UPDATE agents 
		SET name = 'Updated Agent', soul_md = 'new_soul_hash', updated_at = NOW()
		WHERE agent_id = 'agent101'
	`)
	require.NoError(t, err, "Update should succeed")
	assert.Equal(t, int64(1), result.RowsAffected(), "One row should be updated")

	var name, soulMd string
	dbRow = pool.QueryRow(ctx, `SELECT name, soul_md FROM agents WHERE agent_id = 'agent101'`)
	err = dbRow.Scan(&name, &soulMd)
	require.NoError(t, err)
	assert.Equal(t, "Updated Agent", name, "Name should be updated")
	assert.Equal(t, "new_soul_hash", soulMd, "soul_md should be updated")

	result, err = pool.Exec(ctx, `DELETE FROM agents WHERE agent_id = 'agent101'`)
	require.NoError(t, err, "Delete should succeed")
	assert.Equal(t, int64(1), result.RowsAffected(), "One row should be deleted")

	dbRow = pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE agent_id = 'agent101'`)
	err = dbRow.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "Agent should not exist after delete")
}

// TestCreateAgentsTable_NullableColumns tests that nullable columns can be NULL.
func TestCreateAgentsTable_NullableColumns(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user202', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, created_at, updated_at)
		VALUES ('agent202', 'user202', NOW(), NOW())
	`)
	require.NoError(t, err, "Should be able to insert agent with only required columns")

	type nullableFields struct {
		name        pgtype.Text
		soulMd      pgtype.Text
		agentsMd    pgtype.Text
		userMd      pgtype.Text
		identityMd  pgtype.Text
		toolsMd     pgtype.Text
		heartbeatMd pgtype.Text
		config      pgtype.Text
	}
	var nf nullableFields
	dbRow := pool.QueryRow(ctx, `
		SELECT name, soul_md, agents_md, user_md, identity_md, tools_md, heartbeat_md, config
		FROM agents WHERE agent_id = 'agent202'
	`)
	err = dbRow.Scan(&nf.name, &nf.soulMd, &nf.agentsMd, &nf.userMd, &nf.identityMd, &nf.toolsMd, &nf.heartbeatMd, &nf.config)
	require.NoError(t, err)

	assert.False(t, nf.name.Valid, "name should be NULL")
	assert.False(t, nf.soulMd.Valid, "soul_md should be NULL")
	assert.False(t, nf.agentsMd.Valid, "agents_md should be NULL")
	assert.False(t, nf.userMd.Valid, "user_md should be NULL")
	assert.False(t, nf.identityMd.Valid, "identity_md should be NULL")
	assert.False(t, nf.toolsMd.Valid, "tools_md should be NULL")
	assert.False(t, nf.heartbeatMd.Valid, "heartbeat_md should be NULL")
	assert.False(t, nf.config.Valid, "config should be NULL")
}

// TestCreateAgentsTable_PrimaryKeyUnique tests that primary key enforces uniqueness.
func TestCreateAgentsTable_PrimaryKeyUnique(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user303', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent303', 'user303', 'Agent 1', NOW(), NOW())
	`)
	require.NoError(t, err, "First insert should succeed")

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent303', 'user303', 'Agent 2', NOW(), NOW())
	`)
	assert.Error(t, err, "Duplicate primary key should fail")
	assert.Contains(t, err.Error(), "duplicate key", "Error should mention duplicate key")
}

// TestCreateAgentsTable_MultipleAgentsPerUser tests that multiple agents can belong to one user.
func TestCreateAgentsTable_MultipleAgentsPerUser(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user404', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	for i := 1; i <= 5; i++ {
		_, err = pool.Exec(ctx, fmt.Sprintf(`
			INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
			VALUES ('agent404_%d', 'user404', 'Agent %d', NOW(), NOW())
		`, i, i))
		require.NoError(t, err, "Insert agent %d should succeed", i)
	}

	var count int
	dbRow := pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE user_id = 'user404'`)
	err = dbRow.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count, "Should have 5 agents for user404")
}

// TestCreateAgentsTable_QueryWithIndex tests that queries can use indexes efficiently.
func TestCreateAgentsTable_QueryWithIndex(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user505', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent505_1', 'user505', 'Agent 1', NOW() - INTERVAL '1 hour', NOW() - INTERVAL '1 hour')
	`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent505_2', 'user505', 'Agent 2', NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '30 minutes')
	`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent505_3', 'user505', 'Agent 3', NOW(), NOW())
	`)
	require.NoError(t, err)

	rows, err := pool.Query(ctx, `
		SELECT agent_id, name FROM agents 
		WHERE user_id = $1 
		ORDER BY updated_at DESC
	`, "user505")
	require.NoError(t, err, "Query with user_id filter should succeed")

	agentIDs := []string{}
	defer rows.Close()
	for rows.Next() {
		var agentID, name string
		err = rows.Scan(&agentID, &name)
		require.NoError(t, err)
		agentIDs = append(agentIDs, agentID)
	}
	require.NoError(t, rows.Err())

	assert.Len(t, agentIDs, 3, "Should return 3 agents")
	assert.Equal(t, "agent505_3", agentIDs[0], "Most recently updated agent should be first")
	assert.Equal(t, "agent505_2", agentIDs[1], "Middle agent should be second")
	assert.Equal(t, "agent505_1", agentIDs[2], "Oldest agent should be last")
}

// TestCreateAgentsTable_JSONBNull tests that JSONB column can be NULL.
func TestCreateAgentsTable_JSONBNull(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user606', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, config, created_at, updated_at)
		VALUES ('agent606', 'user606', 'Agent without config', NULL, NOW(), NOW())
	`)
	require.NoError(t, err, "Should be able to insert agent with NULL config")

	var config pgtype.Text
	dbRow := pool.QueryRow(ctx, `SELECT config FROM agents WHERE agent_id = 'agent606'`)
	err = dbRow.Scan(&config)
	require.NoError(t, err)
	assert.False(t, config.Valid, "config should be NULL")
}

// TestCreateAgentsTable_JSONBEmptyObject tests that JSONB column can hold empty objects.
func TestCreateAgentsTable_JSONBEmptyObject(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user707', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, config, created_at, updated_at)
		VALUES ('agent707', 'user707', 'Agent with empty config', '{}'::jsonb, NOW(), NOW())
	`)
	require.NoError(t, err, "Should be able to insert agent with empty JSONB object")

	var config string
	dbRow := pool.QueryRow(ctx, `SELECT config FROM agents WHERE agent_id = 'agent707'`)
	err = dbRow.Scan(&config)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, config, "config should be empty JSONB object")
}

// TestCreateAgentsTable_TransactionalInserts tests that multiple agent inserts can be transactional.
func TestCreateAgentsTable_TransactionalInserts(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := billingpkg.RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user808', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err, "Should begin transaction")

	_, err = tx.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent808_1', 'user808', 'Agent 1', NOW(), NOW())
	`)
	require.NoError(t, err, "First insert in transaction should succeed")

	_, err = tx.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent808_2', 'user808', 'Agent 2', NOW(), NOW())
	`)
	require.NoError(t, err, "Second insert in transaction should succeed")

	err = tx.Commit(ctx)
	require.NoError(t, err, "Transaction commit should succeed")

	var count int
	dbRow := pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE user_id = 'user808'`)
	err = dbRow.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Both agents should be inserted in committed transaction")

	tx, err = pool.Begin(ctx)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent808_3', 'user808', 'Agent 3', NOW(), NOW())
	`)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO agents (agent_id, user_id, name, created_at, updated_at)
		VALUES ('agent808_3', 'user808', 'Agent 3 duplicate', NOW(), NOW())
	`)
	assert.Error(t, err, "Duplicate insert in transaction should fail")

	err = tx.Rollback(ctx)
	require.NoError(t, err, "Transaction rollback should succeed")

	dbRow = pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE user_id = 'user808'`)
	err = dbRow.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Rolled back transaction should not insert any agents")
}
