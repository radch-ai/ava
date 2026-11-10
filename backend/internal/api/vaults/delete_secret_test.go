package vaults

import (
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

func TestDeleteSecret(t *testing.T) {
	sqlDB := testdb.Open(t)
	seedEntry(t, sqlDB, vault.NameSecrets, "openai-key", "secret", "sk-1")

	rec := do(t, sqlDB, http.MethodDelete, "/api/vault/openai-key", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}

	entries, err := newTestStore(t, sqlDB).List(t.Context(), vault.NameSecrets)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}

func TestDeleteSecretMissing(t *testing.T) {
	rec := do(t, testdb.Open(t), http.MethodDelete, "/api/vault/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "not found") {
		t.Fatalf("body = %s, want it to contain %q", rec.Body, "not found")
	}
}
