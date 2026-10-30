package schema

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
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

func TestMigrateCreatesVersionTable(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "ava.db"))
	defer func() { _ = db.Close() }()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("version table: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ava.db")

	db := open(t, path)
	_ = db.Close()

	db2 := open(t, path) // second open + migrate on the same file must succeed
	defer func() { _ = db2.Close() }()
}
