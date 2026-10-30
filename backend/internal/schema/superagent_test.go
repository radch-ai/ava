package schema

// Superagent SQL-level invariants: the seed guarantees and the
// DB-enforced rules from migrations/0002_seed_superagent. Everything
// else about the database is verified through the api endpoint tests —
// schema tests pin only what the SQL layer itself enforces.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedSuperagent(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "ava.db"))
	defer func() { _ = db.Close() }()

	var name, icon string
	if err := db.QueryRow(
		`SELECT name, icon FROM agents WHERE id = '_superagent'`,
	).Scan(&name, &icon); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if name != "Ava" || icon != "message-square" {
		t.Errorf("seed row = %q/%q, want Ava/message-square", name, icon)
	}
}

func TestSystemAgentsCannotBeDeleted(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "ava.db"))
	defer func() { _ = db.Close() }()

	// The trigger aborts deletes of "_"-prefixed ids — every code
	// path, not just a future endpoint's.
	_, err := db.Exec(`DELETE FROM agents WHERE id = '_superagent'`)
	if err == nil || !strings.Contains(err.Error(), "system agents cannot be deleted") {
		t.Fatalf("delete _superagent err = %v, want trigger abort", err)
	}

	// Non-system rows delete normally.
	if _, err := db.Exec(
		`INSERT INTO agents (id, name, icon, created_at)
		 VALUES ('research', 'Research', 'globe', 1)`); err != nil {
		t.Fatalf("insert research: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM agents WHERE id = 'research'`); err != nil {
		t.Fatalf("delete research: %v", err)
	}
}
