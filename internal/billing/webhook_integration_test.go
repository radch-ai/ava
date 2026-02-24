//go:build integration
// +build integration

package billing

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getIntegrationTestDBURL returns the database URL for integration testing.
func getIntegrationTestDBURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://test:test@localhost:5433/test?sslmode=disable"
}

// TestMigrations_Integration_WithPgxPool tests that migrations work correctly
// and create all required tables and indexes.
func TestMigrations_Integration_WithPgxPool(t *testing.T) {
	dbURL := getIntegrationTestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err, "Failed to connect to test database")
	defer pool.Close()

	// Verify connection
	err = pool.Ping(ctx)
	require.NoError(t, err, "Failed to ping test database")

	// Run migrations
	err = RunMigrationsWithPool(ctx, pool)
	require.NoError(t, err, "Failed to run migrations")

	// Verify all tables exist
	tables := []struct {
		name    string
		columns []string
		hasFK   bool
		fkTable string
	}{
		{name: "users", columns: []string{"id", "email", "tier", "credits_remaining", "subscription_tier", "subscription_status", "billing_period_start", "billing_period_end", "revenuecat_customer_id", "created_at", "updated_at"}},
		{name: "usage_logs", columns: []string{"id", "user_id", "agent_id", "tokens_used", "credit_cost", "timestamp"}, hasFK: true, fkTable: "users"},
		{name: "processed_webhooks", columns: []string{"event_id", "processed_at", "event_type"}},
	}

	for _, tt := range tables {
		t.Run("table_"+tt.name, func(t *testing.T) {
			// Verify table exists
			var exists bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = $1)",
				tt.name,
			).Scan(&exists)
			require.NoError(t, err)
			assert.True(t, exists, "%s table should exist", tt.name)

			// Verify columns exist
			for _, col := range tt.columns {
				var colExists bool
				err := pool.QueryRow(ctx,
					"SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = $2)",
					tt.name, col,
				).Scan(&colExists)
				require.NoError(t, err)
				assert.True(t, colExists, "column %s should exist in %s", col, tt.name)
			}

			// Verify foreign key if applicable
			if tt.hasFK {
				var fkExists bool
				err := pool.QueryRow(ctx,
					"SELECT EXISTS(SELECT 1 FROM information_schema.table_constraints WHERE table_name = $1 AND constraint_type = 'FOREIGN KEY')",
					tt.name,
				).Scan(&fkExists)
				require.NoError(t, err)
				assert.True(t, fkExists, "%s table should have foreign key constraint", tt.name)
			}
		})
	}

	// Verify indexes exist
	t.Run("indexes", func(t *testing.T) {
		indexes := []struct {
			table  string
			column string
		}{
			{"users", "email"},
			{"users", "revenuecat_customer_id"},
			{"usage_logs", "user_id"},
			{"usage_logs", "timestamp"},
		}

		for _, idx := range indexes {
			var idxExists bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE tablename = $1 AND indexdef LIKE '%' || $2 || '%')",
				idx.table, idx.column,
			).Scan(&idxExists)
			require.NoError(t, err)
			assert.True(t, idxExists, "index on %s.%s should exist", idx.table, idx.column)
		}
	})
}

// TestMigrations_Integration_Idempotency verifies that migrations can be run multiple times
func TestMigrations_Integration_Idempotency(t *testing.T) {
	dbURL := getIntegrationTestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err, "Failed to connect to test database")
	defer pool.Close()

	// Run migrations twice
	for i := 0; i < 2; i++ {
		err = RunMigrationsWithPool(ctx, pool)
		require.NoError(t, err, "Migration run %d should succeed", i+1)
	}

	// Verify tables still exist and are accessible
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	require.NoError(t, err)
}

// TestMigrations_Integration_DataPreservation verifies data is preserved across migrations
func TestMigrations_Integration_DataPreservation(t *testing.T) {
	dbURL := getIntegrationTestDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err, "Failed to connect to test database")
	defer pool.Close()

	// Run migrations
	err = RunMigrationsWithPool(ctx, pool)
	require.NoError(t, err, "Failed to run migrations")

	// Insert test data
	now := time.Now().Unix()
	_, err = pool.Exec(ctx,
		"INSERT INTO users (id, email, tier, credits_remaining, subscription_status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		"test_user_preserve", "preserve@test.com", 1, 500.0, "active", now, now,
	)
	require.NoError(t, err, "Failed to insert test data")

	// Run migrations again
	err = RunMigrationsWithPool(ctx, pool)
	require.NoError(t, err, "Migrations should succeed with existing data")

	// Verify data still exists
	var email string
	err = pool.QueryRow(ctx, "SELECT email FROM users WHERE id = $1", "test_user_preserve").Scan(&email)
	require.NoError(t, err)
	assert.Equal(t, "preserve@test.com", email)
}
