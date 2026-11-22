// Tools-table rules: the catalog's DB-enforced shape — unique ids,
// opt-out rows that die with their owner, and the insert rule that keeps
// built-ins ON while a discovered MCP tool lands globally off.
package schema_test

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// seedBuiltinAndAgent inserts one built-in tool and one agent.
func seedBuiltinAndAgent(t *testing.T, sqlDB *sql.DB, toolID, agentID string) {
	t.Helper()
	queries := db.New(sqlDB)
	if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
		ID: toolID, Name: toolID, Description: "", Builtin: 1,
	}); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: agentID, Name: "Agent", Icon: "globe",
	})
}

// disableTool records one agent's explicit opt-out.
func disableTool(t *testing.T, sqlDB *sql.DB, agentID, toolID string) {
	t.Helper()
	if err := db.New(sqlDB).AddAgentDisabledTool(t.Context(), db.AddAgentDisabledToolParams{
		AgentID: agentID, ToolID: toolID,
	}); err != nil {
		t.Fatalf("disable %s/%s: %v", agentID, toolID, err)
	}
}

func isDisabled(t *testing.T, sqlDB *sql.DB, agentID, toolID string) bool {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_disabled_tools WHERE agent_id = ? AND tool_id = ?`,
		agentID, toolID).Scan(&n); err != nil {
		t.Fatalf("opt-out lookup %s/%s: %v", agentID, toolID, err)
	}
	return n > 0
}

func countAgentOptOuts(t *testing.T, sqlDB *sql.DB, agentID string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_disabled_tools WHERE agent_id = ?`, agentID).Scan(&n); err != nil {
		t.Fatalf("count opt-outs for %s: %v", agentID, err)
	}
	return n
}

func countToolOptOuts(t *testing.T, sqlDB *sql.DB, toolID string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_disabled_tools WHERE tool_id = ?`, toolID).Scan(&n); err != nil {
		t.Fatalf("count opt-outs for %s: %v", toolID, err)
	}
	return n
}

// A raw duplicate insert hits the primary key — the catalog never
// holds two rows for one id.
func TestToolIDDuplicateInsertRejected(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	if _, err := sqlDB.ExecContext(t.Context(),
		`INSERT INTO tools (id, name) VALUES ('ddg', 'a')`); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := sqlDB.ExecContext(t.Context(),
		`INSERT INTO tools (id, name) VALUES ('ddg', 'b')`); err == nil {
		t.Error("duplicate id accepted, want primary-key rejection")
	}
}

// Built-ins install globally ON: the column default is the only switch
// a shipped tool needs.
func TestBuiltinInsertDefaultsOn(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	if err := db.New(sqlDB).UpsertTool(t.Context(), db.UpsertToolParams{
		ID: "ddg", Name: "DuckDuckGo Search", Builtin: 1,
	}); err != nil {
		t.Fatalf("seed builtin: %v", err)
	}
	row, err := db.New(sqlDB).GetTool(t.Context(), "ddg")
	if err != nil {
		t.Fatalf("get tool: %v", err)
	}
	if row.Enabled != 1 {
		t.Errorf("builtin enabled = %d, want 1", row.Enabled)
	}
}

// A discovered MCP tool lands globally OFF: its switch is opted in, and
// a re-sync of the same tool never touches the saved switch.
func TestMcpToolInsertStartsGloballyOff(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	if err := queries.UpsertMcpTool(t.Context(), db.UpsertMcpToolParams{
		ID: "github:search", Name: "search", ServerID: sql.NullString{String: "github", Valid: true},
	}); err != nil {
		t.Fatalf("insert mcp tool: %v", err)
	}
	row, err := queries.GetTool(t.Context(), "github:search")
	if err != nil {
		t.Fatalf("get mcp tool: %v", err)
	}
	if row.Enabled != 0 {
		t.Fatalf("new mcp tool enabled = %d, want 0", row.Enabled)
	}
	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 1, ID: "github:search",
	}); err != nil {
		t.Fatalf("enable mcp tool: %v", err)
	}
	if err := queries.UpsertMcpTool(t.Context(), db.UpsertMcpToolParams{
		ID: "github:search", Name: "search-v2", Description: "newer",
		ServerID: sql.NullString{String: "github", Valid: true},
	}); err != nil {
		t.Fatalf("re-sync mcp tool: %v", err)
	}
	row, err = queries.GetTool(t.Context(), "github:search")
	if err != nil {
		t.Fatalf("get re-synced tool: %v", err)
	}
	if row.Enabled != 1 {
		t.Errorf("re-sync reset the saved switch: enabled = %d, want 1", row.Enabled)
	}
	if row.Name != "search-v2" {
		t.Errorf("re-sync kept stale metadata: name = %q, want search-v2", row.Name)
	}
}

// Inheritance is the default: a globally enabled builtin resolves with
// zero per-agent rows, and an explicit opt-out is the only thing that
// removes it.
func TestResolveInheritsUntilOptOut(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")

	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg"}; !slices.Equal(got, want) {
		t.Fatalf("resolve with no per-agent rows: %v, want %v", got, want)
	}

	disableTool(t, sqlDB, "a1", "ddg")
	if got := resolveIDs(t, sqlDB, "a1"); len(got) != 0 {
		t.Fatalf("resolve with an opt-out: %v, want none", got)
	}

	if err := db.New(sqlDB).RemoveAgentDisabledTool(t.Context(), db.RemoveAgentDisabledToolParams{
		AgentID: "a1", ToolID: "ddg",
	}); err != nil {
		t.Fatalf("re-inherit: %v", err)
	}
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg"}; !slices.Equal(got, want) {
		t.Fatalf("resolve after re-inherit: %v, want %v", got, want)
	}
}

// Opt-out rows die with their agent — no orphans accumulate.
func TestOptOutsCascadeOnAgentDelete(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedBuiltinAndAgent(t, sqlDB, "ddg", "research")
	disableTool(t, sqlDB, "research", "ddg")

	if _, err := sqlDB.ExecContext(t.Context(),
		`DELETE FROM agents WHERE id = 'research'`); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	if n := countAgentOptOuts(t, sqlDB, "research"); n != 0 {
		t.Errorf("opt-outs after agent delete = %d, want 0", n)
	}
}

// Opt-out rows die with their tool — a future uninstall leaves nothing
// behind.
func TestOptOutsCascadeOnToolDelete(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedBuiltinAndAgent(t, sqlDB, "ddg", "research")
	disableTool(t, sqlDB, "research", "ddg")

	if _, err := sqlDB.ExecContext(t.Context(),
		`DELETE FROM tools WHERE id = 'ddg'`); err != nil {
		t.Fatalf("delete tool: %v", err)
	}
	if n := countToolOptOuts(t, sqlDB, "ddg"); n != 0 {
		t.Errorf("opt-outs after tool delete = %d, want 0", n)
	}
}
