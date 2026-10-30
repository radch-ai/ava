// Package schema holds ava's SQL migrations (schema/migrations, golang-migrate
// up/down file layout) and the small runner that applies the pending ".up"
// steps in order, recording each in a version table. Down files exist for
// layout parity and manual rollbacks; the runner is forward-only.
package schema

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

//go:embed all:migrations
var migrationsFS embed.FS

// Migrate applies every pending migration in filename order and records it.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT NOT NULL PRIMARY KEY,
		applied_at BIGINT NOT NULL -- micros since epoch
	)`); err != nil {
		return err
	}

	upFiles, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	if err != nil {
		return err
	}

	for _, file := range upFiles {
		name := strings.TrimSuffix(strings.TrimPrefix(file, "migrations/"), ".up.sql")

		var applied bool
		if err := db.QueryRow(
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = ?)`, name,
		).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}

		body, err := migrationsFS.ReadFile(file)
		if err != nil {
			return err
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
			name, time.Now().UnixMicro(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
