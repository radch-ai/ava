// MCP-server rules: the DB-enforced shape of server-owned catalog rows
// -- cascades that uninstall a server completely, discovery sync and
// prune semantics, and the two switches (global, per-agent gate) that
// decide turn resolution.
package schema_test

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// schemaTestKey seals the discovery fence's vault reads in these tests.
var schemaTestKey = []byte("0123456789abcdef0123456789abcdef")

// seedMcpServer inserts one server row; the id is the slug. Approvals
// stay off — these tests toggle them per scenario.
func seedMcpServer(t *testing.T, sqlDB *sql.DB, id string) {
	t.Helper()
	testdb.SeedMcpServer(t, sqlDB, id, "https://"+id+".example/mcp",
		func(arg *db.CreateMcpServerParams) {
			arg.AllowPrivateNetwork = 0
			arg.AllowPlaintext = 0
		})
}

// upsertMcpTool lands one server-owned catalog row through the
// discovery sync path (mcpserver.UpsertListing) -- composite id
// {slug}:{remoteName}. The production writer is the only path: the
// insert's SQL constants live there, not in a test twin.
func upsertMcpTool(t *testing.T, sqlDB *sql.DB, id, serverID string) {
	t.Helper()
	row, err := db.New(sqlDB).GetMcpServer(t.Context(), serverID)
	if err != nil {
		t.Fatalf("read server %q: %v", serverID, err)
	}
	master, err := vault.NewMasterAEAD(schemaTestKey)
	if err != nil {
		t.Fatalf("master AEAD: %v", err)
	}
	store, err := vault.NewStore(sqlDB, master)
	if err != nil {
		t.Fatalf("vault store: %v", err)
	}
	cfg := mcp.ServerConfig{
		ID:                  serverID,
		URL:                 row.Url.String,
		AllowPrivateNetwork: row.AllowPrivateNetwork != 0,
		AllowPlaintext:      row.AllowPlaintext != 0,
	}
	_, remote, _ := strings.Cut(id, ":")
	// The listing unions the server's existing rows: a discovery pass
	// prunes what it does not report, and this helper only ever adds.
	queries := db.New(sqlDB)
	rows, err := queries.ListMcpServerTools(t.Context(), sql.NullString{String: serverID, Valid: true})
	if err != nil {
		t.Fatalf("list %q tools: %v", serverID, err)
	}
	listed := make([]mcp.ListedTool, 0, len(rows)+1)
	for _, existing := range rows {
		if existing.ID == id {
			continue // replaced by the entry below
		}
		listed = append(listed, mcp.ListedTool{
			Remote: existing.Name, Description: existing.Description,
			DefinitionJSON: existing.DefinitionJson,
		})
	}
	listed = append(listed, mcp.ListedTool{Remote: remote, Description: "", DefinitionJSON: "{}"})
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, serverID, cfg, listed); err != nil {
		t.Fatalf("upsert mcp tool %q: %v", id, err)
	}
}

// enableTool flips one tool's global switch on.
func enableTool(t *testing.T, sqlDB *sql.DB, toolID string) {
	t.Helper()
	if _, err := db.New(sqlDB).ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 1, ID: toolID,
	}); err != nil {
		t.Fatalf("enable %s: %v", toolID, err)
	}
}

// resolveIDs reads the turn's effective tool set for one agent.
func resolveIDs(t *testing.T, sqlDB *sql.DB, agentID string) []string {
	t.Helper()
	rows, err := db.New(sqlDB).ResolveAgentToolsDetailed(t.Context(), agentID)
	if err != nil {
		t.Fatalf("resolve agent tools: %v", err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// Deleting a server uninstalls it completely: its catalog rows and,
// through them, every agent opt-out -- two cascade hops.
func TestServerDeleteCascadesToolsAndOptOuts(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "github:issues", "github")

	for _, tool := range []string{"github:search", "github:issues"} {
		disableTool(t, sqlDB, "a1", tool)
	}

	if n, err := queries.DeleteMcpServer(t.Context(), "github"); err != nil || n != 1 {
		t.Fatalf("delete server: rows=%d err=%v", n, err)
	}
	if n, err := queries.CountMcpServerTools(t.Context(), sql.NullString{String: "github", Valid: true}); err != nil || n != 0 {
		t.Errorf("server tools survived delete: count=%d err=%v", n, err)
	}
	if isDisabled(t, sqlDB, "a1", "github:search") || isDisabled(t, sqlDB, "a1", "github:issues") {
		t.Error("opt-outs survived server delete")
	}
	if countAgentOptOuts(t, sqlDB, "a1") != 0 {
		t.Errorf("opt-outs left behind: %d, want 0", countAgentOptOuts(t, sqlDB, "a1"))
	}
	if _, err := queries.GetMcpServer(t.Context(), "github"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("server row survived delete: err=%v", err)
	}
	if countAgentMcps(t, sqlDB, "a1") != 0 {
		t.Error("per-agent server gates survived delete")
	}
}

// Pruning vanished tools removes their rows -- and the opt-outs die
// with them; an empty kept-list prunes everything.
func TestVanishedToolPruneCascadesOptOuts(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "github:issues", "github")
	for _, tool := range []string{"github:search", "github:issues"} {
		disableTool(t, sqlDB, "a1", tool)
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
	if isDisabled(t, sqlDB, "a1", "github:issues") {
		t.Error("pruned tool's opt-out survived")
	}
	if !isDisabled(t, sqlDB, "a1", "github:search") {
		t.Error("kept tool's opt-out was disturbed")
	}

	if n := prune(`[]`); n != 1 {
		t.Fatalf("final prune removed %d rows, want 1", n)
	}
	if isDisabled(t, sqlDB, "a1", "github:search") {
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
	enableTool(t, sqlDB, "github:search")
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

// approveAgentMcp turns the per-agent parent gate on without touching
// any tool choice: configured, choices kept.
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

// The per-agent parent gate gates resolution: a globally enabled tool
// is not enough -- an agent with no gate row resolves none of the
// server's tools, and the agent's own opt-out survives every gate
// toggle (a gate never writes a tool choice).
func TestResolveHonorsParentGate(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	upsertMcpTool(t, sqlDB, "github:issues", "github")
	enableTool(t, sqlDB, "github:search")
	enableTool(t, sqlDB, "github:issues")

	// Gate absent: the server's tools resolve to nothing.
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg"}; !slices.Equal(got, want) {
		t.Fatalf("resolve without a gate row: %v, want %v", got, want)
	}

	// Gate on: both globally enabled tools inherit, no per-tool rows.
	approveAgentMcp(t, sqlDB, "a1", "github")
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg", "github:issues", "github:search"}; !slices.Equal(got, want) {
		t.Fatalf("resolve with gate on: %v, want %v", got, want)
	}
	// One tool opted out, then gate off/on: the opt-out stays.
	disableTool(t, sqlDB, "a1", "github:issues")
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
		t.Fatalf("resolve after gate re-enable: %v, want %v (the opt-out must survive)", got, want)
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

// A paused catalog resolves nothing while its rows stay
// listed for inspection -- and an unpause restores availability.
func TestPausedCatalogResolvesNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, sqlDB, "github")
	seedBuiltinAndAgent(t, sqlDB, "ddg", "a1")
	upsertMcpTool(t, sqlDB, "github:search", "github")
	enableTool(t, sqlDB, "github:search")
	approveAgentMcp(t, sqlDB, "a1", "github")

	if _, err := queries.SetMcpServerState(t.Context(), db.SetMcpServerStateParams{
		CatalogState: "paused", ID: "github",
	}); err != nil {
		t.Fatalf("pause catalog: %v", err)
	}
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg"}; !slices.Equal(got, want) {
		t.Fatalf("resolve with a paused catalog: %v, want %v", got, want)
	}
	if n, err := queries.CountMcpServerTools(t.Context(), sql.NullString{String: "github", Valid: true}); err != nil || n != 1 {
		t.Fatalf("paused catalog lost its rows: count=%d err=%v", n, err)
	}
	if _, err := queries.SetMcpServerState(t.Context(), db.SetMcpServerStateParams{
		CatalogState: "active", ID: "github",
	}); err != nil {
		t.Fatalf("unpause catalog: %v", err)
	}
	if got, want := resolveIDs(t, sqlDB, "a1"), []string{"ddg", "github:search"}; !slices.Equal(got, want) {
		t.Fatalf("resolve after unpause: %v, want %v", got, want)
	}
}

// The catalog state column is enum-checked: an unknown state cannot be
// stored, so no reader has to guess what it means.
func TestMcpServerCatalogStateChecked(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedMcpServer(t, sqlDB, "github")
	if _, err := db.New(sqlDB).SetMcpServerState(t.Context(), db.SetMcpServerStateParams{
		CatalogState: "broken", ID: "github",
	}); err == nil {
		t.Error("catalog_state 'broken' accepted, want CHECK rejection")
	}
}

// Destination authority and catalog admission default to deny/active:
// a fresh row grants no private-network or plaintext exception and
// starts with an active catalog.
func TestMcpServerPolicyDefaults(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedMcpServer(t, sqlDB, "github")
	row, err := db.New(sqlDB).GetMcpServer(t.Context(), "github")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if row.CatalogState != "active" {
		t.Fatalf("fresh row catalog_state = %q, want active", row.CatalogState)
	}
	if row.AllowPrivateNetwork != 0 || row.AllowPlaintext != 0 {
		t.Fatalf("fresh row flags = %d/%d, want 0/0 (default deny)",
			row.AllowPrivateNetwork, row.AllowPlaintext)
	}
}
