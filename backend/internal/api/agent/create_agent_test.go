package agent

// CreateAgent handler tests: the handler over the real store.
// Post-create state is read back through the store layer; route
// mounting is pinned in backend's tests.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// create runs the handler with a JSON body and returns the recorder
// plus the handler's error — nil on handled client errors, non-nil on
// server faults, for api.Log.
func create(t *testing.T, queries *db.Queries, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	err := CreateAgent(queries)(rec,
		httptest.NewRequest(http.MethodPost, "/api/agent", strings.NewReader(body)))
	return rec, err
}

func TestCreateAgent(t *testing.T) {
	queries := db.New(testdb.Open(t))
	rec, err := create(t, queries, `{"name":"Research Bot","icon":"globe","config":{"soul":{"personality":"curious"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	// The 201 body is {id, name}; the id is the derived one.
	want := `{"id":"research-bot","name":"Research Bot"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestCreateAgentConfigStoredVerbatim(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	if rec, err := create(t, queries,
		`{"name":"Research Bot","icon":"globe","config":{"soul":{"personality":"curious"},"unknownField":"kept"}}`,
	); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	// No query exposes config yet, so read the row directly. Parsed,
	// never byte-compared — storage bytes are the column's business.
	var config string
	if err := sqlDB.QueryRow(
		`SELECT config FROM agents WHERE id = 'research-bot'`,
	).Scan(&config); err != nil {
		t.Fatalf("read config: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(config), &doc); err != nil {
		t.Fatalf("stored config JSON: %v", err)
	}
	if _, ok := doc["soul"]; !ok {
		t.Errorf("stored config lost soul")
	}
	// The document passes through verbatim — fields Go doesn't model
	// survive.
	if _, ok := doc["unknownField"]; !ok {
		t.Errorf("stored config lost unknownField")
	}
}

func TestCreateAgentInvalidName(t *testing.T) {
	queries := db.New(testdb.Open(t))
	for _, name := range []string{
		"",      // empty
		"A  B",  // double space
		"Bob-1", // hyphen — letters, digits and single spaces only
		"_x",    // underscore (would collide with the system prefix)
		"Bob!",  // punctuation
	} {
		rec, err := create(t, queries, `{"name":"`+name+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("create %q status = %d, want 400", name, rec.Code)
		}
		// Message is contract — the UI surfaces it verbatim.
		want := `{"message":"Invalid agent name: use letters, digits and single spaces only"}`
		if got := strings.TrimSpace(rec.Body.String()); got != want {
			t.Errorf("create %q body = %s, want %s", name, got, want)
		}
	}
}

func TestCreateAgentDuplicateName(t *testing.T) {
	queries := db.New(testdb.Open(t))
	if rec, err := create(t, queries, `{"name":"Research","config":{}}`); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201", rec.Code)
	}

	rec, err := create(t, queries, `{"name":"Research","config":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	want := `{"message":"An agent named \"Research\" already exists"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	// Derivation lowercases, so differently-cased names collide on the
	// same id — and the 409 carries the name as sent.
	rec, err = create(t, queries, `{"name":"RESEARCH","config":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("RESEARCH status = %d, want 409", rec.Code)
	}
	want = `{"message":"An agent named \"RESEARCH\" already exists"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestCreateAgentMalformedBody(t *testing.T) {
	queries := db.New(testdb.Open(t))
	// Malformed JSON never reaches the mock's handler (its
	// request.json() throws); the no-config 400 is shared contract —
	// the mock answers the same message.
	for _, body := range []string{"", "not json", `{"name":"Valid Name","icon":"bot"}`} {
		rec, err := create(t, queries, body)
		if err != nil {
			t.Errorf("POST %q err = %v, want handled client error", body, err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %q status = %d, want 400", body, rec.Code)
		}
		want := `{"message":"Invalid request body"}`
		if got := strings.TrimSpace(rec.Body.String()); got != want {
			t.Errorf("POST %q body = %s, want %s", body, got, want)
		}
	}
}

func TestCreateAgentStoreFailure(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	// Break the store under the handler.
	if _, err := sqlDB.Exec(`DROP TABLE agents`); err != nil {
		t.Fatalf("drop agents: %v", err)
	}

	rec, err := create(t, queries, `{"name":"Scribe","icon":"pencil","config":{}}`)
	// The cause returns for api.Log; the response stays contract-shaped.
	if err == nil {
		t.Fatal("store failure: want error returned for logging")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	want := `{"message":"Internal server error"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
