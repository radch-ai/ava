//go:build unix

package backend_test

// MCP boot discovery pins the wiring: registry routing, catalog
// upsert/prune, default-deny grants, diagnostics, and the shutdown kill
// of stdio children. The helper binary is the same one internal/mcp
// drives (backend/testdata/mcpserver).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// mcpHelperBin is the compiled testdata server, built once per run.
var mcpHelperBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "backend-mcpserver-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp dir:", err)
		os.Exit(1)
	}
	mcpHelperBin = filepath.Join(dir, "mcpserver")
	if out, err := exec.Command("go", "build", "-o", mcpHelperBin, "./testdata/mcpserver").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build helper: %v\n%s\n", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// bootBackend boots over an already-seeded database; every boot owns its
// MCP children, so each one registers its own teardown.
func bootBackend(t *testing.T, sqlDB *sql.DB) *backend.TurnService {
	t.Helper()
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	t.Cleanup(svc.CloseTools)
	return svc
}

func seedMcpServer(t *testing.T, queries *db.Queries, id, command string, args []string, timeoutMs int64) {
	t.Helper()
	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID:         id,
		Name:       "Test " + id,
		Transport:  "stdio",
		Command:    sql.NullString{String: command, Valid: true},
		Args:       string(argsJSON),
		EnvKeys:    "[]",
		HeaderKeys: "[]",
		TimeoutMs:  sql.NullInt64{Int64: timeoutMs, Valid: true},
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
}

func readPids(t *testing.T, path string) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			fields := strings.Fields(string(data))
			pids := make([]int, 0, len(fields))
			for _, f := range fields {
				pid, err := strconv.Atoi(f)
				if err != nil {
					t.Fatalf("pidfile %q unparsable: %v", data, err)
				}
				pids = append(pids, pid)
			}
			return pids
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pidfile %s never appeared", path)
	return nil
}

func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return true
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(data)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] == 'Z'
	}
	return false
}

func waitPidsGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range pids {
			if !processGone(pid) {
				alive = true
				break
			}
		}
		if !alive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("processes %v still alive after CloseTools", pids)
}

// A live server's tools land in the catalog un-granted, run for the
// agent once granted, keep their grants semantics through a re-sync
// prune, and die with the service on CloseTools.
func TestMcpBootDiscoveryLiveServer(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	ctx := t.Context()
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "research", Name: "Research", Icon: "globe"})
	pidfile := filepath.Join(t.TempDir(), "pid")
	seedMcpServer(t, queries, "helper", mcpHelperBin, []string{"-pidfile", pidfile}, 5000)

	svc := bootBackend(t, sqlDB)

	// Discovery upserted the server's tools as MCP catalog rows.
	echo, err := queries.GetTool(ctx, "helper:echo")
	if err != nil {
		t.Fatalf("get helper:echo: %v", err)
	}
	if echo.Kind != "mcp" || echo.ServerID != "helper" {
		t.Fatalf("tool row = kind %q server %v", echo.Kind, echo.ServerID)
	}

	// Default-deny: the agent sees the tool but holds no grant.
	row, err := queries.GetAgentTool(ctx, db.GetAgentToolParams{AgentID: "research", ID: "helper:echo"})
	if err != nil {
		t.Fatalf("agent view: %v", err)
	}
	if row.AgentEnabled != 0 {
		t.Fatal("mcp tool installed granted")
	}

	// Grant it and approve the agent's server gate (resolution needs
	// the grant and the parent gate), then the turn's fetch runs it under
	// the composite name.
	if _, err := queries.AddAgentTool(ctx, db.AddAgentToolParams{AgentID: "research", ToolID: "helper:echo"}); err != nil {
		t.Fatalf("grant: %v", err)
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
		t.Fatalf("granted mcp tool not in the turn set: %d tools", len(impls))
	}
	out, err := found.InvokableRun(ctx, `{"text":"boot round"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `"text":"boot round"`) {
		t.Fatalf("result = %q", out)
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
	// grant on the next boot's sync.
	if err := queries.UpsertMcpTool(ctx, db.UpsertMcpToolParams{
		ID: "helper:ghost", Name: "ghost", Description: "stale", ServerID: "helper",
	}); err != nil {
		t.Fatalf("seed stale tool: %v", err)
	}
	if _, err := queries.AddAgentTool(ctx, db.AddAgentToolParams{AgentID: "research", ToolID: "helper:ghost"}); err != nil {
		t.Fatalf("grant stale tool: %v", err)
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

// The kill assertion lives in its own test so the ordering stays
// honest: boot, then CloseTools, then the child must be gone.
func TestMcpCloseToolsKillsChildren(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	pidfile := filepath.Join(t.TempDir(), "pid")
	seedMcpServer(t, queries, "helper", mcpHelperBin, []string{"-pidfile", pidfile}, 5000)

	svc := bootBackend(t, sqlDB)
	pids := readPids(t, pidfile)
	svc.CloseTools()
	waitPidsGone(t, pids)
}

// A dead server never fails boot: its row explains itself, and no tools
// are listed.
func TestMcpBootSurvivesDeadServer(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	seedMcpServer(t, queries, "dead", "/nonexistent-ava-mcp-helper", nil, 300)

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
	seedMcpServer(t, queries, "off", mcpHelperBin, nil, 5000)
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
	count, err := queries.CountMcpServerTools(t.Context(), "off")
	if err != nil {
		t.Fatalf("count tools: %v", err)
	}
	if count != 0 {
		t.Fatalf("disabled server listed %d tools", count)
	}
}
