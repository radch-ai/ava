package agent

// Per-agent MCP gate endpoint tests: the first enable approves the
// server's current tools, later toggles preserve per-tool choices,
// and the tools listing carries the gate state for the editor.

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
// tools, granted to no one.
func mcpStore(t *testing.T) (*db.Queries, *sql.DB) {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})
	if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID: "github", Name: "GitHub", Transport: "stdio",
		Command: sql.NullString{String: "go", Valid: true},
		Args:    "[]", EnvKeys: "[]", HeaderKeys: "[]",
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	for _, id := range []string{"github:search", "github:issues"} {
		if err := queries.UpsertMcpTool(t.Context(), db.UpsertMcpToolParams{
			ID: id, Name: strings.TrimPrefix(id, "github:"), Description: "",
			ServerID: sql.NullString{String: "github", Valid: true},
		}); err != nil {
			t.Fatalf("seed tool %q: %v", id, err)
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
	ids, err := queries.ResolveAgentTools(t.Context(), db.ResolveAgentToolsParams{
		AgentID: agentID, AgentID_2: agentID,
	})
	if err != nil {
		t.Fatalf("resolve tools: %v", err)
	}
	return ids
}

func toggleMcp(t *testing.T, queries *db.Queries, sqlDB *sql.DB, enabled bool) *apitypes.GetAgentToolsResponse {
	t.Helper()
	body := `{"enabled":false}`
	if enabled {
		body = `{"enabled":true}`
	}
	rec, err := serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries),
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

// First enable (no gate row) approves exactly the server's current
// tools and turns the listing switches on.
func TestAgentMcpFirstEnableSeedsTools(t *testing.T) {
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
	var granted int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_tool_preferences WHERE agent_id = 'research'`).Scan(&granted); err != nil {
		t.Fatalf("grant count: %v", err)
	}
	if granted != 2 {
		t.Fatalf("first enable granted %d tools, want 2", granted)
	}
}

// Off -> on flips only the gate: a tool revoked while the gate was on
// stays revoked. Off answers with the gates array still present,
// agentEnabled false, and tool rows still listing their grants.
func TestAgentMcpTogglePreservesChoices(t *testing.T) {
	queries, sqlDB := mcpStore(t)
	toggleMcp(t, queries, sqlDB, true)

	// Revoke one tool through the per-tool endpoint.
	rec, err := serve(t, "PATCH /agent/{id}/tools/{toolId}", ToggleAgentTool(api.NewThrottle(0), queries),
		http.MethodPatch, "/agent/research/tools/github:issues", `{"enabled":false}`)
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("revoke: err=%v status=%d", err, rec.Code)
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
		byID[tool.ID] = tool.AgentEnabled
	}
	if !byID["github:search"] || byID["github:issues"] {
		t.Fatalf("listing switches after re-enable = %v, want search on, issues off", byID)
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

	rec, err := serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries),
		http.MethodPatch, "/agent/research/mcp/ghost", `{"enabled":true}`)
	if err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("unknown server: err=%v status=%d, want 404", err, rec.Code)
	}

	rec, err = serve(t, "PATCH /agent/{id}/mcp/{serverId}", ToggleAgentMcp(api.NewThrottle(0), sqlDB, queries),
		http.MethodPatch, "/agent/research/mcp/github", `{}`)
	if err != nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled: err=%v status=%d, want 400", err, rec.Code)
	}
}
