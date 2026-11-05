package providers

// The HTTP contract over the real store; DB rules are pinned in
// internal/schema.

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

// seedCatalog inserts the starter rows via raw SQL (the migrations
// seed nothing).
func seedCatalog(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB := testdb.Open(t)
	for _, stmt := range []string{
		`INSERT INTO providers (id, name, base_url) VALUES
			('openai',    'OpenAI',    'https://api.openai.com/v1'),
			('anthropic', 'Anthropic', 'https://api.anthropic.com/v1'),
			('gemini',    'Gemini',    'https://generativelanguage.googleapis.com/v1beta/openai'),
			('grok',      'Grok',      'https://api.x.ai/v1')`,
		`INSERT INTO models (model_id, provider_id) VALUES
			('openai/gpt-4',          'openai'),
			('openai/gpt-3.5-turbo',  'openai'),
			('anthropic/claude-3-sonnet', 'anthropic'),
			('gemini/gemini-pro',     'gemini'),
			('gemini/gemini-ultra',   'gemini'),
			('grok/grok-1',           'grok')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("seed catalog: %v", err)
		}
	}
	return sqlDB
}

func TestGetProviders(t *testing.T) {
	sqlDB := seedCatalog(t)
	rec := do(t, sqlDB, http.MethodGet, "/api/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// One deterministic shape: shared created_at, id tiebreak,
	// id-ordered models, no api_key anywhere.
	want := `{"defaultModel":"auto","providers":[` +
		`{"id":"anthropic","name":"Anthropic","baseUrl":"https://api.anthropic.com/v1","hasKey":false,` +
		`"models":["anthropic/claude-3-sonnet"]},` +
		`{"id":"gemini","name":"Gemini","baseUrl":"https://generativelanguage.googleapis.com/v1beta/openai","hasKey":false,` +
		`"models":["gemini/gemini-pro","gemini/gemini-ultra"]},` +
		`{"id":"grok","name":"Grok","baseUrl":"https://api.x.ai/v1","hasKey":false,` +
		`"models":["grok/grok-1"]},` +
		`{"id":"openai","name":"OpenAI","baseUrl":"https://api.openai.com/v1","hasKey":false,` +
		`"models":["openai/gpt-3.5-turbo","openai/gpt-4"]}]}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
	if strings.Contains(body(rec), "apiKey") {
		t.Error("body leaks an apiKey field")
	}
}
