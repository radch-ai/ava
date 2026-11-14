package schema_test

// Default-model invariants: one row at most, and it goes when its
// provider goes. Raw SQL: the schema rules are under test.

import (
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestDefaultModelSingleton(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p1', 'P1', 'https://p1.dev/v1')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO default_model (id, model, provider_id) VALUES (1, 'glm-4.7', 'p1')`,
	); err != nil {
		t.Fatalf("set default model: %v", err)
	}

	// id is pinned to 1: a second default — re-inserting row 1 or
	// smuggling in another id — cannot exist. Assert the outcome, not
	// the driver's error wording.
	for _, stmt := range []string{
		`INSERT INTO default_model (id, model, provider_id) VALUES (1, 'other', 'p1')`,
		`INSERT INTO default_model (id, model, provider_id) VALUES (2, 'other', 'p1')`,
	} {
		if _, err := sqlDB.Exec(stmt); err == nil {
			t.Errorf("insert %q succeeded, want constraint failure", stmt)
		}
	}
	var model, providerID string
	if err := sqlDB.QueryRow(
		`SELECT model, provider_id FROM default_model WHERE id = 1`,
	).Scan(&model, &providerID); err != nil {
		t.Fatalf("read default model: %v", err)
	}
	if model != "glm-4.7" || providerID != "p1" {
		t.Errorf("default model = (%q, %q), want (glm-4.7, p1) — the first write stands", model, providerID)
	}
}

func TestDefaultModelProviderDeleteCascades(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p1', 'P1', 'https://p1.dev/v1')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO default_model (id, model, provider_id) VALUES (1, 'glm-4.7', 'p1')`,
	); err != nil {
		t.Fatalf("set default model: %v", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM providers WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete provider: %v", err)
	}

	// The cascade removes the default with its provider — routing
	// falls back to "no default", never a dangling pair.
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM default_model`).Scan(&n); err != nil {
		t.Fatalf("count default_model: %v", err)
	}
	if n != 0 {
		t.Errorf("default_model rows after provider delete = %d, want 0 (cascade)", n)
	}
}
