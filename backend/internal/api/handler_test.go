package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/sqlstore"
)

// openHandler boots the full route table over a fresh, migrated
// in-memory database — every endpoint test starts here.
func openHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	sqlDB, err := sqlstore.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("sqlstore.OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return Handler(sqlDB, zerolog.Nop()), sqlDB
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestRoutes(t *testing.T) {
	handler, _ := openHandler(t)

	if rec := do(t, handler, http.MethodPost, "/api/agents"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/agents = %d, want 405", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/nope = %d, want 404", rec.Code)
	}
}
