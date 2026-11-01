package schema_test

// Migration runner mechanics only: version table, idempotence.

import (
	"path/filepath"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/sqlstore"
)

func TestMigrateCreatesVersionTable(t *testing.T) {
	db := testdb.Open(t)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("version table: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	// File-based: the reopen must land on the same database — memory
	// would hand the second open a fresh one.
	path := filepath.Join(t.TempDir(), "ava.db")

	db, err := sqlstore.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_ = db.Close()

	// Second open + migrate on the same file must succeed.
	db2, err := sqlstore.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = db2.Close() }()
}
