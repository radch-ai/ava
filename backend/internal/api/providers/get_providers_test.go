package providers

// The HTTP contract over the real store; DB rules are pinned in
// internal/schema. Providers are rows users create — nothing is
// embedded.

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// do runs one route through a real ServeMux (PathValue needs it).
func do(t *testing.T, sqlDB *sql.DB, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	queries, vaultStore := db.New(sqlDB), newTestStore(t, sqlDB)
	mux := http.NewServeMux()
	switch method {
	case http.MethodGet:
		mux.Handle("GET /providers", api.Log(zerolog.Nop(), GetProviders(queries, vaultStore)))
		mux.Handle("GET /providers/{id}/models", api.Log(zerolog.Nop(), GetProviderModels(queries, vaultStore)))
		mux.Handle("GET /models", api.Log(zerolog.Nop(), GetModels(queries, vaultStore)))
	case http.MethodPost:
		mux.Handle("POST /providers", api.Log(zerolog.Nop(), CreateProvider(queries, vaultStore)))
	case http.MethodPatch:
		mux.Handle("PATCH /providers/{id}", api.Log(zerolog.Nop(), UpdateProvider(queries, vaultStore)))
	case http.MethodDelete:
		mux.Handle("DELETE /providers/{id}", api.Log(zerolog.Nop(), DeleteProvider(queries, vaultStore)))
	default:
		t.Fatalf("unsupported method %s", method)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

// testMasterKey seals every test vault.
var testMasterKey = bytes.Repeat([]byte{0x42}, 32)

// newTestStore opens the vault over a test database.
func newTestStore(t *testing.T, sqlDB *sql.DB) *vault.Store {
	t.Helper()
	master, err := vault.NewMasterAEAD(testMasterKey)
	if err != nil {
		t.Fatalf("master AEAD: %v", err)
	}
	vaultStore, err := vault.NewStore(sqlDB, master)
	if err != nil {
		t.Fatalf("vault store: %v", err)
	}
	return vaultStore
}

// seedKey seals a provider key into the runtime vault directly — what
// a keyed create or key patch leaves behind.
func seedKey(t *testing.T, sqlDB *sql.DB, id, key string) {
	t.Helper()
	if err := newTestStore(t, sqlDB).Put(context.Background(),
		vault.NameRuntime, providerEntryName(id), kindProvider, []byte(key), nil); err != nil {
		t.Fatalf("seed key for %s: %v", id, err)
	}
}

// body is the trimmed response body, for exact pins.
func body(rec *httptest.ResponseRecorder) string {
	return strings.TrimSpace(rec.Body.String())
}

// The empty store lists nothing: providers are rows users create.
func TestGetProvidersEmptyStore(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	rec := do(t, sqlDB, http.MethodGet, "/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"providers":[]}`+"\n" {
		t.Errorf("body = %s, want empty list", got)
	}
}

// Rows list in creation order with their custom models; the stored
// key never leaves.
func TestGetProvidersListsRows(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	for _, stmt := range []string{
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('p-1', 'OpenRouter', 'https://openrouter.ai/api/v1', '["or/m1"]')`,
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-1', 'Mistral', 'https://api.mistral.ai/v1', '["mistral/m1"]')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	seedKey(t, sqlDB, "p-1", "k")
	rec := do(t, sqlDB, http.MethodGet, "/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"providers":[` +
		`{"id":"p-1","name":"OpenRouter","baseUrl":"https://openrouter.ai/api/v1","hasKey":true,"models":["or/m1"]},` +
		`{"id":"u-1","name":"Mistral","baseUrl":"https://api.mistral.ai/v1","hasKey":false,"models":["mistral/m1"]}]}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
	if strings.Contains(body(rec), "apiKey") || strings.Contains(body(rec), `"k"`) {
		t.Error("body leaks the stored key")
	}
}
