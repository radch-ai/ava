package backend_test

// AgentTools pins the lane's per-turn fetch: the registry executables
// an agent may use — globally enabled built-ins, inherited unless the
// agent opted out — matched by id, plus the boot sync that must never
// reset a saved global switch or drop an agent's opt-out.

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// toolsDB boots a backend — real registry, real sync — and one agent
// row.
func toolsDB(t *testing.T, ephemeral bool) (*sql.DB, *db.Queries, *backend.TurnService) {
	t.Helper()
	sqlDB := testdb.Open(t, ephemeral)
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	queries := db.New(sqlDB)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})
	return sqlDB, queries, svc
}

// builtinID reads the one catalog row the boot sync wrote.
func builtinID(t *testing.T, queries *db.Queries) string {
	t.Helper()
	rows, err := queries.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("catalog rows = %d, want the single built-in", len(rows))
	}
	return rows[0].ID
}

// fetchTools returns the catalog tool names AgentTools handed the
// lane; the internal read_tool_output is filtered out so the catalog
// pins below assert exactly the catalog.
func fetchTools(t *testing.T, svc *backend.TurnService, agentID string) []string {
	t.Helper()
	resolved, err := svc.AgentTools(context.Background(), agentID)
	if err != nil {
		t.Fatalf("AgentTools: %v", err)
	}
	var names []string
	for _, name := range toolNames(resolved) {
		if name != "read_tool_output" {
			names = append(names, name)
		}
	}
	return names
}

// Built-ins default ON and inherit: a fresh agent's first turn already
// sees the built-in with no per-agent row written.
func TestAgentToolsDefaultOnForFreshAgent(t *testing.T) {
	_, _, svc := toolsDB(t, true)

	if names := fetchTools(t, svc, "research"); len(names) != 1 {
		t.Errorf("fresh agent fetched = %v, want the default-granted built-in", names)
	}
}

// The global switch is the outer gate: OFF blocks every agent until it
// is turned back on.
func TestAgentToolsRespectsGlobalSwitch(t *testing.T) {
	_, queries, svc := toolsDB(t, true)
	id := builtinID(t, queries)

	// The switch alone decides: nothing is written per agent.
	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{Enabled: 0, ID: id}); err != nil {
		t.Fatalf("disable %s: %v", id, err)
	}
	if names := fetchTools(t, svc, "research"); len(names) != 0 {
		t.Errorf("fetched while globally off = %v, want none", names)
	}

	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{Enabled: 1, ID: id}); err != nil {
		t.Fatalf("re-enable %s: %v", id, err)
	}
	if names := fetchTools(t, svc, "research"); len(names) != 1 || names[0] != id {
		t.Errorf("fetched after re-enable = %v, want [%s]", names, id)
	}
}

// A catalog row the registry no longer knows yields nothing — the fetch
// is the intersection of SQL permissions and executables.
func TestAgentToolsUnknownCatalogRowYieldsNothing(t *testing.T) {
	_, queries, svc := toolsDB(t, true)
	id := builtinID(t, queries)

	// A tools-table row with no executable behind it — the registry
	// shrunk between boots.
	if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
		ID: "ghost_tool", Name: "Ghost", Description: "removed from code",
	}); err != nil {
		t.Fatalf("seed ghost row: %v", err)
	}
	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 1, ID: "ghost_tool",
	}); err != nil {
		t.Fatalf("enable ghost_tool: %v", err)
	}
	if names := fetchTools(t, svc, "research"); len(names) != 1 || names[0] != id {
		t.Errorf("fetched = %v, want only the built-in [%s] — ghost_tool yields nothing", names, id)
	}
}

// An explicit opt-out removes the inherited tool, and repeating it is a
// harmless no-op.
func TestAgentToolsOptOutDropsTool(t *testing.T) {
	_, queries, svc := toolsDB(t, true)
	id := builtinID(t, queries)

	if err := queries.AddAgentDisabledTool(t.Context(), db.AddAgentDisabledToolParams{
		AgentID: "research", ToolID: id,
	}); err != nil {
		t.Fatalf("opt out %s: %v", id, err)
	}
	if names := fetchTools(t, svc, "research"); len(names) != 0 {
		t.Errorf("fetched after opt-out = %v, want none", names)
	}

	if err := queries.AddAgentDisabledTool(t.Context(), db.AddAgentDisabledToolParams{
		AgentID: "research", ToolID: id,
	}); err != nil {
		t.Fatalf("opt out again: %v", err)
	}
	if names := fetchTools(t, svc, "research"); len(names) != 0 {
		t.Errorf("fetched after repeated opt-out = %v, want none", names)
	}
}

// Boot sync must never reset a saved global switch: the second boot
// over the same file keeps the disabled state and refreshes metadata.
func TestSyncPreservesSavedGlobalSwitch(t *testing.T) {
	sqlDB, queries, _ := toolsDB(t, false)
	id := builtinID(t, queries)

	before, err := queries.GetTool(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{Enabled: 0, ID: id}); err != nil {
		t.Fatalf("disable %s: %v", id, err)
	}

	// Reboot over the same file: metadata refreshes, the switch
	// stays OFF.
	if _, _, _, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits()); err != nil {
		t.Fatalf("second boot: %v", err)
	}
	after, err := db.New(sqlDB).GetTool(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Enabled != 0 {
		t.Errorf("enabled after reboot = %d, want 0", after.Enabled)
	}
	if after.Name != before.Name {
		t.Errorf("name after reboot = %s, want refreshed %s", after.Name, before.Name)
	}
}

// Restart re-syncs the catalog: the upsert's update path refreshes
// metadata without restoring an agent's opt-out, and an untouched agent
// keeps inheriting.
func TestSyncDoesNotRestoreOptOut(t *testing.T) {
	sqlDB, queries, _ := toolsDB(t, false)
	id := builtinID(t, queries)

	if err := queries.AddAgentDisabledTool(t.Context(), db.AddAgentDisabledToolParams{
		AgentID: "research", ToolID: id,
	}); err != nil {
		t.Fatalf("opt out %s: %v", id, err)
	}

	if _, _, _, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits()); err != nil {
		t.Fatalf("second boot: %v", err)
	}
	queries = db.New(sqlDB)
	if rows, err := queries.ResolveAgentToolsDetailed(t.Context(), "research"); err != nil {
		t.Fatal(err)
	} else if len(rows) != 0 {
		t.Errorf("research tools after reboot = %v, want none — the opt-out survives", rows)
	}
	if rows, err := queries.ResolveAgentToolsDetailed(t.Context(), "_superagent"); err != nil {
		t.Fatal(err)
	} else if len(rows) != 1 || rows[0].ID != id {
		t.Errorf("superagent tools after reboot = %v, want [%s]", rows, id)
	}
}

// read_tool_output is the read side of /shake's elision: an internal
// tool, not a catalog row — assembled beside the effective ids for
// every agent, never user-managed, and present even when the catalog
// resolves nothing, so a placeholder is never stranded.
func TestAgentToolsIncludesReadToolOutput(t *testing.T) {
	_, queries, svc := toolsDB(t, true)

	resolved, err := svc.AgentTools(context.Background(), "research")
	if err != nil {
		t.Fatalf("AgentTools: %v", err)
	}
	if !hasTool(resolved, "read_tool_output") {
		t.Fatalf("fresh agent fetched = %v, want read_tool_output offered internally", toolNames(resolved))
	}

	// Exactly one definition rides, and it is never catalog-managed:
	// a synced row of the same name would hand the model two
	// same-name definitions.
	if got := strings.Count(strings.Join(toolNames(resolved), ","), "read_tool_output"); got != 1 {
		t.Fatalf("read_tool_output offered %d times = %v, want exactly one", got, toolNames(resolved))
	}
	catalog, err := queries.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list catalog: %v", err)
	}
	for _, row := range catalog {
		if row.Name == "read_tool_output" {
			t.Fatal("read_tool_output must never be a catalog row")
		}
	}

	// The global switch is the outer gate for every catalog tool; the
	// internal tool stays offered behind it, so an elided context
	// keeps its restore path.
	id := builtinID(t, queries)
	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{Enabled: 0, ID: id}); err != nil {
		t.Fatalf("disable %s: %v", id, err)
	}
	resolved, err = svc.AgentTools(context.Background(), "research")
	if err != nil {
		t.Fatalf("AgentTools behind the switch: %v", err)
	}
	if !hasTool(resolved, "read_tool_output") {
		t.Fatalf("catalog-empty fetch = %v, want read_tool_output still offered", toolNames(resolved))
	}
}

// hasTool reports whether one resolved tool id is present.
func hasTool(resolved backend.TurnTools, id string) bool {
	return slices.Contains(toolNames(resolved), id)
}

// toolNames lists the resolved tool ids.
func toolNames(resolved backend.TurnTools) []string {
	out := make([]string, 0, len(resolved.Infos))
	for _, info := range resolved.Infos {
		out = append(out, info.Name)
	}
	return out
}

// The internal tool resolved by AgentTools is the scoped read itself:
// one call restores this agent's elided original, and another agent's
// reference answers as an ordinary unknown-reference result.
func TestAgentToolsReadToolOutputInvokesScopedRead(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	original := strings.Repeat("x", 3000)

	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, original, nil), // research-row-2
	})
	if _, err := sqlDB.Exec(
		`UPDATE thread_messages SET elided = 1 WHERE agent_id = 'research' AND id = 'research-row-2'`); err != nil {
		t.Fatalf("elide seed row: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "other", Name: "Other", Icon: "globe"})
	seedRows(t, sqlDB, "other", []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, original, nil), // other-row-2
	})
	if _, err := sqlDB.Exec(
		`UPDATE thread_messages SET elided = 1 WHERE agent_id = 'other' AND id = 'other-row-2'`); err != nil {
		t.Fatalf("elide other row: %v", err)
	}

	resolved, err := turns.AgentTools(ctx, "research")
	if err != nil {
		t.Fatalf("AgentTools: %v", err)
	}
	var impl tool.InvokableTool
	for i, info := range resolved.Infos {
		if info.Name == "read_tool_output" {
			impl = resolved.Impls[i]
		}
	}
	if impl == nil {
		t.Fatalf("resolved = %v, want read_tool_output", toolNames(resolved))
	}

	got, err := impl.InvokableRun(ctx, `{"reference":"research-row-2"}`)
	if err != nil || got != original {
		t.Fatalf("own reference = %d bytes, %v; want the stored original", len(got), err)
	}
	got, err = impl.InvokableRun(ctx, `{"reference":"other-row-2"}`)
	if err != nil || got != "read_tool_output: unknown tool output reference" {
		t.Fatalf("cross-agent reference = %q, %v; want the unknown-reference result", got, err)
	}

	// A malformed argument is the tool's own result, never an error:
	// the model corrects its call instead of losing the turn.
	for args, want := range map[string]string{
		``:                  "decode arguments",
		`null`:              `"reference" is required`,
		`{}`:                `"reference" is required`,
		`{"reference":123}`: "decode arguments",
		`not json`:          "decode arguments",
	} {
		got, err := impl.InvokableRun(ctx, args)
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("args %q = %q, %v; want result text containing %q", args, got, err, want)
		}
	}
}
