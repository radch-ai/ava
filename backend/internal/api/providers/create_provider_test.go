package providers

// CreateProvider endpoint tests: uuid ids and validation answers.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestCreateProvider(t *testing.T) {
	sqlDB := testdb.Open(t)
	rec := do(t, sqlDB, http.MethodPost, "/api/providers",
		`{"name":"Mistral","baseUrl":"https://api.mistral.ai/v1","apiKey":"sk-x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var created struct{ ID string }
	if err := json.Unmarshal([]byte(body(rec)), &created); err != nil {
		t.Fatalf("decode 201 body: %v", err)
	}
	// The id is a minted uuid - nothing derives from the name.
	if created.ID == "" || created.ID == "mistral" {
		t.Errorf("id = %q, want a non-empty uuid distinct from the name", created.ID)
	}

	// The created provider lists with its key masked (never echoed).
	rec = do(t, sqlDB, http.MethodGet, "/api/providers", "")
	wantEntry := `"name":"Mistral","baseUrl":"https://api.mistral.ai/v1","hasKey":true,"models":[]`
	if !strings.Contains(body(rec), wantEntry) {
		t.Errorf("list missing entry %s (got %s)", wantEntry, body(rec))
	}
}

func TestCreateProviderValidation(t *testing.T) {
	sqlDB := testdb.Open(t)
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
		rec := do(t, sqlDB, http.MethodPost, "/api/providers", tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want 400", tc.body, rec.Code)
		}
		if got := body(rec); got != tc.want {
			t.Errorf("POST %s body = %s, want %s", tc.body, got, tc.want)
		}
	}
}
