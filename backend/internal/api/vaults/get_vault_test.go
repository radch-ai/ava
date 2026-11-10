package vaults

// The HTTP contract over the real vault store; crypto and DB rules are
// pinned in internal/vault and internal/schema.

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// do runs one route through a real ServeMux (PathValue needs it).
func do(t *testing.T, sqlDB *sql.DB, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	vaultStore := newTestStore(t, sqlDB)
	mux := http.NewServeMux()
	mux.Handle("GET /vault", api.Log(zerolog.Nop(), GetVault(vaultStore)))
	mux.Handle("PUT /vault/{name}", api.Log(zerolog.Nop(), PutSecret(vaultStore)))
	mux.Handle("DELETE /vault/{name}", api.Log(zerolog.Nop(), DeleteSecret(vaultStore)))
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

// seedEntry seals an entry directly — what a PUT (or an app flow, for
// runtime entries) leaves behind.
func seedEntry(t *testing.T, sqlDB *sql.DB, vaultName, name, kind, value string) {
	t.Helper()
	if err := newTestStore(t, sqlDB).Put(t.Context(), vaultName,
		name, kind, []byte(value), nil); err != nil {
		t.Fatalf("seed entry %s/%s: %v", vaultName, name, err)
	}
}

func TestGetListsMetadataOnly(t *testing.T) {
	sqlDB := testdb.Open(t)
	seedEntry(t, sqlDB, vault.NameSecrets, "openai-key", "secret", "sk-live-1")
	seedEntry(t, sqlDB, vault.NameSecrets, "anthropic-key", "secret", "sk-ant-2")

	rec := do(t, sqlDB, http.MethodGet, "/vault", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	want := `{"secrets":[` +
		`{"name":"anthropic-key","kind":"secret"},` +
		`{"name":"openai-key","kind":"secret"}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s\nwant %s", got, want)
	}
	for _, leaked := range []string{"sk-live-1", "sk-ant-2"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("list leaked value %q: %s", leaked, rec.Body)
		}
	}
}

func TestGetEmpty(t *testing.T) {
	rec := do(t, testdb.Open(t), http.MethodGet, "/vault", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if got, want := strings.TrimSpace(rec.Body.String()), `{"secrets":[]}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

// The runtime vault has no HTTP surface: its entries never appear in
// the secrets list.
func TestGetHidesRuntimeVault(t *testing.T) {
	sqlDB := testdb.Open(t)
	seedEntry(t, sqlDB, vault.NameRuntime, "provider:p-1", "provider", "k-1")
	seedEntry(t, sqlDB, vault.NameSecrets, "openai-key", "secret", "sk-1")

	rec := do(t, sqlDB, http.MethodGet, "/vault", "")
	if strings.Contains(rec.Body.String(), "provider:p-1") {
		t.Fatalf("list leaked runtime entry: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "openai-key") {
		t.Fatalf("list missing secrets entry: %s", rec.Body)
	}
}

// Stored meta is context, never secret: it lists back, value does not.
func TestGetReturnsMeta(t *testing.T) {
	sqlDB := testdb.Open(t)
	store := newTestStore(t, sqlDB)
	if err := store.Put(t.Context(), vault.NameSecrets, "db-url", "secret",
		[]byte("postgres://u:p@h/db"), []byte(`{"note":"staging"}`)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := do(t, sqlDB, http.MethodGet, "/vault", "")
	want := `{"secrets":[{"name":"db-url","kind":"secret","meta":{"note":"staging"}}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s\nwant %s", got, want)
	}
	if strings.Contains(rec.Body.String(), "postgres://") {
		t.Fatalf("list leaked value: %s", rec.Body)
	}
}
