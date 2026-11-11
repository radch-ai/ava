package agent

// CreateAgent handler tests: the handler over the real store.
// Post-create state is read back through the store layer; route
// mounting is pinned in backend's tests.

import (
	"database/sql"
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
		httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(body)))
	return rec, err
}

func TestCreateAgent(t *testing.T) {
	queries := db.New(testdb.Open(t))
	rec, err := create(t, queries, `{"name":"Research Bot","icon":"globe","soul":{"personality":"curious"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	// The 201 body is {id}; the id is the derived one.
	want := `{"id":"research-bot"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestCreateAgentConfigStoredAsColumns(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	if rec, err := create(t, queries,
		`{"name":"Research Bot","icon":"globe","soul":{"personality":"curious","identity":"the lab researcher"},"userContext":"morning person","operatingInstructions":"cite sources","toolsContext":""}`,
	); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	// The five prompt texts land in their columns verbatim.
	var personality, identity, userContext, operating, toolsContext string
	if err := sqlDB.QueryRow(
		`SELECT soul_personality, soul_identity, user_context,
		        operating_instructions, tools_context
		 FROM agents WHERE id = 'research-bot'`,
	).Scan(&personality, &identity, &userContext, &operating, &toolsContext); err != nil {
		t.Fatalf("read config columns: %v", err)
	}
	if personality != "curious" || identity != "the lab researcher" ||
		userContext != "morning person" || operating != "cite sources" || toolsContext != "" {
		t.Errorf("config columns = %q/%q/%q/%q/%q, want curious/the lab researcher/morning person/cite sources/\"\"",
			personality, identity, userContext, operating, toolsContext)
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
	if rec, err := create(t, queries, `{"name":"Research"}`); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201", rec.Code)
	}

	rec, err := create(t, queries, `{"name":"Research"}`)
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
	rec, err = create(t, queries, `{"name":"RESEARCH"}`)
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
	// request.json() throws).
	for _, body := range []string{"", "not json"} {
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

func TestCreateAgentConfigOptional(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	// Config fields are optional; omitted texts insert as '' and
	// user_context as NULL.
	if rec, err := create(t, queries, `{"name":"Minimal","icon":"bot"}`); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%v), want 201", rec.Code, err)
	}
	var personality string
	var userContext sql.NullString
	if err := sqlDB.QueryRow(
		`SELECT soul_personality, user_context FROM agents WHERE id = 'minimal'`,
	).Scan(&personality, &userContext); err != nil {
		t.Fatalf("read column: %v", err)
	}
	if personality != "" {
		t.Errorf("soul_personality = %q, want empty", personality)
	}
	if userContext.Valid {
		t.Errorf("user_context = %q, want NULL", userContext.String)
	}
}

func TestCreateAgentStoreFailure(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)
	// Break the store under the handler. Referencing tables drop first
	// so the teardown stays valid under foreign_keys=ON.
	for _, table := range []string{"heartbeat", "cron_jobs", "agents"} {
		if _, err := sqlDB.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	rec, err := create(t, queries, `{"name":"Scribe","icon":"pencil"}`)
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
