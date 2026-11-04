package sqlstore

import (
	"path/filepath"
	"testing"
)

func TestOpenSQLiteCreatesFileAndVersionTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "ava.db") // exercises dir creation

	sqlDB, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	var table string
	if err := sqlDB.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`,
	).Scan(&table); err != nil {
		t.Fatalf("version table after open: %v", err)
	}
	if table != "schema_migrations" {
		t.Fatalf("version table = %q, want schema_migrations", table)
	}
}

func TestOpenSQLiteMemory(t *testing.T) {
	sqlDB, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	var table string
	if err := sqlDB.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`,
	).Scan(&table); err != nil {
		t.Fatalf("version table in memory db: %v", err)
	}
	if table != "schema_migrations" {
		t.Fatalf("version table = %q, want schema_migrations", table)
	}
}
