package schema

// Shared test infrastructure for this package's SQL-level tests: open
// returns a fully migrated database and is used by the runner-mechanics
// tests in migrations_test.go and the SQL-invariant tests (e.g.
// superagent_test.go).

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func open(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
