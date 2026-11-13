package backend_test

// Resolver pins: an explicit pair resolves to the provider's
// endpoint, and a nil entry or a missing key fails closed. The tests
// call the package function directly — the same shape the gateway
// uses.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// resolverDB boots a fresh backend — which binds ResolveModel — over
// its own seeded database.
func resolverDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	if _, _, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey); err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	return sqlDB
}

func seedProvider(t *testing.T, sqlDB *sql.DB, id, baseURL string) {
	t.Helper()
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO providers (id, name, base_url) VALUES (?, ?, ?)`,
		id, id+"-name", baseURL); err != nil {
		t.Fatalf("seed provider %s: %v", id, err)
	}
}

func seedProviderKey(t *testing.T, sqlDB *sql.DB, id, key string) {
	t.Helper()
	master, err := vault.NewMasterAEAD(testMasterKey)
	if err != nil {
		t.Fatalf("master AEAD: %v", err)
	}
	store, err := vault.NewStore(sqlDB, master)
	if err != nil {
		t.Fatalf("vault store: %v", err)
	}
	// "provider" mirrors kindProvider in the providers package.
	if err := store.Put(context.Background(), vault.NameRuntime,
		providers.ProviderEntryName(id), "provider", []byte(key), nil); err != nil {
		t.Fatalf("seed key for %s: %v", id, err)
	}
}

func TestResolveModelEndpoint(t *testing.T) {
	sqlDB := resolverDB(t)
	seedProvider(t, sqlDB, "prov-1", "https://api.example.com/v1")
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")

	got, err := backend.ResolveModel(context.Background(),
		&apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "prov-1"})
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	want := backend.ResolvedModel{
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-test",
		Model:   "glm-4.7",
	}
	if got != want {
		t.Errorf("ResolveModel = %+v, want %+v", got, want)
	}
}

func TestResolveModelNilEntryFails(t *testing.T) {
	resolverDB(t)
	if got, err := backend.ResolveModel(context.Background(), nil); err == nil || got != (backend.ResolvedModel{}) {
		t.Fatalf("ResolveModel nil entry = (%+v, %v), want zero value and an error", got, err)
	}
}

func TestResolveModelKeylessProviderFails(t *testing.T) {
	sqlDB := resolverDB(t)
	seedProvider(t, sqlDB, "prov-2", "https://api.example.com/v1")
	if got, err := backend.ResolveModel(context.Background(),
		&apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "prov-2"}); err == nil || got != (backend.ResolvedModel{}) {
		t.Fatalf("ResolveModel keyless provider = (%+v, %v), want zero value and an error", got, err)
	}
}
