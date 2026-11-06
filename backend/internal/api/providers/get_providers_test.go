package providers

// The HTTP contract over the real store; DB rules are pinned in
// internal/schema. Providers are rows users create — nothing is
// embedded.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// do runs one route through a real ServeMux (PathValue needs it).
func do(t *testing.T, sqlDB *sql.DB, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	switch method {
	case http.MethodGet:
		mux.Handle("GET /api/providers", api.Log(zerolog.Nop(), GetProviders(db.New(sqlDB))))
		mux.Handle("GET /api/providers/{id}/models", api.Log(zerolog.Nop(), GetProviderModels(db.New(sqlDB))))
		mux.Handle("GET /api/models", api.Log(zerolog.Nop(), GetModels(db.New(sqlDB))))
	case http.MethodPost:
		mux.Handle("POST /api/providers", api.Log(zerolog.Nop(), CreateProvider(db.New(sqlDB))))
	case http.MethodPatch:
		mux.Handle("PATCH /api/providers/{id}", api.Log(zerolog.Nop(), UpdateProvider(db.New(sqlDB))))
	case http.MethodDelete:
		mux.Handle("DELETE /api/providers/{id}", api.Log(zerolog.Nop(), DeleteProvider(db.New(sqlDB))))
	default:
		t.Fatalf("unsupported method %s", method)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

// body is the trimmed response body, for exact pins.
func body(rec *httptest.ResponseRecorder) string {
	return strings.TrimSpace(rec.Body.String())
}

// The empty store lists nothing: providers are rows users create.
func TestGetProvidersEmptyStore(t *testing.T) {
	sqlDB := testdb.Open(t)
	rec := do(t, sqlDB, http.MethodGet, "/api/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"defaultModel":"auto","providers":[]}`+"\n" {
		t.Errorf("body = %s, want empty list", got)
	}
}

// Rows list in creation order with their custom models; the stored
// key never leaves.
func TestGetProvidersListsRows(t *testing.T) {
	sqlDB := testdb.Open(t)
	for _, stmt := range []string{
		`INSERT INTO providers (id, name, base_url, api_key, models)
		 VALUES ('p-1', 'OpenRouter', 'https://openrouter.ai/api/v1', 'k', '["or/m1"]')`,
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-1', 'Mistral', 'https://api.mistral.ai/v1', '["mistral/m1"]')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	rec := do(t, sqlDB, http.MethodGet, "/api/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"defaultModel":"auto","providers":[` +
		`{"id":"p-1","name":"OpenRouter","baseUrl":"https://openrouter.ai/api/v1","hasKey":true,"models":["or/m1"]},` +
		`{"id":"u-1","name":"Mistral","baseUrl":"https://api.mistral.ai/v1","hasKey":false,"models":["mistral/m1"]}]}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
	if strings.Contains(body(rec), "apiKey") || strings.Contains(body(rec), `"k"`) {
		t.Error("body leaks the stored key")
	}
}
