package backend_test

// Routing pins over New's route table; endpoint behavior is pinned in
// each endpoint package's own tests.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestRoutes(t *testing.T) {
	handler := backend.New(testdb.Open(t), zerolog.Nop())

	do := func(method, path string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}

	if code := do(http.MethodPost, "/api/agents"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/agents = %d, want 405", code)
	}
	if code := do(http.MethodGet, "/api/nope"); code != http.StatusNotFound {
		t.Errorf("GET /api/nope = %d, want 404", code)
	}
}
