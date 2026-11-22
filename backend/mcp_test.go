package backend_test

// MCP boot discovery pins the wiring: registry routing, catalog
// upsert/prune, globally-off discovery, diagnostics. The server is the
// in-process streamable-HTTP fixture (backend/internal/mcptest).

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcptest"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// bootBackend boots over an already-seeded database; every boot owns its
// MCP clients, so each one registers its own teardown.
func bootBackend(t *testing.T, sqlDB *sql.DB) *backend.TurnService {
	t.Helper()
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	t.Cleanup(svc.CloseTools)
	return svc
}

// seedMcpServer inserts one HTTP server row pointing at url; the fixture
// listens on loopback, so the destination approval is part of the row.
func seedMcpServer(t *testing.T, queries *db.Queries, id, url string, timeoutMs int64) {
	t.Helper()
	if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID:         id,
		Name:       "Test " + id,
		Url:        sql.NullString{String: url, Valid: true},
		HeaderKeys: "[]",
		TimeoutMs:  sql.NullInt64{Int64: timeoutMs, Valid: true},

		AllowPrivateNetwork: 1,
		AllowPlaintext:      1,
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
}

// A live server's tools land in the catalog globally off, run for the
// agent once switched on and gated, keep that state through a re-sync
// prune, and die with the service on CloseTools.
func TestMcpBootDiscoveryLiveServer(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	ctx := t.Context()
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "research", Name: "Research", Icon: "globe"})
	srv := mcptest.Start(t)
	seedMcpServer(t, queries, "helper", srv.URL, 5000)

	svc := bootBackend(t, sqlDB)

	// Discovery upserted the server's tools as MCP catalog rows.
	echo, err := queries.GetTool(ctx, "helper:echo")
	if err != nil {
		t.Fatalf("get helper:echo: %v", err)
	}
	if echo.Kind != "mcp" || echo.ServerID != "helper" {
		t.Fatalf("tool row = kind %q server %v", echo.Kind, echo.ServerID)
	}

	// The new tool is globally off; the agent inherits it once the
	// switch is on, with no per-agent row of its own.
	if echo.Enabled != 0 {
		t.Fatal("mcp tool installed globally on")
	}
	row, err := queries.GetAgentTool(ctx, db.GetAgentToolParams{AgentID: "research", ID: "helper:echo"})
	if err != nil {
		t.Fatalf("agent view: %v", err)
	}
	if row.LocallyDisabled {
		t.Fatal("fresh tool holds a per-agent opt-out")
	}

	// Switch it on and open the agent's server gate (resolution needs
	// both), then the turn's fetch runs it under the composite name.
	if _, err := queries.ToggleToolEnabled(ctx, db.ToggleToolEnabledParams{
		Enabled: 1, ID: "helper:echo",
	}); err != nil {
		t.Fatalf("enable tool: %v", err)
	}
	if _, err := queries.SetAgentMcpEnabled(ctx, db.SetAgentMcpEnabledParams{
		AgentID: "research", ServerID: "helper", Enabled: 1,
	}); err != nil {
		t.Fatalf("approve mcp gate: %v", err)
	}
	impls, err := svc.AgentTools(ctx, "research")
	if err != nil {
		t.Fatalf("agent tools: %v", err)
	}
	var found tool.InvokableTool
	for _, impl := range impls {
		info, err := impl.Info(ctx)
		if err != nil {
			t.Fatalf("info: %v", err)
		}
		if info.Name == "helper_echo" {
			found = impl
		}
	}
	if found == nil {
		t.Fatalf("enabled mcp tool not in the turn set: %d tools", len(impls))
	}
	out, err := found.InvokableRun(ctx, `{"text":"boot round"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// A single text block arrives as text, not JSON-enveloped.
	if out != "boot round" {
		t.Fatalf("result = %q, want the bare text", out)
	}

	// Diagnostics: healthy sync recorded, no error.
	server, err := queries.GetMcpServer(ctx, "helper")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.LastError.Valid {
		t.Fatalf("healthy sync recorded error %q", server.LastError.String)
	}
	if !server.RefreshedAt.Valid {
		t.Fatal("refreshed_at not set after sync")
	}

	// Prune: a row the server no longer reports cascades away with its
	// agent opt-out on the next boot's sync.
	if err := queries.UpsertMcpTool(ctx, db.UpsertMcpToolParams{
		ID: "helper:ghost", Name: "ghost", Description: "stale",
		ServerID: sql.NullString{String: "helper", Valid: true},
	}); err != nil {
		t.Fatalf("seed stale tool: %v", err)
	}
	if err := queries.AddAgentDisabledTool(ctx, db.AddAgentDisabledToolParams{
		AgentID: "research", ToolID: "helper:ghost",
	}); err != nil {
		t.Fatalf("opt out of stale tool: %v", err)
	}
	svc.CloseTools()
	bootBackend(t, sqlDB)

	if _, err := queries.GetTool(ctx, "helper:ghost"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale tool survived prune: %v", err)
	}
	tools, err := queries.ListAgentTools(ctx, "research")
	if err != nil {
		t.Fatalf("list agent tools: %v", err)
	}
	for _, at := range tools {
		if at.ID == "helper:ghost" {
			t.Fatal("stale tool still in the agent's list after prune")
		}
	}
}

// A dead server never fails boot: its row explains itself, and no tools
// are listed.
func TestMcpBootSurvivesDeadServer(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, queries, "dead", "http://127.0.0.1:1/mcp", 300)

	bootBackend(t, sqlDB)

	server, err := queries.GetMcpServer(t.Context(), "dead")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if !server.LastError.Valid || !strings.Contains(server.LastError.String, "mcp server dead") {
		t.Fatalf("last_error = %v, want the connect failure", server.LastError)
	}
	count, err := queries.CountMcpServerTools(t.Context(), "dead")
	if err != nil {
		t.Fatalf("count tools: %v", err)
	}
	if count != 0 {
		t.Fatalf("dead server listed %d tools", count)
	}
}

// The kill switch skips a disabled server entirely: no connect, no
// discovery, no diagnostics write.
func TestMcpDisabledServerNeverAttempted(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	seedMcpServer(t, queries, "off", srv.URL, 5000)
	if _, err := queries.ToggleMcpServerEnabled(t.Context(), db.ToggleMcpServerEnabledParams{Enabled: 0, ID: "off"}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	bootBackend(t, sqlDB)

	server, err := queries.GetMcpServer(t.Context(), "off")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.RefreshedAt.Valid {
		t.Fatal("disabled server was discovered at boot")
	}
	if count, err := queries.CountMcpServerTools(t.Context(), "off"); err != nil || count != 0 {
		t.Fatalf("disabled server listed %d tools (err %v)", count, err)
	}
	if srv.Initializes() != 0 {
		t.Fatalf("disabled server was dialed %d times", srv.Initializes())
	}
}
