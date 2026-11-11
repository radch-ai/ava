// Package testdb opens migrated stores for tests — the one piece of
// bootstrap every store-backed test needs. Tests get their databases
// from Open only; sqlstore's openers are production paths, not test
// fixtures. The package holds no tests and crosses no package
// boundary: each test builds its own queries and handlers from the
// store it gets.
package testdb

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go)

	"github.com/radch-ai/ava/backend/internal/schema"
)

// fileDirs hands each test one temp directory, so ephemeral=false
// opens in that test address the same file.
var fileDirs sync.Map // *testing.T → dir

// Open returns a migrated store, closed on test cleanup.
//
// ephemeral picks the store's lifetime. true is a fresh in-memory
// database, capped to one connection — each pooled connection would
// otherwise get its own empty memory database. false is a file under
// this test's temp directory: every call in a test addresses the same
// file, so close/reopen keeps the data and the file disappears with
// the test. The cap is a memory constraint; it stays off for files.
func Open(t *testing.T, ephemeral bool) *sql.DB {
	t.Helper()
	dsn := ":memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	if !ephemeral {
		dir, ok := fileDirs.Load(t)
		if !ok {
			dir = t.TempDir()
			fileDirs.Store(t, dir)
			t.Cleanup(func() { fileDirs.Delete(t) })
		}
		dsn = filepath.Join(dir.(string), "ava.db") +
			"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
			"&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	}
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if ephemeral {
		sqlDB.SetMaxOpenConns(1)
	}

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping sqlite: %v", err)
	}
	if err := schema.MigrateSQLite(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}
