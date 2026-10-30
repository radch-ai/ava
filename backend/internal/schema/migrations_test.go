package schema

// Migration runner mechanics only: version table, idempotence.

import (
	"path/filepath"
	"testing"
)

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
