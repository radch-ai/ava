// Package billing provides tests for database migrations.
package billing

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

	// Clean up any existing test tables
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

	// Drop tables in correct order (respecting foreign keys)
	tables := []string{"usage_logs", "processed_webhooks", "users"}

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		if err != nil {
			t.Logf("Warning: failed to drop table %s: %v", table, err)
		}
	}
}

// TestRunMigrations_CreatesUsersTable tests that the users table is created with correct schema.
func TestRunMigrations_CreatesUsersTable(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := RunMigrations(ctx, dbURL)
	require.NoError(t, err, "RunMigrations should not return an error")

	// Verify users table exists
	var exists bool
	dbRow := pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'users')`)
	err = dbRow.Scan(&exists)
	require.NoError(t, err, "Failed to check if users table exists")
	assert.True(t, exists, "users table should exist")

	// Verify columns exist
	columns := []string{"id", "email", "tier", "credits_remaining", "subscription_tier", "subscription_status", "billing_period_start", "billing_period_end", "revenuecat_customer_id", "created_at", "updated_at"}

	for _, col := range columns {
		var colExists bool
		dbRow = pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = $1)
		`, col)
		err := dbRow.Scan(&colExists)
		require.NoError(t, err, "Failed to check column %s", col)
		assert.True(t, colExists, "Column %s should exist", col)
	}

	// Verify check constraint for tier values
	var constraintExists bool
	dbRow = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'users_tier_check'
		)
	`)
	err = dbRow.Scan(&constraintExists)
	require.NoError(t, err)
	assert.True(t, constraintExists, "tier check constraint should exist")
}

// TestRunMigrations_CreatesUsageLogsTable tests that the usage_logs table is created.
func TestRunMigrations_CreatesUsageLogsTable(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	// Verify usage_logs table exists
	var exists bool
	dbRow := pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'usage_logs')`)
	err = dbRow.Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "usage_logs table should exist")

	// Verify foreign key constraint exists
	var fkExists bool
	dbRow = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE constraint_name = 'usage_logs_user_id_fkey'
		)
	`)
	err = dbRow.Scan(&fkExists)
	require.NoError(t, err)
	assert.True(t, fkExists, "foreign key constraint should exist")
}

// TestRunMigrations_CreatesProcessedWebhooksTable tests that the processed_webhooks table is created.
func TestRunMigrations_CreatesProcessedWebhooksTable(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	// Verify processed_webhooks table exists
	var exists bool
	dbRow := pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'processed_webhooks')`)
	err = dbRow.Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "processed_webhooks table should exist")
}

// TestRunMigrations_CreatesIndexes tests that all required indexes are created.
func TestRunMigrations_CreatesIndexes(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	err := RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	expectedIndexes := []string{
		"idx_users_tier",
		"idx_users_updated_at",
		"idx_users_email",
		"idx_users_revenuecat_customer_id",
		"idx_usage_logs_user_id",
		"idx_usage_logs_agent_id",
		"idx_usage_logs_timestamp",
		"idx_usage_logs_user_timestamp",
		"idx_processed_webhooks_processed_at",
		"idx_processed_webhooks_event_type",
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

// TestRunMigrations_IsIdempotent tests that migrations can be run multiple times safely.
func TestRunMigrations_IsIdempotent(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	// Run migrations twice
	err := RunMigrations(ctx, dbURL)
	require.NoError(t, err, "First migration should succeed")

	err = RunMigrations(ctx, dbURL)
	require.NoError(t, err, "Second migration should succeed (idempotent)")

	// Verify tables still exist and work correctly
	var tableCount int
	dbRow := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_name IN ('users', 'usage_logs', 'processed_webhooks')
	`)
	err = dbRow.Scan(&tableCount)
	require.NoError(t, err)
	assert.Equal(t, 3, tableCount, "All tables should exist after re-running migrations")
}

// TestRunMigrations_WithExistingData tests that migrations preserve existing data.
func TestRunMigrations_WithExistingData(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	// Run initial migrations
	err := RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	// Insert test data
	now := time.Now().Unix()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, tier, credits_remaining, created_at, updated_at)
		VALUES ('user123', 1, 100.50, $1, $2)
	`, now, now)
	require.NoError(t, err, "Failed to insert test user")

	// Run migrations again
	err = RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	// Verify data is preserved
	var tier int
	var credits float64
	dbRow := pool.QueryRow(ctx, `SELECT tier, credits_remaining FROM users WHERE id = 'user123'`)
	err = dbRow.Scan(&tier, &credits)
	require.NoError(t, err)
	assert.Equal(t, 1, tier, "User tier should be preserved")
	assert.Equal(t, 100.50, credits, "User credits should be preserved")
}

// TestCheckMigrations tests the CheckMigrations function.
func TestCheckMigrations(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	dbURL := getTestDBURL()

	// Check migrations before running them
	results, err := CheckMigrations(ctx, dbURL)
	require.NoError(t, err, "CheckMigrations should not return an error")
	require.Len(t, results, len(migrations), "Should return results for all migrations")

	// All migrations should show as not existing
	for _, result := range results {
		assert.False(t, result.AlreadyExists, "Migration %s should not exist before running", result.MigrationName)
		assert.NoError(t, result.Error, "CheckMigrations should not return errors")
	}

	// Run migrations
	err = RunMigrations(ctx, dbURL)
	require.NoError(t, err)

	// Check migrations again
	results, err = CheckMigrations(ctx, dbURL)
	require.NoError(t, err)

	// All migrations should now exist (at least the table creation ones)
	for _, result := range results {
		switch result.MigrationName {
		case "create_users_table", "create_usage_logs_table", "create_processed_webhooks_table",
			"create_users_indexes", "create_usage_logs_indexes", "create_processed_webhooks_indexes",
			"create_usage_logs_foreign_key":
			assert.True(t, result.AlreadyExists, "Migration %s should exist after running", result.MigrationName)
		}
		assert.NoError(t, result.Error, "CheckMigrations should not return errors")
	}
}

// TestRunMigrationsWithPool tests running migrations with an existing pool.
func TestRunMigrationsWithPool(t *testing.T) {
	pool := setupTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()

	err := RunMigrationsWithPool(ctx, pool)
	require.NoError(t, err, "RunMigrationsWithPool should not return an error")

	// Verify tables exist
	var exists bool
	dbRow := pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'users')`)
	err = dbRow.Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "users table should exist")
}

// TestRunMigrations_InvalidURL tests error handling for invalid database URL.
func TestRunMigrations_InvalidURL(t *testing.T) {
	ctx := context.Background()

	err := RunMigrations(ctx, "")
	assert.Error(t, err, "Empty URL should return an error")

	err = RunMigrations(ctx, "invalid://url")
	assert.Error(t, err, "Invalid URL should return an error")
}

// TestRunMigrationsWithPool_NilPool tests error handling for nil pool.
func TestRunMigrationsWithPool_NilPool(t *testing.T) {
	ctx := context.Background()

	err := RunMigrationsWithPool(ctx, nil)
	assert.Error(t, err, "Nil pool should return an error")
}
