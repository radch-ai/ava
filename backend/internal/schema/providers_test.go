package schema_test

// Model-catalog DB-enforced rules, via raw SQL.

import (
	"database/sql"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// The catalog is user configuration, not seed data; the fake
// starter catalog exists only in the UI mocks.
func TestCatalogIsUnseeded(t *testing.T) {
	sqlDB := testdb.Open(t)
	for _, table := range []string{"providers", "models"} {
		var n int
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM ` + table,
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("seeded %s = %d, want 0 (catalog is user configuration)", table, n)
		}
	}
}

// seedCatalog opens a store with one scratch provider and model.
func seedCatalog(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB := testdb.Open(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p1', 'P1', 'https://p1.dev/v1')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO models (model_id, provider_id) VALUES ('p1/m1', 'p1')`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	return sqlDB
}

// UNIQUE (model_id): a second provider's claim fails.
func TestModelIdOwnedByOneProvider(t *testing.T) {
	sqlDB := seedCatalog(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p2', 'P2', 'https://p2.dev/v1')`,
	); err != nil {
		t.Fatalf("seed second provider: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO models (model_id, provider_id) VALUES ('p1/m1', 'p2')`,
	); err == nil {
		t.Fatal("duplicate model id under second provider succeeded, want UNIQUE constraint failure")
	}
}

// Deleting a provider takes its models with it (ON DELETE CASCADE).
func TestProviderDeleteCascadesModels(t *testing.T) {
	sqlDB := seedCatalog(t)
	if _, err := sqlDB.Exec(`DELETE FROM providers WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM models WHERE provider_id = 'p1'`,
	).Scan(&n); err != nil {
		t.Fatalf("count models: %v", err)
	}
	if n != 0 {
		t.Errorf("provider models after delete = %d, want 0 (cascade)", n)
	}
}

// A dropped model hands referencing heartbeat/cron rows back to
// NULL routing (ON DELETE SET NULL).
func TestProviderDeleteNullsModelReferences(t *testing.T) {
	sqlDB := seedCatalog(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('ref-probe', 'Probe', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, enabled, duration, content)
		 VALUES ('ref-probe', 'p1/m1', 1, '30m', 'x')`); err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM providers WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	var model any
	if err := sqlDB.QueryRow(
		`SELECT model FROM heartbeat WHERE agent_id = 'ref-probe'`,
	).Scan(&model); err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if model != nil {
		t.Errorf("heartbeat model after provider delete = %v, want NULL (default routing)", model)
	}
}
