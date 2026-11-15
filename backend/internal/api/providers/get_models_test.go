package providers

// GET /api/models: the selector-facing grouped catalog.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// Keyless empty store: no groups — system models exist only once a
// key fetches them.
func TestGetModelsEmptyStore(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	rec := do(t, sqlDB, http.MethodGet, "/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := body(rec); got != `{"groups":[]}` {
		t.Errorf("body = %s, want empty groups", got)
	}
}

// A keyed user provider serves its live list merged with custom ids
// (custom wins the dedupe); providers with no models at all are
// dropped from the groups.
func TestGetModelsMergesLiveAndCustom(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	url := fakeProvider(t).URL
	for _, stmt := range []string{
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-1', 'Fake', '` + url + `', '["live-a","custom-1"]')`,
		// Custom-only row against a rejecting endpoint: no group.
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-2', 'Empty', '` + url + `', '[]')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	seedKey(t, sqlDB, "u-1", "k-1")

	rec := do(t, sqlDB, http.MethodGet, "/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Custom "live-a" wins the dedupe; the rejected row contributes
	// nothing.
	want := `{"groups":[{"providerName":"Fake","models":[` +
		`{"id":"custom-1","providerId":"u-1"},` +
		`{"id":"live-a","providerId":"u-1"},` +
		`{"id":"live-z","providerId":"u-1"}]}]}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
}

// Providers sharing a display name merge into one group.
func TestGetModelsMergesGroupsByName(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	url := fakeProvider(t).URL
	for _, stmt := range []string{
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-1', 'OpenAI', '` + url + `', '[]')`,
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-2', 'OpenAI', '` + url + `', '["custom-1","live-z"]')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	seedKey(t, sqlDB, "u-1", "k-1")
	seedKey(t, sqlDB, "u-2", "k-1")

	rec := do(t, sqlDB, http.MethodGet, "/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// "live-a"/"live-z" on both rows: two entries each.
	want := `{"groups":[{"providerName":"OpenAI","models":[` +
		`{"id":"custom-1","providerId":"u-2"},` +
		`{"id":"live-a","providerId":"u-1"},` +
		`{"id":"live-a","providerId":"u-2"},` +
		`{"id":"live-z","providerId":"u-1"},` +
		`{"id":"live-z","providerId":"u-2"}]}]}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
}

// The grouped catalog never leaks keys or base URLs.
func TestGetModelsCarriesNoConfig(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	url := fakeProvider(t).URL
	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-1', 'Fake', '` + url + `', '[]')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	seedKey(t, sqlDB, "u-1", "k-1")
	rec := do(t, sqlDB, http.MethodGet, "/models", "")
	if b := body(rec); strings.Contains(b, `"k-1"`) || strings.Contains(b, url) || strings.Contains(b, "baseUrl") {
		t.Errorf("body leaks connection config: %s", b)
	}
}
