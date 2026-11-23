package agent

// Per-agent MCP gate endpoint tests: the gate decides whether the agent
// uses a server's globally enabled tools — it never seeds or revokes a
// per-tool choice — and the tools listing carries the gate state for the
// editor.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// mcpStore seeds one agent, one MCP server and two of its catalog
// tools, switched on globally and inherited by no one yet (the gate is
// off).
func mcpStore(t *testing.T) (*db.Queries, *sql.DB) {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})
	testdb.SeedMcpServer(t, sqlDB, "github", "https://github.example/mcp")
	for _, id := range []string{"github:search", "github:issues"} {
		if _, err := sqlDB.Exec(
			`INSERT INTO tools (id, name, description, enabled, kind, server_id, definition_json) VALUES (?, ?, '', 0, 'mcp', 'github', '{}')`,
			id, strings.TrimPrefix(id, "github:")); err != nil {
			t.Fatalf("seed tool %q: %v", id, err)
		}
		if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
			Enabled: 1, ID: id,
		}); err != nil {
			t.Fatalf("enable tool %q: %v", id, err)
		}
	}
	return queries, sqlDB
}

func decodeTools(t *testing.T, rec *json.RawMessage) apitypes.GetAgentToolsResponse {
	t.Helper()
	var out apitypes.GetAgentToolsResponse
	if err := json.Unmarshal(*rec, &out); err != nil {
		t.Fatalf("decode tools response: %v", err)
	}
	return out
}

func resolveIDs(t *testing.T, queries *db.Queries, agentID string) []string {
	t.Helper()
	rows, err := queries.ResolveAgentToolsDetailed(t.Context(), agentID)
	if err != nil {
		t.Fatalf("resolve tools: %v", err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func toggleMcp(t *testing.T, queries *db.Queries, sqlDB *sql.DB, enabled bool) *apitypes.GetAgentToolsResponse {
	t.Helper()
	body := `{"enabled":false}`
	if enabled {
		body = `{"enabled":true}`
	}
	rec, err := serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/mcp/github", body)
	if err != nil {
		t.Fatalf("toggle mcp: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle mcp status = %d: %s", rec.Code, rec.Body.String())
	}
	raw := json.RawMessage(rec.Body.String())
	out := decodeTools(t, &raw)
	return &out
}

// Enable (no gate row) inherits the server's globally enabled tools
// and writes no per-tool row at all.
func TestAgentMcpEnableInheritsTools(t *testing.T) {
	queries, sqlDB := mcpStore(t)

	if got := resolveIDs(t, queries, "research"); len(got) != 0 {
		t.Fatalf("effective tools before enable = %v, want none", got)
	}

	out := toggleMcp(t, queries, sqlDB, true)
	if got, want := resolveIDs(t, queries, "research"), []string{"github:issues", "github:search"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("effective tools after first enable = %v, want %v", got, want)
	}
	if len(out.Servers) != 1 || out.Servers[0].ServerID != "github" || !out.Servers[0].AgentEnabled {
		t.Fatalf("servers in response = %+v, want github enabled", out.Servers)
	}
	var rows int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_disabled_tools WHERE agent_id = 'research'`).Scan(&rows); err != nil {
		t.Fatalf("opt-out count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("first enable wrote %d per-tool rows, want 0 (inheritance)", rows)
	}
}

// Off -> on flips only the gate: an opt-out recorded while the gate was
// on survives every toggle. Off answers with the gates array still
// present and agentEnabled false.
func TestAgentMcpTogglePreservesChoices(t *testing.T) {
	queries, sqlDB := mcpStore(t)
	toggleMcp(t, queries, sqlDB, true)

	// Opt out of one tool through the per-tool endpoint.
	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/tools/github:issues", `{"enabled":false}`)
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("opt out: err=%v status=%d", err, rec.Code)
	}

	off := toggleMcp(t, queries, sqlDB, false)
	if len(off.Servers) != 1 || off.Servers[0].AgentEnabled {
		t.Fatalf("servers after gate off = %+v, want github disabled", off.Servers)
	}
	if got := resolveIDs(t, queries, "research"); len(got) != 0 {
		t.Fatalf("effective tools with gate off = %v, want none", got)
	}

	on := toggleMcp(t, queries, sqlDB, true)
	if len(on.Servers) != 1 || !on.Servers[0].AgentEnabled {
		t.Fatalf("servers after gate on = %+v, want github enabled", on.Servers)
	}
	if got, want := resolveIDs(t, queries, "research"), []string{"github:search"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("re-enable changed tool choices: %v, want %v", got, want)
	}
	byID := map[string]bool{}
	for _, tool := range on.Tools {
		byID[tool.ID] = tool.LocallyDisabled
	}
	if byID["github:search"] || !byID["github:issues"] {
		t.Fatalf("listing opt-outs after re-enable = %v, want search on, issues opted out", byID)
	}
}

// The listing carries the gate state so the editor renders server
// switches from one fetch.
func TestGetAgentToolsIncludesServers(t *testing.T) {
	queries, sqlDB := mcpStore(t)
	toggleMcp(t, queries, sqlDB, true)

	rec, err := serve(t, "GET /agent/{id}/tools", GetAgentTools(queries),
		http.MethodGet, "/agent/research/tools", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	raw := json.RawMessage(rec.Body.String())
	out := decodeTools(t, &raw)
	if len(out.Servers) != 1 || out.Servers[0].ServerID != "github" || !out.Servers[0].AgentEnabled {
		t.Fatalf("servers = %+v, want github enabled", out.Servers)
	}
}

func TestAgentMcpRejectsUnknownAndMissingFields(t *testing.T) {
	queries, sqlDB := mcpStore(t)

	rec, err := serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/mcp/ghost", `{"enabled":true}`)
	if err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("unknown server: err=%v status=%d, want 404", err, rec.Code)
	}

	rec, err = serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/mcp/github", `{}`)
	if err != nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled: err=%v status=%d, want 400", err, rec.Code)
	}
}

// An enable whose approval record cannot be written activates
// nothing — no gate row, no per-tool writes.
func TestEnableAuditFailureBlocks(t *testing.T) {
	queries, sqlDB := mcpStore(t)
	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	rec, err := serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/mcp/github", `{"enabled":true}`)
	if err == nil {
		t.Fatal("unauditable first enable returned no error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, rec.Body.String())
	}
	var gateRows int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_mcp_preferences WHERE agent_id = 'research' AND server_id = 'github'`).Scan(&gateRows); err != nil || gateRows != 0 {
		t.Fatalf("unaudited enable left a gate row: rows=%d err %v", gateRows, err)
	}
	var rows int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_disabled_tools WHERE agent_id = 'research'`).Scan(&rows); err != nil {
		t.Fatalf("opt-out count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("unaudited enable wrote %d per-tool rows, want 0", rows)
	}
}

// Re-opening a configured gate is new authority: an unauditable
// record activates nothing — the gate stays closed and choices stay.
func TestReEnableAuditFailureBlocks(t *testing.T) {
	queries, sqlDB := mcpStore(t)
	toggleMcp(t, queries, sqlDB, true)  // gate on
	toggleMcp(t, queries, sqlDB, false) // gate off; choices kept

	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	rec, err := serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/agent/research/mcp/github", `{"enabled":true}`)
	if err == nil {
		t.Fatal("unauditable re-enable returned no error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var gateEnabled int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT enabled FROM agent_mcp_preferences WHERE agent_id = 'research' AND server_id = 'github'`).Scan(&gateEnabled); err != nil || gateEnabled != 0 {
		t.Fatalf("gate after failed re-enable = %d err %v, want 0", gateEnabled, err)
	}
	if got := resolveIDs(t, queries, "research"); len(got) != 0 {
		t.Fatalf("effective tools after blocked re-enable = %v, want none", got)
	}
}
