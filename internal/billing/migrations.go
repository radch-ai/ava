// Package billing provides database migrations for billing and usage tracking.
package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration represents a single database migration.
type Migration struct {
	Name string
	SQL  string
}

// migrations is the ordered list of migrations to run.
// Each migration is idempotent and can be run multiple times safely.
var migrations = []Migration{
	{
		Name: "create_users_table",
		SQL: `
			CREATE TABLE IF NOT EXISTS users (
				id TEXT PRIMARY KEY,
				email TEXT,
				tier INTEGER NOT NULL DEFAULT 0,
				credits_remaining NUMERIC(10,2) NOT NULL DEFAULT 0,
				subscription_tier TEXT DEFAULT 'free',
				subscription_status TEXT DEFAULT 'active',
				billing_period_start INTEGER,
				billing_period_end INTEGER,
				revenuecat_customer_id TEXT,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				CHECK (tier IN (0, 1, 2))
			)
		`,
	},
	{
		Name: "create_usage_logs_table",
		SQL: `
			CREATE TABLE IF NOT EXISTS usage_logs (
				id BIGSERIAL PRIMARY KEY,
				user_id TEXT NOT NULL,
				agent_id TEXT NOT NULL,
				tokens_used INTEGER NOT NULL,
				credit_cost NUMERIC(10,4) NOT NULL,
				timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`,
	},
	{
		Name: "create_processed_webhooks_table",
		SQL: `
			CREATE TABLE IF NOT EXISTS processed_webhooks (
				event_id TEXT PRIMARY KEY,
				processed_at INTEGER NOT NULL,
				event_type TEXT NOT NULL
			)
		`,
	},
	{
		Name: "create_users_indexes",
		SQL: `
			CREATE INDEX IF NOT EXISTS idx_users_tier ON users(tier);
			CREATE INDEX IF NOT EXISTS idx_users_updated_at ON users(updated_at);
			CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
			CREATE INDEX IF NOT EXISTS idx_users_revenuecat_customer_id ON users(revenuecat_customer_id);
		`,
	},
	{
		Name: "create_usage_logs_indexes",
		SQL: `
			CREATE INDEX IF NOT EXISTS idx_usage_logs_user_id ON usage_logs(user_id);
			CREATE INDEX IF NOT EXISTS idx_usage_logs_agent_id ON usage_logs(agent_id);
			CREATE INDEX IF NOT EXISTS idx_usage_logs_timestamp ON usage_logs(timestamp);
			CREATE INDEX IF NOT EXISTS idx_usage_logs_user_timestamp ON usage_logs(user_id, timestamp DESC);
		`,
	},
	{
		Name: "create_processed_webhooks_indexes",
		SQL: `
			CREATE INDEX IF NOT EXISTS idx_processed_webhooks_processed_at ON processed_webhooks(processed_at);
			CREATE INDEX IF NOT EXISTS idx_processed_webhooks_event_type ON processed_webhooks(event_type);
		`,
	},
	{
		Name: "create_usage_logs_foreign_key",
		SQL: `
			ALTER TABLE usage_logs
			DROP CONSTRAINT IF EXISTS usage_logs_user_id_fkey;

			ALTER TABLE usage_logs
			ADD CONSTRAINT usage_logs_user_id_fkey
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		`,
	},
	{
		Name: "create_agents_table",
		SQL: `
			CREATE TABLE IF NOT EXISTS agents (
				agent_id     TEXT PRIMARY KEY,
				user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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
			)
		`,
	},
	{
		Name: "create_agents_indexes",
		SQL: `
			CREATE INDEX IF NOT EXISTS idx_agents_user_id ON agents(user_id);
			CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at);
		`,
	},
}

// RunMigrations runs all database migrations for the billing package.
// It uses a connection pool for efficient database access and handles
// context cancellation appropriately. Migrations are run in a transaction
// to ensure atomicity.
func RunMigrations(ctx context.Context, dbURL string) error {
	if dbURL == "" {
		return fmt.Errorf("database URL is required")
	}

	// Parse connection string and configure pool
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return fmt.Errorf("parse connection string: %w", err)
	}

	// Configure pool settings for migrations
	config.MaxConns = 5 // Migrations don't need many connections
	config.MinConns = 1

	// Create connection pool with context timeout
	ctxTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctxTimeout, config)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	// Verify connection
	if err := pool.Ping(ctxTimeout); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// Run each migration
	for _, migration := range migrations {
		if err := runMigration(ctx, pool, migration); err != nil {
			return fmt.Errorf("migration %q failed: %w", migration.Name, err)
		}
	}

	return nil
}

// runMigration executes a single migration within a transaction.
// If the migration fails, the transaction is rolled back.
func runMigration(ctx context.Context, pool *pgxpool.Pool, migration Migration) error {
	// Begin transaction for atomic migration execution
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Ensure transaction is rolled back if we return early
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Execute migration SQL
	// Using Exec for schema changes that don't return rows
	_, err = tx.Exec(ctx, migration.SQL)
	if err != nil {
		return fmt.Errorf("execute migration: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// MigrationResult represents the result of a migration check.
type MigrationResult struct {
	MigrationName string
	AlreadyExists bool
	Error         error
}

// CheckMigrations checks if all migrations have been applied without running them.
// Useful for validation and testing purposes.
func CheckMigrations(ctx context.Context, dbURL string) ([]MigrationResult, error) {
	if dbURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}

	config.MaxConns = 2

	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctxTimeout, config)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	results := make([]MigrationResult, len(migrations))

	for i, migration := range migrations {
		results[i] = MigrationResult{
			MigrationName: migration.Name,
		}

		// Check if tables exist by querying information schema
		var exists bool
		checkSQL := checkExistenceSQL(migration)

		row := pool.QueryRow(ctx, checkSQL)
		err := row.Scan(&exists)
		if err != nil && err != pgx.ErrNoRows {
			results[i].Error = fmt.Errorf("check migration: %w", err)
		} else {
			results[i].AlreadyExists = exists
		}
	}

	return results, nil
}

// checkExistenceSQL returns SQL to check if a migration has been applied.
func checkExistenceSQL(migration Migration) string {
	switch migration.Name {
	case "create_users_table":
		return `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'users')`
	case "create_usage_logs_table":
		return `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'usage_logs')`
	case "create_processed_webhooks_table":
		return `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'processed_webhooks')`
	case "create_users_indexes":
		return `SELECT EXISTS (SELECT FROM pg_indexes WHERE indexname = 'idx_users_tier')`
	case "create_usage_logs_indexes":
		return `SELECT EXISTS (SELECT FROM pg_indexes WHERE indexname = 'idx_usage_logs_user_id')`
	case "create_processed_webhooks_indexes":
		return `SELECT EXISTS (SELECT FROM pg_indexes WHERE indexname = 'idx_processed_webhooks_processed_at')`
	case "create_usage_logs_foreign_key":
		return `SELECT EXISTS (SELECT FROM information_schema.table_constraints WHERE constraint_name = 'usage_logs_user_id_fkey')`
	case "create_agents_table":
		return `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'agents')`
	case "create_agents_indexes":
		return `SELECT EXISTS (SELECT FROM pg_indexes WHERE indexname = 'idx_agents_user_id')`
	default:
		return "SELECT false"
	}
}

// RunMigrationsWithPool runs migrations using an existing connection pool.
// Useful when the application already has a database connection established.
func RunMigrationsWithPool(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("connection pool is required")
	}

	for _, migration := range migrations {
		if err := runMigration(ctx, pool, migration); err != nil {
			return fmt.Errorf("migration %q failed: %w", migration.Name, err)
		}
	}

	return nil
}
