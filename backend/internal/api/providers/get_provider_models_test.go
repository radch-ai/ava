package providers

// GET /api/providers/{id}/models: one provider's system model ids.

import (
	"net/http"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// A keyed provider serves its live list; a keyless provider serves
// the same live list against a no-auth endpoint and an empty list —
// not null — when the endpoint rejects it.
func TestGetProviderModelsUserProvider(t *testing.T) {
	sqlDB := testdb.Open(t)
	url := fakeProvider(t).URL
	for _, stmt := range []string{
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-1', 'Fake', '` + url + `', '["custom-1"]')`,
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-2', 'Local', '` + noAuthURL(t) + `', '[]')`,
		`INSERT INTO providers (id, name, base_url, models)
		 VALUES ('u-3', 'Rejected', '` + url + `', '[]')`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	seedKey(t, sqlDB, "u-1", "k-1")

	rec := do(t, sqlDB, http.MethodGet, "/providers/u-1/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// System ids only — custom ids are not repeated here.
	want := `{"systemModels":["live-a","live-z"]}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	rec = do(t, sqlDB, http.MethodGet, "/providers/u-2/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := body(rec); got != `{"systemModels":["local-m1","local-m2"]}` {
		t.Errorf("body = %s, want the no-auth live list", got)
	}

	rec = do(t, sqlDB, http.MethodGet, "/providers/u-3/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := body(rec); got != `{"systemModels":[]}` {
		t.Errorf("body = %s, want empty list", got)
	}
}

func TestGetProviderModelsNotFound(t *testing.T) {
	sqlDB := testdb.Open(t)
	rec := do(t, sqlDB, http.MethodGet, "/providers/nope/models", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	want := `{"message":"Provider \"nope\" not found"}`
	if got := body(rec); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
