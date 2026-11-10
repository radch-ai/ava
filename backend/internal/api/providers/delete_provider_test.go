package providers

// DeleteProvider endpoint tests: user rows go away, system-provider
// attachments revert to keyless, stored model strings survive.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// A row is gone after delete, its sealed key entry with it.
func TestDeleteProviderUserRow(t *testing.T) {
	sqlDB := testdb.Open(t)
	created := do(t, sqlDB, http.MethodPost, "/api/providers",
		`{"name":"Mistral","baseUrl":"`+noAuthURL(t)+`","apiKey":"k-1"}`)
	id := createdID(t, created)

	rec := do(t, sqlDB, http.MethodDelete, "/api/providers/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := body(rec); got != `{"message":"Provider deleted"}` {
		t.Errorf("body = %s, want delete message", got)
	}

	rec = do(t, sqlDB, http.MethodGet, "/api/providers", "")
	if strings.Contains(body(rec), `"Mistral"`) {
		t.Error("deleted provider still listed")
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM vault_entries`).Scan(&n); err != nil {
		t.Fatalf("count vault entries: %v", err)
	}
	if n != 0 {
		t.Errorf("vault entries after provider delete = %d, want 0", n)
	}
}

func TestDeleteProviderNotFound(t *testing.T) {
	sqlDB := testdb.Open(t)
	rec := do(t, sqlDB, http.MethodDelete, "/api/providers/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	want := `{"message":"Provider \"nope\" not found"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// A stored model string survives its provider's delete — staleness
// is caught at call time, not by a SET NULL sweep.
func TestDeleteProviderLeavesStoredModelStrings(t *testing.T) {
	sqlDB := testdb.Open(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('d1', 'Del', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO cron_jobs (id, agent_id, time, frequency, model, prompt)
		 VALUES ('j1', 'd1', '09:00', 'daily', 'mistral/m1', 'digest')`,
	); err != nil {
		t.Fatalf("insert cron job: %v", err)
	}
	created := do(t, sqlDB, http.MethodPost, "/api/providers",
		`{"name":"Mistral","baseUrl":"`+noAuthURL(t)+`"}`)
	id := createdID(t, created)
	if rec := do(t, sqlDB, http.MethodPatch, "/api/providers/"+id, `{"models":["mistral/m1"]}`); rec.Code != http.StatusOK {
		t.Fatalf("seed models: %d", rec.Code)
	}

	if rec := do(t, sqlDB, http.MethodDelete, "/api/providers/"+id, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	var model any
	if err := sqlDB.QueryRow(
		`SELECT model FROM cron_jobs WHERE id = 'j1'`,
	).Scan(&model); err != nil {
		t.Fatalf("read cron job: %v", err)
	}
	if model != "mistral/m1" {
		t.Errorf("cron job model after provider delete = %v, want mistral/m1 (stored verbatim)", model)
	}
}
