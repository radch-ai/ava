package schema_test

// Down-migration integrity: the full down stack must run on a database
// that holds data (agents, threads, and their circular FK), proving the
// DROP order respects dependencies; the up stack must then rebuild.

import (
	"database/sql"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func migrateInstance(t *testing.T, db *sql.DB) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(os.DirFS("migrations"), ".")
	if err != nil {
		t.Fatalf("migration source: %v", err)
	}
	drv, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		t.Fatalf("migration driver: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	return m
}

func TestDownMigratesPopulatedDatabaseAndUpRebuilds(t *testing.T) {
	db := testdb.Open(t, false)
	m := migrateInstance(t, db)

	// Seed the circular-FK pair with data: an agent whose active pointer
	// names a thread, plus a second thread chaining to the first.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := []string{
		// Agent first: its active-thread FK is deferred, satisfied when
		// the thread lands in the same transaction.
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity,
			operating_instructions, tools_context, active_thread_id)
			VALUES ('a1', 'A', 'x', 's', 's', 'o', 't', 't1')`,
		`INSERT INTO threads (id, agent_id, created_at) VALUES ('t1', 'a1', 0)`,
		`INSERT INTO threads (id, agent_id, previous_thread_id, created_at)
			VALUES ('t2', 'a1', 't1', 0)`,
		`UPDATE agents SET active_thread_id = 't2' WHERE id = 'a1'`,
	}
	for _, q := range seed {
		if _, err := tx.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	if err := m.Down(); err != nil {
		t.Fatalf("down with data: %v", err)
	}
	for _, table := range []string{"agents", "threads", "tools", "mcp_servers"} {
		if _, err := db.Exec(`SELECT 1 FROM ` + table); err == nil {
			t.Fatalf("table %s survived down", table)
		}
	}

	if err := m.Up(); err != nil {
		t.Fatalf("up after down: %v", err)
	}
	// The rebuilt schema accepts the same circular-FK seed again.
	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("begin reseed: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity,
			operating_instructions, tools_context, active_thread_id)
			VALUES ('a1', 'A', 'x', 's', 's', 'o', 't', 't1')`,
		`INSERT INTO threads (id, agent_id, created_at) VALUES ('t1', 'a1', 0)`,
	} {
		if _, err := tx2.Exec(q); err != nil {
			t.Fatalf("reseed %q: %v", q, err)
		}
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit reseed: %v", err)
	}
}
