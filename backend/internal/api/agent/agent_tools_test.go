package agent

// Per-agent tool endpoint tests: the agent's catalog view (global
// switch + own preference) and the live toggle with its burst guard.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// agentToolsStore seeds one agent and two tools: ddg (globally on)
// and notes (globally off).
func agentToolsStore(t *testing.T) *db.Queries {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})
	for _, seed := range []struct {
		id      string
		enabled int64
	}{{"ddg", 1}, {"notes", 0}} {
		if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
			ID: seed.id, Name: seed.id + " name", Description: "", Builtin: 1,
		}); err != nil {
			t.Fatalf("seed tool %q: %v", seed.id, err)
		}
		if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
			Enabled: seed.enabled, ID: seed.id,
		}); err != nil {
			t.Fatalf("seed switch %q: %v", seed.id, err)
		}
	}
	return queries
}

func TestGetAgentToolsShowsBothSwitches(t *testing.T) {
	queries := agentToolsStore(t)

	rec, err := serve(t, "GET /agent/{id}/tools", GetAgentTools(queries),
		http.MethodGet, "/agent/research/tools", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Built-ins default ON: the insert rules seeded grants for both
	// tools — the globally disabled one included; its switch, not
	// its grants, gates use.
	want := `{"tools":[` +
		`{"id":"ddg","name":"ddg name","description":"","globalEnabled":true,"agentEnabled":true},` +
		`{"id":"notes","name":"notes name","description":"","globalEnabled":false,"agentEnabled":true}` +
		`]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestGetAgentToolsUnknownAgent(t *testing.T) {
	queries := agentToolsStore(t)

	rec, err := serve(t, "GET /agent/{id}/tools", GetAgentTools(queries),
		http.MethodGet, "/agent/ghost/tools", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Enable adds the row, disable removes it; the view and the
// effective set follow the row's presence.
func TestSetAgentToolAddsAndRemovesRow(t *testing.T) {
	queries := agentToolsStore(t)
	throttle := api.NewThrottle(0)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, queries),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	want := `{"id":"ddg","name":"ddg name","description":"","globalEnabled":true,"agentEnabled":true}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	ids, err := queries.ResolveAgentTools(t.Context(), "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "ddg" {
		t.Errorf("effective tools after enable = %v, want [ddg]", ids)
	}

	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, queries),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want = `{"id":"ddg","name":"ddg name","description":"","globalEnabled":true,"agentEnabled":false}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	ids, err = queries.ResolveAgentTools(t.Context(), "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("effective tools after disable = %v, want none", ids)
	}
}

func TestSetAgentToolRejectsUnknownAgentAndTool(t *testing.T) {
	queries := agentToolsStore(t)
	throttle := api.NewThrottle(0)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, queries),
		http.MethodPatch, "/agent/ghost/tools/ddg", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: status = %d, want 404", rec.Code)
	}

	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, queries),
		http.MethodPatch, "/agent/research/tools/nope", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown tool: status = %d, want 404", rec.Code)
	}
}

func TestSetAgentToolRequiresEnabled(t *testing.T) {
	queries := agentToolsStore(t)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(api.NewThrottle(0), queries),
		http.MethodPatch, "/agent/research/tools/ddg", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	want := `{"message":"enabled is required"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// A burst on one target draws a 429; the next toggle passes once the
// window elapses.
func TestSetAgentToolThrottlesBursts(t *testing.T) {
	queries := agentToolsStore(t)
	throttle := api.NewThrottle(50 * time.Millisecond)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, queries),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("first toggle: status = %d, want 200", rec.Code)
	}

	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, queries),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("burst toggle: status = %d, want 429", rec.Code)
	}

	time.Sleep(60 * time.Millisecond)
	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, queries),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("post-window toggle: status = %d, want 200", rec.Code)
	}
}
