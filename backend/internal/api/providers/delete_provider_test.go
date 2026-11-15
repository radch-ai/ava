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
	sqlDB := testdb.Open(t, true)
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Mistral","baseUrl":"`+noAuthURL(t)+`","apiKey":"k-1"}`)
	id := createdID(t, created)

	rec := do(t, sqlDB, http.MethodDelete, "/providers/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := body(rec); got != `{"message":"Provider deleted"}` {
		t.Errorf("body = %s, want delete message", got)
	}

	rec = do(t, sqlDB, http.MethodGet, "/providers", "")
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
	sqlDB := testdb.Open(t, true)
	rec := do(t, sqlDB, http.MethodDelete, "/providers/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	want := `{"message":"Provider \"nope\" not found"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// Deleting a provider is a SET NULL sweep on ownership: rows pinned
// to it lose their owner (default routing) while the id strings
// stay verbatim.
func TestDeleteProviderNullsModelOwner(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('d1', 'Del', 'globe', '', '', '', '')`,
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Mistral","baseUrl":"`+noAuthURL(t)+`"}`)
	id := createdID(t, created)
	if _, err := sqlDB.Exec(
		`INSERT INTO cron_jobs (id, agent_id, time, frequency, model, provider_id, prompt)
		 VALUES ('j1', 'd1', '09:00', 'daily', 'mistral/m1', '` + id + `', 'digest')`,
	); err != nil {
		t.Fatalf("insert cron job: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, provider_id, enabled, duration, content)
		 VALUES ('d1', 'mistral/m1', '` + id + `', 0, '1hr', 'beat')`,
	); err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}

	if rec := do(t, sqlDB, http.MethodDelete, "/providers/"+id, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	for _, row := range []struct{ query, what string }{
		{`SELECT model, provider_id FROM cron_jobs WHERE id = 'j1'`, "cron job"},
		{`SELECT model, provider_id FROM heartbeat WHERE agent_id = 'd1'`, "heartbeat"},
	} {
		var model any
		var owner any
		if err := sqlDB.QueryRow(row.query).Scan(&model, &owner); err != nil {
			t.Fatalf("read %s: %v", row.what, err)
		}
		if model != "mistral/m1" {
			t.Errorf("%s model after provider delete = %v, want mistral/m1 (stored verbatim)", row.what, model)
		}
		if owner != nil {
			t.Errorf("%s owner after provider delete = %v, want NULL (default routing)", row.what, owner)
		}
	}
}
