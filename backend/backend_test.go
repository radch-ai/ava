package backend_test

// Routing pins over New's route table; endpoint behavior is pinned in
// each endpoint package's own tests.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestRoutes(t *testing.T) {
	handler := backend.New(testdb.Open(t), zerolog.Nop())

	do := func(method, path, body string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code
	}

	if code := do(http.MethodPost, "/api/agents", ""); code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/agents = %d, want 405", code)
	}
	if code := do(http.MethodGet, "/api/agents", ""); code != http.StatusOK {
		t.Errorf("GET /api/agents = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/api/nope", ""); code != http.StatusNotFound {
		t.Errorf("GET /api/nope = %d, want 404", code)
	}
	// The save surface's per-agent routes.
	if code := do(http.MethodGet, "/api/agent/_superagent/config", ""); code != http.StatusOK {
		t.Errorf("GET /api/agent/_superagent/config = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/api/agent/ghost/config", ""); code != http.StatusNotFound {
		t.Errorf("GET /api/agent/ghost/config = %d, want 404", code)
	}
	if code := do(http.MethodPatch, "/api/agent/_superagent/config", "{}"); code != http.StatusOK {
		t.Errorf("PATCH /api/agent/_superagent/config = %d, want 200", code)
	}
	if code := do(http.MethodPatch, "/api/agent/_superagent/heartbeat",
		`{"model":"auto","enabled":true,"duration":"30m","content":"x"}`); code != http.StatusOK {
		t.Errorf("PATCH /api/agent/_superagent/heartbeat = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/api/agent/_superagent/cron-jobs", ""); code != http.StatusOK {
		t.Errorf("GET /api/agent/_superagent/cron-jobs = %d, want 200", code)
	}
	if code := do(http.MethodGet, "/api/agent/ghost/cron-jobs", ""); code != http.StatusNotFound {
		t.Errorf("GET /api/agent/ghost/cron-jobs = %d, want 404", code)
	}
	if code := do(http.MethodDelete, "/api/agent/_superagent/cron-jobs/nope", ""); code != http.StatusNotFound {
		t.Errorf("DELETE /api/agent/_superagent/cron-jobs/nope = %d, want 404", code)
	}
}
