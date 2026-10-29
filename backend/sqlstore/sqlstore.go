// Package sqlstore opens the backend's SQLite database — data directory,
// pragmas, the single-writer connection — and applies schema migrations.
// Query methods live in the sqlc-generated internal/db.
package sqlstore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/glebarez/go-sqlite" // registers the "sqlite" driver (pure Go)

	"github.com/radch-ai/ava/backend/internal/schema"
)

// Open opens (creating if needed) the SQLite database at dbPath and applies
// pending migrations. The ":memory:" sentinel opens an in-process database
// instead — WAL and synchronous are no-ops there, and it depends on the
// single pooled connection below: every extra connection would get its own
// private, empty memory database.
func Open(dbPath string) (*sql.DB, error) {
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}

	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows a single writer; one connection sidesteps write contention.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := schema.Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return sqlDB, nil
}
