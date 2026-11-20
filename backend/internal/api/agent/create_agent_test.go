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
func create(t *testing.T, sqlDB *sql.DB, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	err := CreateAgent(sqlDB, db.New(sqlDB))(rec,
		httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(body)))
	return rec, err
}

func TestCreateAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	rec, err := create(t, sqlDB, `{"name":"Research Bot","icon":"globe","soul":{"personality":"curious"}}`)
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
	sqlDB := testdb.Open(t, true)
	if rec, err := create(t, sqlDB,
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
	sqlDB := testdb.Open(t, true)
	for _, name := range []string{
		"",      // empty
		"A  B",  // double space
		"Bob-1", // hyphen — letters, digits and single spaces only
		"_x",    // underscore (would collide with the system prefix)
		"Bob!",  // punctuation
	} {
		rec, err := create(t, sqlDB, `{"name":"`+name+`"}`)
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
	sqlDB := testdb.Open(t, true)
	if rec, err := create(t, sqlDB, `{"name":"Research"}`); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201", rec.Code)
	}

	rec, err := create(t, sqlDB, `{"name":"Research"}`)
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
	rec, err = create(t, sqlDB, `{"name":"RESEARCH"}`)
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
	sqlDB := testdb.Open(t, true)
	// Malformed JSON never reaches the mock's handler (its
	// request.json() throws).
	for _, body := range []string{"", "not json"} {
		rec, err := create(t, sqlDB, body)
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
	sqlDB := testdb.Open(t, true)
	// Config fields are optional; omitted texts insert as '' and
	// user_context as NULL.
	if rec, err := create(t, sqlDB, `{"name":"Minimal","icon":"bot"}`); err != nil || rec.Code != http.StatusCreated {
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
	sqlDB := testdb.Open(t, true)
	// Break the store under the handler. Referencing tables drop first
	// so the teardown stays valid under foreign_keys=ON.
	for _, table := range []string{"thread_messages", "agent_activity", "heartbeat", "cron_jobs", "agents", "threads"} {
		if _, err := sqlDB.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	rec, err := create(t, sqlDB, `{"name":"Scribe","icon":"pencil"}`)
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

// Creation is eager: the agent and its initial thread land together,
// and the pointer names that thread.
func TestCreateAgentSeedsInitialThread(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	if rec, err := create(t, sqlDB, `{"name":"Research Bot","icon":"globe"}`); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%v), want 201", rec.Code, err)
	}

	active, err := db.New(sqlDB).GetActiveThreadID(t.Context(), "research-bot")
	if err != nil {
		t.Fatalf("read active thread: %v", err)
	}
	if active != "research-bot-t1" {
		t.Errorf("active thread = %q, want research-bot-t1", active)
	}
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM threads WHERE id = 'research-bot-t1' AND agent_id = 'research-bot'`,
	).Scan(&n); err != nil {
		t.Fatalf("count threads: %v", err)
	}
	if n != 1 {
		t.Errorf("thread rows = %d, want 1", n)
	}
}

// The thread write failing rolls the agent back: no half-created
// identity is visible.
func TestCreateAgentThreadFailureLeavesNoAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	// Rename the table under the handler so the thread insert fails
	// while the agent insert itself would have succeeded.
	if _, err := sqlDB.Exec(`ALTER TABLE threads RENAME TO threads_off`); err != nil {
		t.Fatalf("rename threads: %v", err)
	}

	_, err := create(t, sqlDB, `{"name":"Scribe","icon":"pencil"}`)
	if err == nil {
		t.Fatal("thread failure: want error returned for logging")
	}
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM agents WHERE id = 'scribe'`).Scan(&n); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if n != 0 {
		t.Fatalf("agent rows after failed creation = %d, want 0", n)
	}
}

// Every creation failure is answered: a thread-write fault cannot
// leave the client with an empty 200.
func TestCreateAgentInsertThreadFailureAnswers500(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	// Drop the parent table so the initial thread insert fails after
	// the agent insert itself succeeded.
	if _, err := sqlDB.Exec(`ALTER TABLE threads RENAME TO threads_off`); err != nil {
		t.Fatalf("rename threads: %v", err)
	}

	rec, err := create(t, sqlDB, `{"name":"Scribe","icon":"pencil"}`)
	if err == nil {
		t.Fatal("thread failure: want error returned for logging")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"message":"Internal server error"}` {
		t.Errorf("body = %s, want the internal-error body", body)
	}
}

// The id-collision classifier reads the constraint's own message: the
// pre-check races other writers, so the insert must recognize the
// duplicate it loses to.
func TestIsAgentIDConflict(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "taken", Name: "Taken", Icon: "globe"})

	_, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context, active_thread_id)
		 VALUES ('taken', 'Again', 'globe', '', '', '', '', 'taken-t1')`)
	if err == nil {
		t.Fatal("duplicate agent insert succeeded, want a constraint error")
	}
	if !isAgentIDConflict(err) {
		t.Errorf("isAgentIDConflict(%v) = false, want true", err)
	}

	// A different uniqueness rule is not an agent-id collision.
	if _, err := sqlDB.Exec(
		`INSERT INTO threads (id, agent_id) VALUES ('taken-t1', 'taken')`); err == nil {
		t.Fatal("duplicate thread insert succeeded, want a constraint error")
	} else if isAgentIDConflict(err) {
		t.Errorf("isAgentIDConflict(%v) = true for a thread violation, want false", err)
	}

	if isAgentIDConflict(sql.ErrNoRows) {
		t.Error("isAgentIDConflict(sql.ErrNoRows) = true, want false")
	}
}
