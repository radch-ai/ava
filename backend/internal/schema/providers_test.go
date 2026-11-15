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
	sqlDB := testdb.Open(t, true)
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
	sqlDB := testdb.Open(t, true)
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
	sqlDB := testdb.Open(t, true)
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

// The model pair's owner is a real reference: deleting a provider
// nulls provider_id on every row pinned to it (the row falls back
// to default routing), while the id string itself is stored
// verbatim — uncataloged ids persist, staleness caught at call time.
func TestDeleteProviderNullsModelOwner(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('a1', 'A1', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p1', 'P1', 'https://p1.dev/v1')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, provider_id, enabled, duration, content)
		 VALUES ('a1', 'p1/m1', 'p1', 1, '30m', 'x')`); err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO cron_jobs (id, agent_id, time, frequency, model, provider_id, prompt)
		 VALUES ('j1', 'a1', '09:00', 'daily', 'p1/m2', 'p1', 'x')`); err != nil {
		t.Fatalf("seed cron job: %v", err)
	}

	if _, err := sqlDB.Exec(`DELETE FROM providers WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	for _, row := range []struct{ query, what string }{
		{`SELECT model, provider_id FROM heartbeat WHERE agent_id = 'a1'`, "heartbeat"},
		{`SELECT model, provider_id FROM cron_jobs WHERE id = 'j1'`, "cron job"},
	} {
		var model, owner any
		if err := sqlDB.QueryRow(row.query).Scan(&model, &owner); err != nil {
			t.Fatalf("read %s: %v", row.what, err)
		}
		if owner != nil {
			t.Errorf("%s owner after provider delete = %v, want NULL (default routing)", row.what, owner)
		}
	}
}

// provider_id must point at a real provider row: the pair's owner
// is DB-enforced, unlike the free-form id string.
func TestModelOwnerMustReferenceProvider(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('a1', 'A1', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, provider_id, enabled, duration, content)
		 VALUES ('a1', 'ghost/m1', 'missing', 1, '30m', 'x')`,
	); err == nil {
		t.Fatal("heartbeat insert with unknown provider_id succeeded, want FK rejection")
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, provider_id, enabled, duration, content)
		 VALUES ('a1', 'ghost/m1', NULL, 1, '30m', 'x')`,
	); err != nil {
		t.Fatalf("heartbeat insert with uncataloged model and no owner: %v", err)
	}
}
