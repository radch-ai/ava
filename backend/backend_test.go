package backend_test

// Routing pins over New's route table, plus its boot contract: the
// master key must unwrap the keyset. Endpoint behavior is pinned in
// each endpoint package's own tests.

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

var testMasterKey = bytes.Repeat([]byte{0x42}, 32)

func TestRoutes(t *testing.T) {
	handler, err := backend.New(testdb.Open(t), zerolog.Nop(), testMasterKey)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}

	do := func(method, path, body string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code
	}

	if code := do(http.MethodPost, "/agents", ""); code != http.StatusMethodNotAllowed {
		t.Errorf("POST /agents = %d, want 405", code)
	}
	if code := do(http.MethodGet, "/agents", ""); code != http.StatusOK {
		t.Errorf("GET /agents = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/nope", ""); code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", code)
	}
	// The save surface's per-agent routes.
	if code := do(http.MethodGet, "/agent/_superagent/config", ""); code != http.StatusOK {
		t.Errorf("GET /agent/_superagent/config = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/agent/ghost/config", ""); code != http.StatusNotFound {
		t.Errorf("GET /agent/ghost/config = %d, want 404", code)
	}
	if code := do(http.MethodPatch, "/agent/_superagent/config", "{}"); code != http.StatusOK {
		t.Errorf("PATCH /agent/_superagent/config = %d, want 200", code)
	}
	if code := do(http.MethodPatch, "/agent/_superagent/heartbeat",
		`{"model":"auto","enabled":true,"duration":"30m","content":"x"}`); code != http.StatusOK {
		t.Errorf("PATCH /agent/_superagent/heartbeat = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/agent/_superagent/cron-jobs", ""); code != http.StatusOK {
		t.Errorf("GET /agent/_superagent/cron-jobs = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/agent/ghost/cron-jobs", ""); code != http.StatusNotFound {
		t.Errorf("GET /agent/ghost/cron-jobs = %d, want 404", code)
	}
	if code := do(http.MethodDelete, "/agent/_superagent/cron-jobs/nope", ""); code != http.StatusNotFound {
		t.Errorf("DELETE /agent/_superagent/cron-jobs/nope = %d, want 404", code)
	}
	// The selector catalog and per-provider system models.
	if code := do(http.MethodGet, "/models", ""); code != http.StatusOK {
		t.Errorf("GET /models = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/providers/nope/models", ""); code != http.StatusNotFound {
		t.Errorf("GET /providers/nope/models = %d, want 404", code)
	}
}

// tamperedVault seeds one entry in vaultName, then corrupts its envelope.
func tamperedVault(t *testing.T, vaultName string) *sql.DB {
	t.Helper()
	sqlDB := testdb.Open(t)
	master, err := vault.NewMasterAEAD(testMasterKey)
	if err != nil {
		t.Fatalf("master AEAD: %v", err)
	}
	vaultStore, err := vault.NewStore(sqlDB, master)
	if err != nil {
		t.Fatalf("vault store: %v", err)
	}
	if err := vaultStore.Put(context.Background(), "runtime",
		"entry-1", "kind", []byte("value"), nil); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if _, err := sqlDB.Exec(
		`UPDATE vault_entries SET envelope = 'AAAA' WHERE vault = ?`, vaultName); err != nil {
		t.Fatalf("tamper envelope: %v", err)
	}
	return sqlDB
}

// Boot checks exactly one thing: the master key unwraps the stored
// keyset. No envelope walk runs — a tampered entry fails its own read,
// not the plane.
func TestNewBootChecksMasterKeyOnly(t *testing.T) {
	if _, err := backend.New(tamperedVault(t, "runtime"), zerolog.Nop(), testMasterKey); err != nil {
		t.Errorf("tampered entry must not fail boot: %v", err)
	}

	sqlDB := testdb.Open(t)
	if _, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey); err != nil {
		t.Fatalf("first boot: %v", err)
	}
	if _, err := backend.New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x43}, 32)); err == nil {
		t.Error("wrong master key: New succeeded, want boot failure")
	}
}
