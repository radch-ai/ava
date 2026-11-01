// Package testdb opens fresh, migrated in-memory stores for tests —
// the one piece of bootstrap every store-backed test needs. It holds
// no tests and crosses no package boundary: each test builds its own
// queries and handlers from the store it gets.
package testdb

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go)

	"github.com/radch-ai/ava/backend/internal/schema"
)

// Open returns a fresh, migrated in-memory store, closed on test
// cleanup. The setup mirrors sqlstore.OpenSQLite with only the parts
// that mean the same in memory: WAL and synchronous are no-ops there
// and there is no data directory.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	dsn := ":memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// One connection: every extra pooled connection would get its own
	// private, empty memory database.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping sqlite: %v", err)
	}
	if err := schema.MigrateSQLite(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}
