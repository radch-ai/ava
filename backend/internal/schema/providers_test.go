package schema_test

// Provider/model catalog DB-enforced rules, via raw SQL. Custom
// models are a JSON array on the provider row; system models are
// never DB state.

import (
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// The catalog is user configuration, not seed data.
func TestCatalogIsUnseeded(t *testing.T) {
	sqlDB := testdb.Open(t)
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&n); err != nil {
		t.Fatalf("count providers: %v", err)
	}
	if n != 0 {
		t.Errorf("seeded providers = %d, want 0 (catalog is user configuration)", n)
	}
}

// There is no models table: system models are fetched, custom models
// live on the provider row. Any 'models' table is a leftover.
func TestNoModelsTable(t *testing.T) {
	sqlDB := testdb.Open(t)
	var name string
	err := sqlDB.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'models'`,
	).Scan(&name)
	if err == nil {
		t.Fatal("models table exists, want it gone (custom models are a provider column)")
	}
}

// A provider row without models defaults to an empty JSON array.
func TestProviderModelsDefaultsEmpty(t *testing.T) {
	sqlDB := testdb.Open(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p1', 'P1', 'https://p1.dev/v1')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	var models string
	if err := sqlDB.QueryRow(
		`SELECT models FROM providers WHERE id = 'p1'`,
	).Scan(&models); err != nil {
		t.Fatalf("read models: %v", err)
	}
	if models != "[]" {
		t.Errorf("models default = %q, want []", models)
	}
}

// heartbeat.model / cron_jobs.model are plain strings: an id absent
// from every catalog persists (staleness is caught at call time, not
// by the schema).
func TestModelColumnsAcceptUncatalogedIds(t *testing.T) {
	sqlDB := testdb.Open(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('a1', 'A1', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, enabled, duration, content)
		 VALUES ('a1', 'ghost/m1', 1, '30m', 'x')`); err != nil {
		t.Fatalf("seed heartbeat with uncataloged model: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO cron_jobs (id, agent_id, time, frequency, model, prompt)
		 VALUES ('j1', 'a1', '09:00', 'daily', 'ghost/m2', 'x')`); err != nil {
		t.Fatalf("seed cron job with uncataloged model: %v", err)
	}

	// Deleting the provider leaves the stored strings as-is; no
	// SET NULL sweep exists.
	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p1', 'P1', 'https://p1.dev/v1')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM providers WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	var m string
	if err := sqlDB.QueryRow(
		`SELECT model FROM heartbeat WHERE agent_id = 'a1'`,
	).Scan(&m); err != nil {
		t.Fatalf("read heartbeat model: %v", err)
	}
	if m != "ghost/m1" {
		t.Errorf("heartbeat model after provider delete = %q, want ghost/m1 (stored verbatim)", m)
	}
}
