package vaults

import (
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

func TestPutSecretCreates(t *testing.T) {
	sqlDB := testdb.Open(t)
	rec := do(t, sqlDB, http.MethodPut, "/api/vault/openai-key",
		`{"kind":"secret","value":"sk-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sk-1") {
		t.Fatalf("response echoed value: %s", rec.Body)
	}

	// Sealed under the secrets vault at the given name.
	plaintext, _, err := newTestStore(t, sqlDB).Get(t.Context(),
		vault.NameSecrets, "openai-key")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if string(plaintext) != "sk-1" {
		t.Fatalf("stored value = %q, want %q", plaintext, "sk-1")
	}
}

// Change means replace: a second PUT at the same name overwrites in
// place — one entry, new value.
func TestPutSecretReplaces(t *testing.T) {
	sqlDB := testdb.Open(t)
	do(t, sqlDB, http.MethodPut, "/api/vault/openai-key",
		`{"kind":"secret","value":"sk-old"}`)
	rec := do(t, sqlDB, http.MethodPut, "/api/vault/openai-key",
		`{"kind":"secret","value":"sk-new"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}

	store := newTestStore(t, sqlDB)
	plaintext, _, err := store.Get(t.Context(), vault.NameSecrets, "openai-key")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if string(plaintext) != "sk-new" {
		t.Fatalf("stored value = %q, want %q", plaintext, "sk-new")
	}
	entries, err := store.List(t.Context(), vault.NameSecrets)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
}

func TestPutSecretValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty kind", `{"kind":"","value":"v"}`, "Secret kind is required"},
		{"empty value", `{"kind":"secret","value":""}`, "Secret value is required"},
		{"bad json", `{not json`, "Invalid request body"},
		{"bad meta json", `{"kind":"secret","value":"v","meta":{no}}`, "Invalid request body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, testdb.Open(t), http.MethodPut, "/api/vault/n", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body, tc.want)
			}
		})
	}
}
