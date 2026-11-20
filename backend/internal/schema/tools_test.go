// Tools-table rules: the catalog's DB-enforced shape — unique ids,
// preference rows that die with their owner, and the insert rules
// that keep built-ins ON for agents.
package schema_test

import (
	"database/sql"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// seedBuiltinAndAgent inserts one built-in tool and one agent; the
// insert rules seed their preference row.
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

func countAgentGrants(t *testing.T, sqlDB *sql.DB, agentID string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_tool_preferences WHERE agent_id = ?`, agentID).Scan(&n); err != nil {
		t.Fatalf("count grants for %s: %v", agentID, err)
	}
	return n
}

func countToolGrants(t *testing.T, sqlDB *sql.DB, toolID string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM agent_tool_preferences WHERE tool_id = ?`, toolID).Scan(&n); err != nil {
		t.Fatalf("count grants for %s: %v", toolID, err)
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

// A new built-in reaches every agent that already exists — the
// superagent included, no runtime exception.
func TestBuiltinInsertGrantsExistingAgents(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})
	if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
		ID: "ddg", Name: "DuckDuckGo Search", Builtin: 1,
	}); err != nil {
		t.Fatalf("seed builtin: %v", err)
	}

	for _, agent := range []string{"research", "_superagent"} {
		if n := countAgentGrants(t, sqlDB, agent); n != 1 {
			t.Errorf("%s grants = %d, want the new built-in's 1", agent, n)
		}
	}
}

// A user-installed tool grants nothing on insert — default-ON is a
// built-in-only rule.
func TestNonBuiltinInsertGrantsNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	if _, err := sqlDB.ExecContext(t.Context(),
		`INSERT INTO tools (id, name, builtin) VALUES ('custom', 'Custom', 0)`); err != nil {
		t.Fatalf("insert non-builtin: %v", err)
	}
	if n := countToolGrants(t, sqlDB, "custom"); n != 0 {
		t.Errorf("non-builtin grants = %d, want 0", n)
	}
}

// A new agent reaches every installed built-in — globally disabled
// ones included, user-installed ones never.
func TestAgentInsertGrantsInstalledBuiltins(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	for _, seed := range []struct {
		id      string
		builtin int64
	}{{"ddg", 1}, {"dark", 1}, {"custom", 0}} {
		if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
			ID: seed.id, Name: seed.id, Builtin: seed.builtin,
		}); err != nil {
			t.Fatalf("seed tool %q: %v", seed.id, err)
		}
	}
	if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{Enabled: 0, ID: "dark"}); err != nil {
		t.Fatalf("disable dark: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})

	rows, err := sqlDB.QueryContext(t.Context(),
		`SELECT tool_id FROM agent_tool_preferences WHERE agent_id = 'research' ORDER BY tool_id`)
	if err != nil {
		t.Fatalf("list research grants: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan grant: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate grants: %v", err)
	}
	want := []string{"dark", "ddg"}
	if len(got) != len(want) || got[0] != want[0] || got[len(got)-1] != want[len(want)-1] {
		t.Errorf("research grants = %v, want builtins only %v", got, want)
	}
}

// Catalog re-sync takes the upsert's update path: the metadata
// refresh neither seeds nor restores a deleted grant.
func TestCatalogResyncKeepsDeletedGrant(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedBuiltinAndAgent(t, sqlDB, "ddg", "research")
	queries := db.New(sqlDB)

	if _, err := queries.RemoveAgentTool(t.Context(), db.RemoveAgentToolParams{
		AgentID: "research", ToolID: "ddg",
	}); err != nil {
		t.Fatalf("opt out: %v", err)
	}
	if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
		ID: "ddg", Name: "DuckDuckGo Search 2", Builtin: 1,
	}); err != nil {
		t.Fatalf("resync: %v", err)
	}

	if n := countAgentGrants(t, sqlDB, "research"); n != 0 {
		t.Errorf("research grants after resync = %d, want 0 — the opt-out survives", n)
	}
	if n := countAgentGrants(t, sqlDB, "_superagent"); n != 1 {
		t.Errorf("superagent grants after resync = %d, want 1 — untouched grants stay", n)
	}
}

// Preference rows die with their agent — no orphans accumulate.
func TestPreferencesCascadeOnAgentDelete(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedBuiltinAndAgent(t, sqlDB, "ddg", "research")

	if _, err := sqlDB.ExecContext(t.Context(),
		`DELETE FROM agents WHERE id = 'research'`); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	if n := countAgentGrants(t, sqlDB, "research"); n != 0 {
		t.Errorf("preferences after agent delete = %d, want 0", n)
	}
}

// Preference rows die with their tool — a future uninstall leaves
// nothing behind.
func TestPreferencesCascadeOnToolDelete(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedBuiltinAndAgent(t, sqlDB, "ddg", "research")

	if _, err := sqlDB.ExecContext(t.Context(),
		`DELETE FROM tools WHERE id = 'ddg'`); err != nil {
		t.Fatalf("delete tool: %v", err)
	}
	if n := countToolGrants(t, sqlDB, "ddg"); n != 0 {
		t.Errorf("preferences after tool delete = %d, want 0", n)
	}
}
