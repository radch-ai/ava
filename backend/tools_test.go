package backend_test

// AgentTools pins the lane's per-turn fetch: the registry executables
// an agent may use — globally enabled built-ins, inherited unless the
// agent opted out — matched by id, plus the boot sync that must never
// reset a saved global switch or drop an agent's opt-out.

import (
	"context"
	"database/sql"
	"testing"

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

// fetchTools returns the tool names AgentTools handed the lane.
func fetchTools(t *testing.T, svc *backend.TurnService, agentID string) []string {
	t.Helper()
	impls, err := svc.AgentTools(context.Background(), agentID)
	if err != nil {
		t.Fatalf("AgentTools: %v", err)
	}
	var names []string
	for _, impl := range impls {
		info, err := impl.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		names = append(names, info.Name)
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
	if rows, err := queries.ResolveAgentToolsDetailed(t.Context(), db.ResolveAgentToolsDetailedParams{AgentID: "research", AgentID_2: "research"}); err != nil {
		t.Fatal(err)
	} else if len(rows) != 0 {
		t.Errorf("research tools after reboot = %v, want none — the opt-out survives", rows)
	}
	if rows, err := queries.ResolveAgentToolsDetailed(t.Context(), db.ResolveAgentToolsDetailedParams{AgentID: "_superagent", AgentID_2: "_superagent"}); err != nil {
		t.Fatal(err)
	} else if len(rows) != 1 || rows[0].ID != id {
		t.Errorf("superagent tools after reboot = %v, want [%s]", rows, id)
	}
}
