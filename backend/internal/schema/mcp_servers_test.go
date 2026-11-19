// MCP-server rules: the DB-enforced shape of server-owned catalog rows
// -- cascades that uninstall a server completely, the default-deny
// install rule, discovery sync and prune semantics, and the server
// kill switch's effect on turn resolution.
package schema_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// seedMcpServer inserts one stdio server row; the id is the slug.
func seedMcpServer(t *testing.T, sqlDB *sql.DB, id string) {
	t.Helper()
	if err := db.New(sqlDB).CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID: id, Name: id, Transport: "stdio",
		Command: sql.NullString{String: "go", Valid: true},
		Args:    "[]", EnvKeys: "[]", HeaderKeys: "[]",
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
}

// upsertMcpTool lands one server-owned catalog row via the discovery
// sync path -- composite id {slug}:{remoteName}.
func upsertMcpTool(t *testing.T, sqlDB *sql.DB, id, serverID string) {
	t.Helper()
	if err := db.New(sqlDB).UpsertMcpTool(t.Context(), db.UpsertMcpToolParams{
		ID: id, Name: id, Description: "",
		ServerID: sql.NullString{String: serverID, Valid: true},
	}); err != nil {
		t.Fatalf("upsert mcp tool: %v", err)
	}
}

func hasGrant(t *testing.T, sqlDB *sql.DB, agentID, toolID string) bool {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_tool_preferences WHERE agent_id = ? AND tool_id = ?`,
		agentID, toolID).Scan(&n); err != nil {
		t.Fatalf("grant lookup %s/%s: %v", agentID, toolID, err)
	}
	return n > 0
}

func resolveIDs(t *testing.T, sqlDB *sql.DB, agentID string) []string {
	t.Helper()
	ids, err := db.New(sqlDB).ResolveAgentTools(t.Context(), db.ResolveAgentToolsParams{
		AgentID: agentID, AgentID_2: agentID,
	})
	if err != nil {
		t.Fatalf("resolve agent tools: %v", err)
	}
	return ids
}

// Deleting a server uninstalls it completely: its catalog rows and,
// through them, every grant they held -- two cascade hops.
func TestServerDeleteCascadesToolsAndGrants(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "github:issues", "github")

	for _, tool := range []string{"github:search", "github:issues"} {
		if _, err := queries.AddAgentTool(t.Context(), db.AddAgentToolParams{AgentID: "a1", ToolID: tool}); err != nil {
			t.Fatalf("grant %s: %v", tool, err)
		}
	}

	if n, err := queries.DeleteMcpServer(t.Context(), "github"); err != nil || n != 1 {
		t.Fatalf("delete server: rows=%d err=%v", n, err)
	}
	if n, err := queries.CountMcpServerTools(t.Context(), sql.NullString{String: "github", Valid: true}); err != nil || n != 0 {
		t.Errorf("server tools survived delete: count=%d err=%v", n, err)
	}
	if hasGrant(t, sqlDB, "a1", "github:search") || hasGrant(t, sqlDB, "a1", "github:issues") {
		t.Error("grants survived server delete")
	}
	if countAgentGrants(t, sqlDB, "a1") != 1 { // the builtin grant stays
		t.Errorf("builtin grant disturbed: %d, want 1", countAgentGrants(t, sqlDB, "a1"))
	}
	if _, err := queries.GetMcpServer(t.Context(), "github"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("server row survived delete: err=%v", err)
	}
	if countAgentMcps(t, sqlDB, "a1") != 0 {
		t.Error("per-agent server gates survived delete")
	}
}

// An MCP tool installs granted to no one -- default-deny; only
// built-ins seed grants, and discovery inserts carry builtin=0.
func TestMcpToolInsertGrantsNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1") // agent exists pre-install
	upsertMcpTool(t, sqlDB, "github:search", "github")

	if hasGrant(t, sqlDB, "a1", "github:search") {
		t.Error("mcp tool granted on insert, want default-deny")
	}
	if ids := resolveIDs(t, sqlDB, "a1"); slices.Contains(ids, "github:search") {
		t.Errorf("un-granted mcp tool resolves: %v", ids)
	}
}

// Discovery re-sync takes the upsert's update path: it refreshes
// metadata but neither seeds nor restores a deleted grant.
func TestMcpToolResyncKeepsDeletedGrant(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	if _, err := queries.AddAgentTool(t.Context(), db.AddAgentToolParams{AgentID: "a1", ToolID: "github:search"}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := queries.RemoveAgentTool(t.Context(), db.RemoveAgentToolParams{AgentID: "a1", ToolID: "github:search"}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	upsertMcpTool(t, sqlDB, "github:search", "github") // re-discovered
	if hasGrant(t, sqlDB, "a1", "github:search") {
		t.Error("re-sync restored a deleted grant")
	}
}

// Pruning vanished tools removes their rows -- and the grants die
// with them; an empty kept-list prunes everything.
func TestVanishedToolPruneCascadesGrants(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "github:issues", "github")
	for _, tool := range []string{"github:search", "github:issues"} {
		if _, err := queries.AddAgentTool(t.Context(), db.AddAgentToolParams{AgentID: "a1", ToolID: tool}); err != nil {
			t.Fatalf("grant %s: %v", tool, err)
		}
	}

	prune := func(kept string) int64 {
		t.Helper()
		n, err := queries.DeleteVanishedMcpTools(t.Context(), db.DeleteVanishedMcpToolsParams{
			ServerID: sql.NullString{String: "github", Valid: true}, KeptIds: kept,
		})
		if err != nil {
			t.Fatalf("prune: %v", err)
		}
		return n
	}

	if n := prune(`["github:search"]`); n != 1 {
		t.Fatalf("prune removed %d rows, want 1", n)
	}
	if hasGrant(t, sqlDB, "a1", "github:issues") {
		t.Error("pruned tool's grant survived")
	}
	if !hasGrant(t, sqlDB, "a1", "github:search") {
		t.Error("kept tool's grant was disturbed")
	}

	if n := prune(`[]`); n != 1 {
		t.Fatalf("final prune removed %d rows, want 1", n)
	}
	if hasGrant(t, sqlDB, "a1", "github:search") {
		t.Error("empty kept-list left a tool behind")
	}
}

// The server kill switch gates turn resolution: a disabled server's
// tools resolve to nothing while built-ins stay live -- same
// inertness as the global tool switch, one join.
func TestDisabledServerResolvesNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	if _, err := queries.AddAgentTool(t.Context(), db.AddAgentToolParams{AgentID: "a1", ToolID: "github:search"}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	approveAgentMcp(t, sqlDB, "a1", "github")

	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg", "github:search"}; !slices.Equal(got, want) {
		t.Fatalf("resolve before toggle: %v, want %v", got, want)
	}

	if _, err := queries.ToggleMcpServerEnabled(t.Context(), db.ToggleMcpServerEnabledParams{Enabled: 0, ID: "github"}); err != nil {
		t.Fatalf("disable server: %v", err)
	}
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg"}; !slices.Equal(got, want) {
		t.Fatalf("resolve while disabled: %v, want %v", got, want)
	}

	if _, err := queries.ToggleMcpServerEnabled(t.Context(), db.ToggleMcpServerEnabledParams{Enabled: 1, ID: "github"}); err != nil {
		t.Fatalf("enable server: %v", err)
	}
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg", "github:search"}; !slices.Equal(got, want) {
		t.Fatalf("resolve after re-enable: %v, want %v", got, want)
	}
}

// Transport is enum-checked at the DB level: stdio and http are the
// only supported MCP transports.
func TestMcpServerTransportChecked(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)

	if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID: "legacy", Name: "legacy", Transport: "sse",
	}); err == nil {
		t.Error("transport 'sse' accepted, want CHECK rejection")
	}
	if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID: "remote", Name: "remote", Transport: "http", Args: "[]",
		EnvKeys: "[]", HeaderKeys: "[]",
	}); err != nil {
		t.Errorf("transport 'http' rejected: %v", err)
	}
}

// approveAgentMcp stamps the per-agent parent gate (enabled) without
// seeding grants -- configured, choices kept.
func approveAgentMcp(t *testing.T, sqlDB *sql.DB, agentID, serverID string) {
	t.Helper()
	if _, err := db.New(sqlDB).SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{
		AgentID: agentID, ServerID: serverID, Enabled: 1,
	}); err != nil {
		t.Fatalf("approve mcp %s/%s: %v", agentID, serverID, err)
	}
}

func countAgentMcps(t *testing.T, sqlDB *sql.DB, agentID string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_mcp_preferences WHERE agent_id = ?`, agentID).Scan(&n); err != nil {
		t.Fatalf("mcp gate count %s: %v", agentID, err)
	}
	return n
}

// The per-agent parent gate gates resolution: a grant alone is
// not enough -- an agent that never approved the server resolves none
// of its tools; a configured-off gate resolves none either; turning
// the gate back on restores exactly the grants that existed, never
// re-seeding.
func TestResolveHonorsParentGate(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "github:issues", "github")
	for _, tool := range []string{"github:search", "github:issues"} {
		if _, err := queries.AddAgentTool(t.Context(), db.AddAgentToolParams{AgentID: "a1", ToolID: tool}); err != nil {
			t.Fatalf("grant %s: %v", tool, err)
		}
	}

	// Never approved: grants exist, gate row does not.
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg"}; !slices.Equal(got, want) {
		t.Fatalf("resolve without a gate row: %v, want %v", got, want)
	}

	// Approved, one tool revoked, then gate off/on: the revoked tool
	// must stay revoked (no re-seeding on a toggle).
	approveAgentMcp(t, sqlDB, "a1", "github")
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg", "github:issues", "github:search"}; !slices.Equal(got, want) {
		t.Fatalf("resolve with gate on: %v, want %v", got, want)
	}
	if _, err := queries.RemoveAgentTool(t.Context(), db.RemoveAgentToolParams{AgentID: "a1", ToolID: "github:issues"}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := queries.SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{AgentID: "a1", ServerID: "github", Enabled: 0}); err != nil {
		t.Fatalf("gate off: %v", err)
	}
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg"}; !slices.Equal(got, want) {
		t.Fatalf("resolve with gate off: %v, want %v", got, want)
	}
	if _, err := queries.SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{AgentID: "a1", ServerID: "github", Enabled: 1}); err != nil {
		t.Fatalf("gate on: %v", err)
	}
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg", "github:search"}; !slices.Equal(got, want) {
		t.Fatalf("resolve after gate re-enable: %v, want %v (revoked tool must stay revoked)", got, want)
	}
}

// First-enable seeding approves exactly the server's current
// catalog rows: a second server and a builtin are untouched.
func TestSeedAgentMcpToolsCoversCurrentListingOnly(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedMcpServer(t, sqlDB, "gitlab")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "github:issues", "github")
	upsertMcpTool(t, sqlDB, "gitlab:projects", "gitlab")

	if _, err := queries.SeedAgentMcpTools(t.Context(), db.SeedAgentMcpToolsParams{
		AgentID: "a1", ServerID: sql.NullString{String: "github", Valid: true},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !hasGrant(t, sqlDB, "a1", "github:search") || !hasGrant(t, sqlDB, "a1", "github:issues") {
		t.Error("first enable did not grant the server's tools")
	}
	if hasGrant(t, sqlDB, "a1", "gitlab:projects") {
		t.Error("first enable crossed into another server")
	}
}

// Deleting an agent takes its parent gates with it.
func TestAgentDeleteCascadesMcpGates(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	approveAgentMcp(t, sqlDB, "a1", "github")

	if _, err := sqlDB.ExecContext(t.Context(), `DELETE FROM agents WHERE id = 'a1'`); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	if countAgentMcps(t, sqlDB, "a1") != 0 {
		t.Error("gates survived agent delete")
	}
}

// The upgrade derivation (the birth migration's backfill statement)
// turns pre-existing grants into an approved gate without expanding
// anything: grants stay untouched, ungranted servers stay ungranted.
func TestBackfillDerivesGateFromGrants(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedMcpServer(t, sqlDB, "github")
	seedMcpServer(t, sqlDB, "gitlab")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "gitlab:projects", "gitlab")
	if _, err := db.New(sqlDB).AddAgentTool(t.Context(), db.AddAgentToolParams{AgentID: "a1", ToolID: "github:search"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// The migration's statement, verbatim (pin).
	if _, err := sqlDB.ExecContext(t.Context(), `
INSERT OR IGNORE INTO agent_mcp_preferences (agent_id, server_id, enabled)
SELECT DISTINCT p.agent_id, t.server_id, 1
FROM agent_tool_preferences p
JOIN tools t ON t.id = p.tool_id
WHERE t.server_id IS NOT NULL`); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var enabled int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT enabled FROM agent_mcp_preferences WHERE agent_id = 'a1' AND server_id = 'github'`).Scan(&enabled); err != nil {
		t.Fatalf("backfilled gate missing: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("backfilled gate enabled=%d, want 1", enabled)
	}
	if n := countAgentMcps(t, sqlDB, "a1"); n != 1 {
		t.Fatalf("backfill created %d gates, want 1 (ungranted servers stay ungranted)", n)
	}
}
