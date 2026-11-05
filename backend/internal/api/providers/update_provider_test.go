package providers

// UpdateProvider endpoint tests: patch semantics and the models
// replace.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// Absent fields are no-ops; a withheld apiKey keeps the stored key.
func TestUpdateProviderPatchKeepsAbsentFields(t *testing.T) {
	sqlDB := testdb.Open(t)
	created := do(t, sqlDB, http.MethodPost, "/api/providers",
		`{"name":"Mistral","baseUrl":"https://api.mistral.ai/v1","apiKey":"sk-keep"}`)
	id := createdID(t, created)
	patch := do(t, sqlDB, http.MethodPatch, "/api/providers/"+id, `{"name":"Mistral Pro"}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", patch.Code)
	}
	want := `{"id":"` + id + `","name":"Mistral Pro","baseUrl":"https://api.mistral.ai/v1","hasKey":true,"models":[]}`
	if got := body(patch); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// The models list is replaced wholesale: plain id strings, non-empty
// and unique.
func TestUpdateProviderModelsReplace(t *testing.T) {
	sqlDB := seedCatalog(t)
	patch := do(t, sqlDB, http.MethodPatch, "/api/providers/openai",
		`{"models":["openai/gpt-4o","openai/o3"]}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", patch.Code)
	}
	want := `{"id":"openai","name":"OpenAI","baseUrl":"https://api.openai.com/v1","hasKey":false,` +
		`"models":["openai/gpt-4o","openai/o3"]}`
	if got := body(patch); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	cases := []struct {
		body string
		want string
	}{
		{`{"models":["openai/gpt-4o",""]}`,
			`{"message":"Model id cannot be empty"}`},
		{`{"models":["x","x"]}`,
			`{"message":"Duplicate model id \"x\""}`},
	}
	for _, tc := range cases {
		rec := do(t, sqlDB, http.MethodPatch, "/api/providers/openai", tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH %s status = %d, want 400", tc.body, rec.Code)
		}
		if got := body(rec); got != tc.want {
			t.Errorf("PATCH %s body = %s, want %s", tc.body, got, tc.want)
		}
	}
}

// Dropping a referenced model from the replace list succeeds - the
// engine hands the referencing rows back to NULL routing.
func TestUpdateProviderDropsReferencedModel(t *testing.T) {
	sqlDB := seedCatalog(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('u1', 'Upd', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, enabled, duration, content)
		 VALUES ('u1', 'grok/grok-1', 1, '30m', 'x')`); err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}

	rec := do(t, sqlDB, http.MethodPatch, "/api/providers/grok", `{"models":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var model any
	if err := sqlDB.QueryRow(
		`SELECT model FROM heartbeat WHERE agent_id = 'u1'`,
	).Scan(&model); err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if model != nil {
		t.Errorf("heartbeat model after replace = %v, want NULL (default routing)", model)
	}
}

// Model ids are globally unique: one provider cannot claim an id
// another already owns - the engine rejects the insert and the
// handler maps it to a 409.
func TestUpdateProviderModelOwnedByAnother(t *testing.T) {
	sqlDB := seedCatalog(t)
	rec := do(t, sqlDB, http.MethodPatch, "/api/providers/grok",
		`{"models":["grok/grok-1","openai/gpt-4"]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	want := `{"message":"Model \"openai/gpt-4\" already belongs to another provider"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	// The replace is delete-then-insert: the failed claim leaves the
	// models inserted before it (here just grok/grok-1).
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM models WHERE provider_id = 'grok'`,
	).Scan(&n); err != nil {
		t.Fatalf("count grok models: %v", err)
	}
	if n != 1 {
		t.Errorf("grok models after rejected replace = %d, want 1 (untouched)", n)
	}
}

func TestUpdateProviderNotFound(t *testing.T) {
	sqlDB := testdb.Open(t)
	rec := do(t, sqlDB, http.MethodPatch, "/api/providers/nope", `{"name":"X"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	want := `{"message":"Provider \"nope\" not found"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// createdID extracts {id} from a 201 body.
func createdID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body(rec)), &created); err != nil {
		t.Fatalf("decode 201 body: %v", err)
	}
	return created.ID
}
