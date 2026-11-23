package backend_test

// The effective-tool predicate has one definition — the
// agent_effective_tools view — and this test is its migration guard:
// both view-backed queries (the turn's resolution and the
// per-invocation authorization fold) must report the seeded oracle's
// set across every toggle permutation.

import (
	"context"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// matrixResolution runs every combination that matters for one MCP tool
// plus the builtin: the global switch, the server switch, the parent
// gate, and the agent's opt-out.
func TestEnablementEncodingsAgree(t *testing.T) {
	type perm struct {
		name          string
		toolOn        bool
		serverOn      bool
		gateOn        bool
		optedOut      bool
		wantEffective bool
	}
	perms := []perm{
		{"all on", true, true, true, false, true},
		{"tool off", false, true, true, false, false},
		{"server off", true, false, true, false, false},
		{"gate off", true, true, false, false, false},
		{"opted out", true, true, true, true, false},
	}
	for _, p := range perms {
		t.Run(p.name, func(t *testing.T) {
			sqlDB := testdb.Open(t, true)
			queries := db.New(sqlDB)
			ctx := context.Background()
			testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a1", Name: "A", Icon: "globe"})
			testdb.SeedMcpServer(t, sqlDB, "srv", "")
			if _, err := sqlDB.Exec(
				`INSERT INTO tools (id, name, description, enabled, kind, server_id, definition_json)
				 VALUES ('srv:t', 't', '', ?, 'mcp', 'srv', '{}')`, boolInt(p.toolOn)); err != nil {
				t.Fatalf("seed tool: %v", err)
			}
			if _, err := sqlDB.Exec(
				`INSERT INTO tools (id, name, description, kind, definition_json)
				 VALUES ('ddg', 'ddg', '', 'builtin', '{}')`); err != nil {
				t.Fatalf("seed builtin: %v", err)
			}
			if _, err := queries.ToggleMcpServerEnabled(ctx, db.ToggleMcpServerEnabledParams{
				Enabled: boolInt(p.serverOn), ID: "srv",
			}); err != nil {
				t.Fatalf("server switch: %v", err)
			}
			if p.gateOn {
				if _, err := queries.SetAgentMcpEnabled(ctx, db.SetAgentMcpEnabledParams{
					AgentID: "a1", ServerID: "srv", Enabled: 1,
				}); err != nil {
					t.Fatalf("gate: %v", err)
				}
			}
			if p.optedOut {
				if err := queries.AddAgentDisabledTool(ctx, db.AddAgentDisabledToolParams{
					AgentID: "a1", ToolID: "srv:t",
				}); err != nil {
					t.Fatalf("opt-out: %v", err)
				}
			}

			// The turn's resolution: it lists exactly the effective set.
			rows, err := queries.ResolveAgentToolsDetailed(ctx, "a1")
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			resolved := map[string]bool{}
			for _, row := range rows {
				resolved[row.ID] = true
			}

			// The per-invocation fold: it answers effective for the same
			// set, folded from the view's columns.
			authorized := map[string]bool{}
			for _, id := range []string{"srv:t", "ddg"} {
				auth, err := queries.AuthorizeToolCall(ctx, db.AuthorizeToolCallParams{
					AgentID: "a1", ID: id,
				})
				if err != nil {
					t.Fatalf("authorize %s: %v", id, err)
				}
				authorized[id] = auth.ToolEnabled == 1 &&
					(!auth.ServerID.Valid || (auth.ServerEnabled == 1 && auth.CatalogState == "active" && auth.GateOn == 1)) &&
					!auth.LocallyDisabled
			}

			// The builtin rides the global switch alone and is on in every
			// permutation here.
			if !resolved["ddg"] || !authorized["ddg"] {
				t.Fatalf("builtin: resolved=%v authorized=%v, want unconditionally effective", resolved["ddg"], authorized["ddg"])
			}
			if resolved["srv:t"] != p.wantEffective || authorized["srv:t"] != p.wantEffective {
				t.Fatalf("mcp tool: resolved=%v authorized=%v, want %v",
					resolved["srv:t"], authorized["srv:t"], p.wantEffective)
			}
		})
	}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
