package agents

// GetAgents handler tests, within this package's boundary: the
// handler over the real store. Seeding is handwritten SQL until the
// InsertAgent query lands with POST /api/agent. Route mounting is
// pinned in backend's tests.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// seedAgent inserts an agent row with an explicit creation timestamp.
func seedAgent(t *testing.T, sqlDB *sql.DB, id, name, icon string, createdAt int64) {
	t.Helper()
	_, err := sqlDB.Exec(`INSERT INTO agents (id, name, icon, created_at)
VALUES (?, ?, ?, ?)`, id, name, icon, createdAt)
	if err != nil {
		t.Fatalf("seed agent %s: %v", id, err)
	}
}

// getAgents runs the handler and returns status plus raw body — the
// byte-exact comparisons are the contract pin, catching any drift
// between the query's SELECT and the UI's Agent shape.
func getAgents(t *testing.T, queries *db.Queries) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	GetAgents(queries, zerolog.Nop())(rec,
		httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestGetAgentsEmpty(t *testing.T) {
	queries := db.New(testdb.Open(t))
	// Only the seeded superagent exists and it is excluded; the list is
	// an array, never null.
	if code, body := getAgents(t, queries); code != http.StatusOK || body != `{"agents":[]}` {
		t.Errorf("get = %d %s, want 200 {\"agents\":[]}", code, body)
	}
}

func TestGetAgentsContract(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	// created_at order differs from insertion and id order, so the list
	// pins created_at ordering; the seeded superagent never appears.
	seedAgent(t, sqlDB, "scribe", "Scribe", "pencil", 2)
	seedAgent(t, sqlDB, "research", "Research", "globe", 1)

	want := `{"agents":[{"id":"research","name":"Research","icon":"globe"},{"id":"scribe","name":"Scribe","icon":"pencil"}]}`
	if _, body := getAgents(t, queries); body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

func TestGetAgentsStoreFailure(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	// Break the store under the handler — the endpoint's only failure
	// mode.
	if _, err := sqlDB.Exec(`DROP TABLE agents`); err != nil {
		t.Fatalf("drop agents: %v", err)
	}

	code, body := getAgents(t, queries)
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
	// The error shape is contract: ky surfaces {message} verbatim.
	if body != `{"message":"Internal server error"}` {
		t.Errorf("body = %s, want {\"message\":\"Internal server error\"}", body)
	}
}
