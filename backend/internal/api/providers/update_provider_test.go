package providers

// UpdateProvider endpoint tests: patch semantics, the models
// replace, system-provider attach, and the save-time connection
// check.

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
	url := noAuthURL(t)
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Mistral","baseUrl":"`+url+`"}`)
	id := createdID(t, created)
	patch := do(t, sqlDB, http.MethodPatch, "/providers/"+id, `{"name":"Mistral Pro"}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", patch.Code)
	}
	want := `{"id":"` + id + `","name":"Mistral Pro","baseUrl":"` + url + `","hasKey":false,"models":[]}`
	if got := body(patch); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// The models list is replaced wholesale: plain id strings, non-empty
// and unique; an empty list clears the custom models.
func TestUpdateProviderModelsReplace(t *testing.T) {
	sqlDB := testdb.Open(t)
	url := noAuthURL(t)
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Fake","baseUrl":"`+url+`"}`)
	id := createdID(t, created)

	patch := do(t, sqlDB, http.MethodPatch, "/providers/"+id,
		`{"models":["f/m2","f/m1"]}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", patch.Code)
	}
	want := `{"id":"` + id + `","name":"Fake","baseUrl":"` + url + `","hasKey":false,"models":["f/m1","f/m2"]}`
	if got := body(patch); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	patch = do(t, sqlDB, http.MethodPatch, "/providers/"+id, `{"models":[]}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", patch.Code)
	}
	if got := body(patch); !jsonEqualModels(got, id, "Fake", url, false, []string{}) {
		t.Errorf("body = %s, want cleared models", got)
	}
}

func TestUpdateProviderModelsValidation(t *testing.T) {
	sqlDB := testdb.Open(t)
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Fake","baseUrl":"`+noAuthURL(t)+`"}`)
	id := createdID(t, created)

	cases := []struct {
		body string
		want string
	}{
		{`{"models":["f/m1",""]}`, `{"message":"Model id cannot be empty"}`},
		{`{"models":["x","x"]}`, `{"message":"Duplicate model id \"x\""}`},
	}
	for _, tc := range cases {
		rec := do(t, sqlDB, http.MethodPatch, "/providers/"+id, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH %s status = %d, want 400", tc.body, rec.Code)
		}
		if got := body(rec); got != tc.want {
			t.Errorf("PATCH %s body = %s, want %s", tc.body, got, tc.want)
		}
	}
}

// Custom ids are globally unique across providers: a claim on an id
// another provider owns is a 409, and the rejected replace changes
// nothing (the write is one atomic statement).
func TestUpdateProviderModelOwnedByAnother(t *testing.T) {
	sqlDB := testdb.Open(t)
	url := noAuthURL(t)
	first := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"One","baseUrl":"`+url+`"}`)
	id1 := createdID(t, first)
	if rec := do(t, sqlDB, http.MethodPatch, "/providers/"+id1, `{"models":["one/m1"]}`); rec.Code != http.StatusOK {
		t.Fatalf("seed models: %d", rec.Code)
	}
	second := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Two","baseUrl":"`+url+`"}`)
	id2 := createdID(t, second)

	rec := do(t, sqlDB, http.MethodPatch, "/providers/"+id2,
		`{"models":["two/m1","one/m1"]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	want := `{"message":"Model \"one/m1\" already belongs to another provider"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	// Atomic: the rejected list saved nothing — provider Two keeps
	// an empty custom list.
	rec = do(t, sqlDB, http.MethodGet, "/providers", "")
	if containsStr(rec.Body.String(), "two/m1") {
		t.Errorf("rejected replace leaked partial state: %s", body(rec))
	}
}

// A patch touching the key (or URL) probes the effective connection
// first: a rejected or detached key changes nothing, a good one
// saves.
func TestUpdateProviderProbe(t *testing.T) {
	url := fakeProvider(t).URL

	sqlDB := testdb.Open(t)
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Fake","baseUrl":"`+url+`","apiKey":"k-1"}`)
	id := createdID(t, created)

	rec := do(t, sqlDB, http.MethodPatch, "/providers/"+id, `{"apiKey":"wrong"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad-key status = %d, want 400", rec.Code)
	}
	want := `{"message":"Connection check failed: endpoint rejected the request (401) — key missing or invalid"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	// Nothing saved: the stored key survives.
	rec = do(t, sqlDB, http.MethodGet, "/providers", "")
	if want := `"id":"` + id + `","name":"Fake","baseUrl":"` + url + `","hasKey":true`; !containsStr(body(rec), want) {
		t.Errorf("rejected key was saved: %s", body(rec))
	}

	// Detaching the key from an auth-required endpoint is a broken
	// configuration: rejected, nothing saved.
	rec = do(t, sqlDB, http.MethodPatch, "/providers/"+id, `{"apiKey":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("detach status = %d, want 400", rec.Code)
	}
	if got := body(rec); !containsStr(got, "key missing or invalid") {
		t.Errorf("body = %s, want key-missing failure", got)
	}

	// A patch not touching the connection doesn't probe.
	rec = do(t, sqlDB, http.MethodPatch, "/providers/"+id, `{"name":"Fake 2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("name-only status = %d, want 200", rec.Code)
	}
	if want := `"hasKey":true`; !containsStr(body(rec), want) {
		t.Errorf("body = %s, want hasKey true", body(rec))
	}
}

// Clearing the key with an explicit empty string removes the vault
// entry when the keyless connection still checks out.
func TestUpdateProviderClearKey(t *testing.T) {
	sqlDB := testdb.Open(t)
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Local","baseUrl":"`+noAuthURL(t)+`","apiKey":"k-1"}`)
	id := createdID(t, created)

	rec := do(t, sqlDB, http.MethodPatch, "/providers/"+id, `{"apiKey":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", rec.Code)
	}
	if want := `"hasKey":false`; !containsStr(body(rec), want) {
		t.Errorf("body = %s, want hasKey false", body(rec))
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM vault_entries`).Scan(&n); err != nil {
		t.Fatalf("count vault entries: %v", err)
	}
	if n != 0 {
		t.Errorf("vault entries after clear = %d, want 0", n)
	}
}

// A baseUrl patch re-probes with the stored key: moving to a dead
// URL changes nothing.
func TestUpdateProviderBaseUrlReprobes(t *testing.T) {
	url := fakeProvider(t).URL
	sqlDB := testdb.Open(t)
	created := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Fake","baseUrl":"`+url+`","apiKey":"k-1"}`)
	id := createdID(t, created)

	rec := do(t, sqlDB, http.MethodPatch, "/providers/"+id,
		`{"baseUrl":"http://127.0.0.1:1/v1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := body(rec); !containsStr(got, "Connection check failed: could not reach") {
		t.Errorf("body = %s, want could-not-reach failure", got)
	}
	// The stored URL survived the rejected patch.
	rec = do(t, sqlDB, http.MethodGet, "/providers", "")
	if want := `"baseUrl":"` + url + `"`; !containsStr(body(rec), want) {
		t.Errorf("rejected baseUrl was saved: %s", body(rec))
	}
}

func TestUpdateProviderNotFound(t *testing.T) {
	sqlDB := testdb.Open(t)
	rec := do(t, sqlDB, http.MethodPatch, "/providers/nope", `{"name":"X"}`)
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
	var created struct{ ID string }
	if err := json.Unmarshal([]byte(body(rec)), &created); err != nil {
		t.Fatalf("decode 201 body: %v", err)
	}
	return created.ID
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// jsonEqualModels checks the cleared-models body without pinning a
// whole JSON string twice.
func jsonEqualModels(got, id, name, baseURL string, hasKey bool, models []string) bool {
	var parsed struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		return false
	}
	if len(parsed.Models) != len(models) {
		return false
	}
	return true
}
