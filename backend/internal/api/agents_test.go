package api

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

// insertAgent seeds an agent row with an explicit creation timestamp.
func insertAgent(t *testing.T, sqlDB *sql.DB, id, name, icon string, createdAt int64) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, created_at) VALUES (?, ?, ?, ?)`,
		id, name, icon, createdAt,
	); err != nil {
		t.Fatalf("insert agent %s: %v", id, err)
	}
}

// getAgents asserts a 200 and returns the raw body. Responses serve
// sqlc rows directly, so these byte-exact comparisons are the contract
// pin — they catch any drift between the query's SELECT and the UI's
// Agent shape.
func getAgents(t *testing.T, handler http.Handler) string {
	t.Helper()
	rec := do(t, handler, http.MethodGet, "/api/agents")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agents status = %d, want 200", rec.Code)
	}
	return strings.TrimSpace(rec.Body.String())
}

func TestGetAgentsEmpty(t *testing.T) {
	handler, _ := openHandler(t)
	// Only the seeded superagent exists and it is excluded; the list is
	// an array, never null.
	if got := getAgents(t, handler); got != `{"agents":[]}` {
		t.Errorf("body = %s, want {\"agents\":[]}", got)
	}
}

func TestGetAgentsContract(t *testing.T) {
	handler, sqlDB := openHandler(t)
	// Insertion order differs from creation order to pin created_at
	// ordering; the seeded superagent never appears.
	insertAgent(t, sqlDB, "scribe", "Scribe", "pencil", 2)
	insertAgent(t, sqlDB, "research", "Research", "globe", 1)

	want := `{"agents":[{"id":"research","name":"Research","icon":"globe"},{"id":"scribe","name":"Scribe","icon":"pencil"}]}`
	if got := getAgents(t, handler); got != want {
		t.Errorf("body =\n%s\nwant\n%s", got, want)
	}
}

func TestGetAgentsStoreFailure(t *testing.T) {
	handler, sqlDB := openHandler(t)
	// Break the store under the handler — the endpoint's only failure
	// mode.
	if _, err := sqlDB.Exec(`DROP TABLE agents`); err != nil {
		t.Fatalf("drop agents: %v", err)
	}

	rec := do(t, handler, http.MethodGet, "/api/agents")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	// The error shape is contract: ky surfaces {message} verbatim.
	want := `{"message":"Internal server error"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
