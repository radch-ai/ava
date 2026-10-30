// Package schema holds the SQL migrations (schema/migrations,
// golang-migrate up/down file layout) and applies the pending up steps at
// startup through the golang-migrate library over the embedded files. Same
// engine and schema_migrations version table as the migrate CLI (just
// migrate-cli), so startup migrations and manual CLI rollbacks
// interoperate on one database.
package schema

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed all:migrations
var migrationsFS embed.FS

// Migrate applies every pending migration in version order using the
// golang-migrate engine. The engine wraps each step in a transaction and
// records it in schema_migrations (version, dirty).
func Migrate(db *sql.DB) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}

	drv, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
