package providers

// DeleteProvider endpoint tests; DB rules live in internal/schema.
import (
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestDeleteProvider(t *testing.T) {
	sqlDB := seedCatalog(t)
	rec := do(t, sqlDB, http.MethodDelete, "/api/providers/grok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"message":"Provider deleted"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	// Gone from the catalog: no grok provider, no grok models.
	rec = do(t, sqlDB, http.MethodGet, "/api/providers", "")
	if contains(body(rec), `"grok"`) || contains(body(rec), `"grok/grok-1"`) {
		t.Error("grok still listed after delete")
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

// A referenced model does not block the delete.
func TestDeleteProviderWithReferencedModel(t *testing.T) {
	sqlDB := seedCatalog(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('d1', 'Del', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO cron_jobs (id, agent_id, time, frequency, model, prompt)
		 VALUES ('j1', 'd1', '09:00', 'daily', 'gemini/gemini-pro', 'digest')`); err != nil {
		t.Fatalf("insert cron job: %v", err)
	}

	rec := do(t, sqlDB, http.MethodDelete, "/api/providers/gemini", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var model any
	if err := sqlDB.QueryRow(
		`SELECT model FROM cron_jobs WHERE id = 'j1'`,
	).Scan(&model); err != nil {
		t.Fatalf("read cron job: %v", err)
	}
	if model != nil {
		t.Errorf("cron job model after provider delete = %v, want NULL (default routing)", model)
	}
}

// contains: substring probe for the gone-from-list assertion.
func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
