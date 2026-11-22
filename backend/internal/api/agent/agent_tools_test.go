package agent

// Per-agent tool endpoint tests: the agent's catalog view (global
// switch + own opt-out, plus every server's gate) and the live toggle
// with its burst guard.

import (
	"database/sql"
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
	return seedAgentAndBuiltins(t, testdb.Open(t, true))
}

// seedAgentAndBuiltins seeds one agent and two built-in tools over an
// already-open store.
func seedAgentAndBuiltins(t *testing.T, sqlDB *sql.DB) *db.Queries {
	t.Helper()
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
	// Inheritance: both tools are available to this agent (no opt-out),
	// and the global switch decides availability.
	want := `{"tools":[` +
		`{"id":"ddg","name":"ddg name","description":"","globalEnabled":true,"locallyDisabled":false,"kind":"builtin"},` +
		`{"id":"notes","name":"notes name","description":"","globalEnabled":false,"locallyDisabled":false,"kind":"builtin"}` +
		`],"servers":[]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// Every server is listed with its gate state, absent rows included, so
// the editor can explain a global blocker instead of hiding the group.
func TestGetAgentToolsListsAllServers(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)
	for _, id := range []string{"gated", "ungated"} {
		if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
			ID: id, Name: id,
			Url:        sql.NullString{String: "https://" + id + ".example/mcp", Valid: true},
			HeaderKeys: "[]",
		}); err != nil {
			t.Fatalf("seed server %q: %v", id, err)
		}
	}
	if _, err := queries.SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{
		AgentID: "research", ServerID: "gated", Enabled: 1,
	}); err != nil {
		t.Fatalf("gate: %v", err)
	}

	rec, err := serve(t, "GET /agent/{id}/tools", GetAgentTools(queries),
		http.MethodGet, "/agent/research/tools", "")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSpace(rec.Body.String())
	if !strings.Contains(body, `"servers":[{"serverId":"gated","name":"gated","agentEnabled":true,"serverEnabled":true,"catalogState":"active"},{"serverId":"ungated","name":"ungated","agentEnabled":false,"serverEnabled":true,"catalogState":"active"}]`) {
		t.Fatalf("body = %s, want both servers with their gate state and global blocker facts", body)
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

// Disable writes the opt-out and enable clears it; the view and the
// effective set follow the row's presence.
func TestSetAgentToolAddsAndRemovesOptOut(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)
	throttle := api.NewThrottle(0)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	want := `{"id":"ddg","name":"ddg name","description":"","globalEnabled":true,"locallyDisabled":true,"kind":"builtin"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if ids := resolveIDs(t, queries, "research"); len(ids) != 0 {
		t.Errorf("effective tools after opt-out = %v, want none", ids)
	}

	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want = `{"id":"ddg","name":"ddg name","description":"","globalEnabled":true,"locallyDisabled":false,"kind":"builtin"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if ids := resolveIDs(t, queries, "research"); len(ids) != 1 || ids[0] != "ddg" {
		t.Errorf("effective tools after re-inherit = %v, want [ddg]", ids)
	}
}

// Both directions write one approval-change record, each beside the
// flip it explains: enable commits with its record, disable records
// best-effort after the restriction landed.
func TestSetAgentToolWritesAudit(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)

	for _, step := range []struct {
		body    string
		outcome string
		reason  string
	}{
		{`{"enabled":false}`, "revoke", "locally_disabled"},
		{`{"enabled":true}`, "approve", "locally_enabled"},
	} {
		rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}",
			ToggleAgentTool(api.NewThrottle(0), sqlDB, queries, nil),
			http.MethodPatch, "/agent/research/tools/ddg", step.body)
		if err != nil || rec.Code != http.StatusOK {
			t.Fatalf("toggle %s: err=%v status=%d", step.body, err, rec.Code)
		}
		var decision, reason, state string
		if err := sqlDB.QueryRowContext(t.Context(), `
SELECT decision, reason_code, state FROM tool_audit_events
WHERE event = 'approval_change' ORDER BY id DESC LIMIT 1`).
			Scan(&decision, &reason, &state); err != nil {
			t.Fatalf("read audit: %v", err)
		}
		if decision != step.outcome || reason != step.reason || state != "terminal" {
			t.Fatalf("audit = %s/%s/%s, want %s/%s/terminal", decision, reason, state, step.outcome, step.reason)
		}
	}
}

func TestSetAgentToolRejectsUnknownAgentAndTool(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)
	throttle := api.NewThrottle(0)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/ghost/tools/ddg", `{"enabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: status = %d, want 404", rec.Code)
	}

	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/nope", `{"enabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown tool: status = %d, want 404", rec.Code)
	}
}

func TestSetAgentToolRequiresEnabled(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}",
		ToggleAgentTool(api.NewThrottle(0), sqlDB, queries, nil),
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
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)
	throttle := api.NewThrottle(50 * time.Millisecond)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("first toggle: status = %d, want 200", rec.Code)
	}

	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("burst toggle: status = %d, want 429", rec.Code)
	}

	time.Sleep(60 * time.Millisecond)
	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("post-window toggle: status = %d, want 200", rec.Code)
	}
}

// Inherited availability needs no per-agent row: switching a tool on
// globally reaches an agent whose gate is open without writing any
// preference row, while an agent that opted out stays excluded.
func TestGlobalEnableReachesGatedAgentWithoutWrites(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "other", Name: "Other", Icon: "globe",
	})
	if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID: "gh", Name: "GitHub",
		Url:        sql.NullString{String: "https://example.test/mcp", Valid: true},
		HeaderKeys: "[]",
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := queries.UpsertMcpTool(t.Context(), db.UpsertMcpToolParams{
		ID: "gh:search", Name: "search", Description: "search",
		ServerID: sql.NullString{String: "gh", Valid: true}, DefinitionJson: `{}`,
	}); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
	for _, agent := range []string{"research", "other"} {
		if _, err := queries.SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{
			AgentID: agent, ServerID: "gh", Enabled: 1,
		}); err != nil {
			t.Fatalf("gate %s: %v", agent, err)
		}
	}
	// research opts out of the tool; other does not.
	if err := queries.AddAgentDisabledTool(t.Context(), db.AddAgentDisabledToolParams{
		AgentID: "research", ToolID: "gh:search",
	}); err != nil {
		t.Fatalf("opt out: %v", err)
	}

	// A fresh MCP tool is globally off: neither agent resolves it.
	for _, agent := range []string{"research", "other"} {
		if ids := resolveAgent(t, queries, agent); contains(ids, "gh:search") {
			t.Fatalf("%s resolves a globally disabled tool: %v", agent, ids)
		}
	}

	// The global switch reaches the non-opted-out agent only, and writes
	// no preference row.
	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 1, ID: "gh:search",
	}); err != nil {
		t.Fatalf("enable tool: %v", err)
	}
	if ids := resolveAgent(t, queries, "other"); !contains(ids, "gh:search") {
		t.Fatalf("other resolves %v, want the globally enabled tool", ids)
	}
	if ids := resolveAgent(t, queries, "research"); contains(ids, "gh:search") {
		t.Fatalf("research resolves %v, want the opted-out tool excluded", ids)
	}
	var prefRows int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_disabled_tools WHERE tool_id = 'gh:search'`).Scan(&prefRows); err != nil {
		t.Fatalf("count opt-outs: %v", err)
	}
	if prefRows != 1 {
		t.Fatalf("opt-out rows = %d, want only research's", prefRows)
	}
}

// Clearing an opt-out whose record cannot be written persists nothing:
// no activation goes unrecorded.
func TestClearingOptOutAuditFailureBlocks(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := seedAgentAndBuiltins(t, sqlDB)
	throttle := api.NewThrottle(0)

	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":false}`)
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("opt out: err=%v status=%d", err, rec.Code)
	}
	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	rec, err = serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/ddg", `{"enabled":true}`)
	if err == nil {
		t.Fatal("unauditable re-inherit returned no error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, rec.Body.String())
	}
	if ids := resolveIDs(t, queries, "research"); len(ids) != 0 {
		t.Fatalf("unaudited re-inherit resolved: %v", ids)
	}
}

// resolveAgent lists one agent's effective tool ids.
func resolveAgent(t *testing.T, queries *db.Queries, agentID string) []string {
	t.Helper()
	rows, err := queries.ResolveAgentToolsDetailed(t.Context(), db.ResolveAgentToolsDetailedParams{
		AgentID: agentID, AgentID_2: agentID,
	})
	if err != nil {
		t.Fatalf("resolve %s: %v", agentID, err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// contains reports whether ids holds one entry.
func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
