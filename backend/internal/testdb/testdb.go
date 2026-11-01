// Package testdb opens fresh, migrated in-memory stores for tests —
// the one piece of bootstrap every store-backed test needs. It holds
// no tests and crosses no package boundary: each test builds its own
// queries and handlers from the store it gets.
package testdb

import (
	"database/sql"
	"testing"

	"github.com/radch-ai/ava/backend/sqlstore"
)

// Open returns a fresh, migrated in-memory store, closed on test
// cleanup.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sqlstore.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("sqlstore.OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}
