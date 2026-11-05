package agent

// GetAgentConfig / UpdateAgentConfig handler tests over the real
// store: the seeded bundle, shallow patch semantics, and the system
// identity guard. Route mounting is pinned in backend's tests.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// serve runs one handler behind its route pattern so PathValue
// resolves, and returns the recorder plus the handler's error - nil
// on handled client errors, non-nil on server faults, for api.Log.
func serve(t *testing.T, pattern string, h api.Handler, method, path, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// Lift the pattern's wildcards into the request without going
	// through a real mux.
	pattern = strings.TrimPrefix(pattern, method+" ")
	for i, seg := range strings.Split(pattern, "/") {
		if name, ok := strings.CutPrefix(seg, "{"); ok {
			if name, ok = strings.CutSuffix(name, "}"); ok {
				req.SetPathValue(name, strings.Split(path, "/")[i])
			}
		}
	}
	rec := httptest.NewRecorder()
	return rec, h(rec, req)
}

// configStore opens a fresh store holding one regular agent.
func configStore(t *testing.T) *db.Queries {
	t.Helper()
	queries := db.New(testdb.Open(t))
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return queries
}

func TestGetAgentConfigSeededSuperagent(t *testing.T) {
	queries := db.New(testdb.Open(t))

	rec, err := serve(t, "GET /api/agent/{id}/config", GetAgentConfig(queries),
		http.MethodGet, "/api/agent/_superagent/config", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"id":"_superagent"`, `"name":"Ava"`, `"icon":"message-square"`,
		`"heartbeat":{"model":"auto","enabled":true,"duration":"30m","content":"Check agent heartbeats and flag anything stalled. If nothing needs attention, reply NO_REPLY."}`,
		`"toolsEnabled":[]`, `"userContext":`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s:\n%s", want, body)
		}
	}
}

func TestGetAgentConfigUnknownAgent(t *testing.T) {
	queries := db.New(testdb.Open(t))

	rec, err := serve(t, "GET /api/agent/{id}/config", GetAgentConfig(queries),
		http.MethodGet, "/api/agent/ghost/config", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	want := `{"message":"Agent \"ghost\" not found"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestUpdateAgentConfigAppliesPatch(t *testing.T) {
	queries := configStore(t)

	rec, err := serve(t, "PATCH /api/agent/{id}/config", UpdateAgentConfig(queries),
		http.MethodPatch, "/api/agent/research/config", `{
			"name": "Research Two", "icon": "bot",
			"soul": {"personality": "skeptical", "identity": "checks sources"},
			"operatingInstructions": "verify twice",
			"toolsEnabled": ["fs.read", "browser.browse"]
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"name":"Research Two"`) {
		t.Errorf("body missing renamed name: %s", rec.Body.String())
	}

	row, err := queries.GetAgentConfig(t.Context(), "research")
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "Research Two" || row.Icon != "bot" {
		t.Errorf("identity = %s/%s, want Research Two/bot", row.Name, row.Icon)
	}
	if row.SoulPersonality != "skeptical" || row.SoulIdentity != "checks sources" {
		t.Errorf("soul = %s/%s, want skeptical/checks sources", row.SoulPersonality, row.SoulIdentity)
	}
	if row.OperatingInstructions != "verify twice" {
		t.Errorf("operatingInstructions = %q, want verify twice", row.OperatingInstructions)
	}
	if row.ToolsEnabled != `["fs.read","browser.browse"]` {
		t.Errorf("toolsEnabled = %s, want the JSON array", row.ToolsEnabled)
	}
}

func TestUpdateAgentConfigSoulReplacesWhole(t *testing.T) {
	queries := configStore(t)

	rec, err := serve(t, "PATCH /api/agent/{id}/config", UpdateAgentConfig(queries),
		http.MethodPatch, "/api/agent/research/config", `{"soul":{"personality":"only this"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	row, err := queries.GetAgentConfig(t.Context(), "research")
	if err != nil {
		t.Fatal(err)
	}
	// Shallow contract: a present soul replaces both texts; the
	// client always sends both.
	if row.SoulPersonality != "only this" || row.SoulIdentity != "" {
		t.Errorf("soul = %s/%s, want only this/\"\"", row.SoulPersonality, row.SoulIdentity)
	}
}

func TestUpdateAgentConfigSkipsSystemIdentity(t *testing.T) {
	queries := db.New(testdb.Open(t))

	rec, err := serve(t, "PATCH /api/agent/{id}/config", UpdateAgentConfig(queries),
		http.MethodPatch, "/api/agent/_superagent/config", `{
			"name": "Nope", "icon": "bot",
			"soul": {"personality": "patched", "identity": "patched"}
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"name":"Ava"`) {
		t.Errorf("system row renamed: %s", body)
	}
	if !strings.Contains(body, `"personality":"patched"`) {
		t.Errorf("config patch not applied: %s", body)
	}
}

func TestUpdateAgentConfigEmptyPatchIsNoop(t *testing.T) {
	queries := configStore(t)

	rec, err := serve(t, "PATCH /api/agent/{id}/config", UpdateAgentConfig(queries),
		http.MethodPatch, "/api/agent/research/config", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"name":"Research"`) {
		t.Errorf("body changed on empty patch: %s", rec.Body.String())
	}
}

func TestUpdateAgentConfigUnknownAgent(t *testing.T) {
	queries := db.New(testdb.Open(t))

	rec, err := serve(t, "PATCH /api/agent/{id}/config", UpdateAgentConfig(queries),
		http.MethodPatch, "/api/agent/ghost/config", `{"name":"X"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateAgentConfigMalformedBody(t *testing.T) {
	queries := configStore(t)

	rec, err := serve(t, "PATCH /api/agent/{id}/config", UpdateAgentConfig(queries),
		http.MethodPatch, "/api/agent/research/config", `not-json`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	want := `{"message":"Invalid request body"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
