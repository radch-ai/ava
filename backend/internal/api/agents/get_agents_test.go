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
	"time"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// seedAgent inserts a row; insertion order is creation order.
func seedAgent(t *testing.T, sqlDB *sql.DB, id, name, icon string) {
	t.Helper()
	_, err := sqlDB.Exec(`INSERT INTO agents (id, name, icon) VALUES (?, ?, ?)`,
		id, name, icon)
	if err != nil {
		t.Fatalf("seed agent %s: %v", id, err)
	}
}

// getAgents runs the handler and returns status, raw body, and the
// handler's error — byte-exact body comparisons are the contract pin,
// the error asserts the server-fault path returns for api.Log.
func getAgents(t *testing.T, queries *db.Queries) (int, string, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	err := GetAgents(queries)(rec,
		httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	return rec.Code, strings.TrimSpace(rec.Body.String()), err
}

func TestGetAgentsEmpty(t *testing.T) {
	queries := db.New(testdb.Open(t))
	// Only the seeded superagent exists and it is excluded; the list is
	// an array, never null.
	code, body, err := getAgents(t, queries)
	if err != nil || code != http.StatusOK || body != `{"agents":[]}` {
		t.Errorf("get = %d %s (%v), want 200 {\"agents\":[]}", code, body, err)
	}
}

func TestGetAgentsContract(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	// "research" sorts before "scribe", but scribe was created first:
	// listing scribe first pins created_at ordering, not id ordering.
	// Stamps are coarse, so the seeds sit 2ms apart.
	seedAgent(t, sqlDB, "scribe", "Scribe", "pencil")
	time.Sleep(2 * time.Millisecond)
	seedAgent(t, sqlDB, "research", "Research", "globe")

	want := `{"agents":[{"id":"scribe","name":"Scribe","icon":"pencil"},{"id":"research","name":"Research","icon":"globe"}]}`
	if _, body, err := getAgents(t, queries); err != nil || body != want {
		t.Errorf("body =\n%s (%v)\nwant\n%s", body, err, want)
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

	code, body, err := getAgents(t, queries)
	// The cause returns for api.Log; the response stays contract-shaped.
	if err == nil {
		t.Fatal("store failure: want error returned for logging")
	}
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
	// The error shape is contract: ky surfaces {message} verbatim.
	if body != `{"message":"Internal server error"}` {
		t.Errorf("body = %s, want {\"message\":\"Internal server error\"}", body)
	}
}
