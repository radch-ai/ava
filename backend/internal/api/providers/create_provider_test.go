package providers

// CreateProvider endpoint tests: uuid ids, validation answers, and
// the save-time connection check.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// A keyless create still probes: a no-auth endpoint (local
// OpenAI-compatible server) saves; a dead URL saves nothing.
func TestCreateProviderKeylessProbes(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	rec := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Ollama","baseUrl":"`+noAuthURL(t)+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("no-auth status = %d, want 201 (body %s)", rec.Code, body(rec))
	}
	var created struct{ ID string }
	if err := json.Unmarshal([]byte(body(rec)), &created); err != nil {
		t.Fatalf("decode 201 body: %v", err)
	}
	// The id is a minted uuid - nothing derives from the name.
	if created.ID == "" || created.ID == "ollama" {
		t.Errorf("id = %q, want a non-empty uuid distinct from the name", created.ID)
	}

	rec = do(t, sqlDB, http.MethodGet, "/providers", "")
	wantEntry := `"hasKey":false`
	if !strings.Contains(body(rec), wantEntry) {
		t.Errorf("list missing entry %s (got %s)", wantEntry, body(rec))
	}

	sqlDB = testdb.Open(t, true)
	rec = do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Dead","baseUrl":"http://127.0.0.1:1/v1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("keyless-dead status = %d, want 400", rec.Code)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&n); err != nil {
		t.Fatalf("count providers: %v", err)
	}
	if n != 0 {
		t.Errorf("providers after dead keyless create = %d, want 0", n)
	}
}

// A keyed create probes first: success saves, a rejected key saves
// nothing.
func TestCreateProviderProbe(t *testing.T) {
	url := fakeProvider(t).URL

	sqlDB := testdb.Open(t, true)
	rec := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Fake","baseUrl":"`+url+`","apiKey":"k-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ok-key status = %d, want 201 (body %s)", rec.Code, body(rec))
	}

	sqlDB = testdb.Open(t, true)
	rec = do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Fake","baseUrl":"`+url+`","apiKey":"wrong"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad-key status = %d, want 400", rec.Code)
	}
	want := `{"message":"Connection check failed: endpoint rejected the request (401) — key missing or invalid"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&n); err != nil {
		t.Fatalf("count providers: %v", err)
	}
	if n != 0 {
		t.Errorf("providers after rejected create = %d, want 0 (nothing saved)", n)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM vault_entries`).Scan(&n); err != nil {
		t.Fatalf("count vault entries: %v", err)
	}
	if n != 0 {
		t.Errorf("vault entries after rejected create = %d, want 0 (nothing sealed)", n)
	}
}

// An unreachable base URL fails the check the same way.
func TestCreateProviderProbeUnreachable(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	rec := do(t, sqlDB, http.MethodPost, "/providers",
		`{"name":"Dead","baseUrl":"http://127.0.0.1:1/v1","apiKey":"k-1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := body(rec); !strings.Contains(got, "Connection check failed: could not reach") {
		t.Errorf("body = %s, want could-not-reach failure", got)
	}
}

func TestCreateProviderValidation(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	cases := []struct {
		body string
		want string
	}{
		{`{"name":"  ","baseUrl":"https://x.dev"}`,
			`{"message":"Provider name is required"}`},
		{`{"name":"No URL"}`,
			`{"message":"Invalid baseUrl: must be an http(s) URL"}`},
		{`{"name":"No URL","baseUrl":"ftp://x.dev"}`,
			`{"message":"Invalid baseUrl: must be an http(s) URL"}`},
		{`{"name":"No URL","baseUrl":"x.dev"}`,
			`{"message":"Invalid baseUrl: must be an http(s) URL"}`},
		{`not json`, `{"message":"Invalid request body"}`},
	}
	for _, tc := range cases {
		rec := do(t, sqlDB, http.MethodPost, "/providers", tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want 400", tc.body, rec.Code)
		}
		if got := body(rec); got != tc.want {
			t.Errorf("POST %s body = %s, want %s", tc.body, got, tc.want)
		}
	}
}
