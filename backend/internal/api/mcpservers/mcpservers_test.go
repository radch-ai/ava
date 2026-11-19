//go:build unix

package mcpservers

// Endpoint tests over the real helper binary: verify-before-save,
// default-deny catalog rows, write-only secrets (canary never in any
// response), replace semantics, delete cascade, refresh 502 with
// diagnostics.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// helperBin is the compiled testdata server, built once per run.
var helperBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpservers-api-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp dir:", err)
		os.Exit(1)
	}
	helperBin = filepath.Join(dir, "mcpserver")
	if out, err := exec.Command("go", "build", "-o", helperBin, "../../../testdata/mcpserver").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build helper: %v\n%s\n", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var testMasterKey = bytes.Repeat([]byte{0x42}, 32)

func newTestStore(t *testing.T, sqlDB *sql.DB) *vault.Store {
	t.Helper()
	master, err := vault.NewMasterAEAD(testMasterKey)
	if err != nil {
		t.Fatalf("master AEAD: %v", err)
	}
	store, err := vault.NewStore(sqlDB, master)
	if err != nil {
		t.Fatalf("vault store: %v", err)
	}
	return store
}

// do runs one route through a real ServeMux with a fresh throttle and
// manager per call (PathValue needs the mux; the fresh throttle keeps
// write bursts in tests legal).
func do(t *testing.T, sqlDB *sql.DB, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	return doWith(t, sqlDB, manager, method, target, body)
}

// doWith is do over a caller-supplied manager, so a test can observe
// the live-client cache across several requests.
func doWith(t *testing.T, sqlDB *sql.DB, manager *mcp.Manager, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	queries, vaultStore := db.New(sqlDB), newTestStore(t, sqlDB)
	throttle := api.NewThrottle(time.Nanosecond)
	mux := http.NewServeMux()
	mux.Handle("GET /mcp/servers", api.Log(zerolog.Nop(), List(queries)))
	mux.Handle("POST /mcp/servers", api.Log(zerolog.Nop(), Create(manager, sqlDB, queries, vaultStore)))
	mux.Handle("PATCH /mcp/servers/{id}", api.Log(zerolog.Nop(), Update(throttle, manager, sqlDB, queries, vaultStore)))
	mux.Handle("DELETE /mcp/servers/{id}", api.Log(zerolog.Nop(), Delete(throttle, manager, queries, vaultStore)))
	mux.Handle("POST /mcp/servers/{id}/refresh", api.Log(zerolog.Nop(), Refresh(throttle, manager, sqlDB, queries, vaultStore)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func body(rec *httptest.ResponseRecorder) string { return strings.TrimSpace(rec.Body.String()) }

// createHelper installs the test helper with an env canary and returns
// the decoded create response.
func createHelper(t *testing.T, sqlDB *sql.DB, name string) apitypes.McpServerWithToolsResponse {
	t.Helper()
	pidfile := filepath.Join(t.TempDir(), "pid")
	req := `{"name":"` + name + `","transport":"stdio","command":"` + helperBin +
		`","args":["-pidfile","` + pidfile + `"],"env":{"MCP_HELPER_PROBE":"canary-value-xyz"},"timeoutMs":8000}`
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d (body %s)", rec.Code, body(rec))
	}
	var out apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	return out
}

// A failing connection check writes nothing: no row, no sealed blob,
// no catalog tools.
func TestCreateVerifyFailWritesNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers",
		`{"name":"Dead","transport":"stdio","command":"/nonexistent-ava-mcp-helper","timeoutMs":300}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want 400 connection-check failure", rec.Code, body(rec))
	}
	servers, err := queries.ListMcpServers(t.Context())
	if err != nil || len(servers) != 0 {
		t.Fatalf("failed verify wrote %d rows (err %v)", len(servers), err)
	}
	if _, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("dead")); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("failed verify sealed something: %v", err)
	}
	tools, err := queries.ListTools(t.Context())
	if err != nil || len(tools) != 0 {
		t.Fatalf("failed verify wrote %d tools (err %v)", len(tools), err)
	}
}

// A server exposing a tool name the provider cannot call is rejected by
// name and writes nothing — never installed minus that tool.
func TestCreateRejectsUnrepresentableToolName(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)

	req := `{"name":"Long Helper","transport":"stdio","command":"` + helperBin +
		`","args":["-long-tool"],"timeoutMs":8000}`
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body %s, want 400", rec.Code, body(rec))
	}
	if !strings.Contains(body(rec), "cannot be represented as a model tool name") {
		t.Fatalf("body %s does not name the rejected tool", body(rec))
	}
	if servers, err := queries.ListMcpServers(t.Context()); err != nil || len(servers) != 0 {
		t.Fatalf("rejected create wrote %d rows (err %v)", len(servers), err)
	}
	if tools, err := queries.ListTools(t.Context()); err != nil || len(tools) != 0 {
		t.Fatalf("rejected create wrote %d tools (err %v)", len(tools), err)
	}
}

// One dial per save: the verified client is adopted, so discovery reuses
// the same process instead of spawning a second one.
func TestOneDialPerSave(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	spawnLog := filepath.Join(t.TempDir(), "spawns")
	req := `{"name":"Single Dial","transport":"stdio","command":"` + helperBin +
		`","args":["-spawnlog","` + spawnLog + `"],"timeoutMs":8000}`
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	if got := spawnCount(spawnLog); got != 1 {
		t.Fatalf("create spawned %d processes, want 1", got)
	}

	// A connection-affecting patch dials once (the verify client) and
	// reuses it for discovery.
	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/single-dial", `{"timeoutMs":9000}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body %s", rec.Code, body(rec))
	}
	if got := spawnCount(spawnLog); got != 2 {
		t.Fatalf("after one connection patch %d spawns, want 2", got)
	}
}

// A rename is not a connection edit: no new process, the vault is not
// touched, the stored key names and listing survive.
func TestRenameSkipsDialAndKeepsKeys(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	spawnLog := filepath.Join(t.TempDir(), "spawns")
	req := `{"name":"Rename Me","transport":"stdio","command":"` + helperBin +
		`","args":["-spawnlog","` + spawnLog + `"],"env":{"MCP_HELPER_PROBE":"rename-canary"},"timeoutMs":8000}`
	if rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", req); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	spawned := spawnCount(spawnLog)

	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/rename-me", `{"name":"Renamed Server"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body %s", rec.Code, body(rec))
	}
	if got := spawnCount(spawnLog); got != spawned {
		t.Fatalf("rename spawned %d extra processes", got-spawned)
	}
	var out apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode rename body: %v", err)
	}
	if out.Server.Name != "Renamed Server" || out.Server.ID != "rename-me" {
		t.Fatalf("renamed server = %+v", out.Server)
	}
	if len(out.Server.EnvKeys) != 1 || out.Server.EnvKeys[0] != "MCP_HELPER_PROBE" {
		t.Fatalf("rename dropped key names: %v", out.Server.EnvKeys)
	}
	if len(out.Server.Args) != 2 || out.Server.Args[0] != "-spawnlog" {
		t.Fatalf("rename dropped args: %v", out.Server.Args)
	}
	if len(out.Tools) != 3 || out.Tools[0].Name != "echo" {
		t.Fatalf("rename response tools = %+v, want the stored listing", out.Tools)
	}
	if out.Server.LastError != nil {
		t.Fatalf("rename recorded diagnostics: %v", *out.Server.LastError)
	}
	if strings.Contains(body(rec), "rename-canary") {
		t.Fatal("rename echoed the env value")
	}
}

// A healthy create answers 201 with the slug id, key names only, the
// discovered catalog tools — and the env value sealed, never echoed.
func TestCreateSealsSecretsAndDiscovers(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)

	out := createHelper(t, sqlDB, "Test Helper")
	if out.Server.ID != "test-helper" {
		t.Fatalf("slug = %q, want test-helper", out.Server.ID)
	}
	if len(out.Tools) == 0 || out.Tools[0].ID != "test-helper:echo" {
		t.Fatalf("tools = %+v, want test-helper:echo first", out.Tools)
	}
	raw := body(do(t, sqlDB, http.MethodGet, "/mcp/servers", ""))
	if strings.Contains(raw, "canary-value-xyz") {
		t.Fatal("env value leaked into a response")
	}
	if !strings.Contains(raw, `"envKeys":["MCP_HELPER_PROBE"]`) {
		t.Fatalf("list missing env key names: %s", raw)
	}

	tool, err := queries.GetTool(t.Context(), "test-helper:echo")
	if err != nil || tool.Kind != "mcp" || tool.ServerID != "test-helper" {
		t.Fatalf("catalog row = %+v err %v", tool, err)
	}

	blob, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("test-helper"))
	if err != nil {
		t.Fatalf("sealed blob missing: %v", err)
	}
	var secrets mcpserver.Secrets
	if err := json.Unmarshal(blob, &secrets); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if secrets.Env["MCP_HELPER_PROBE"] != "canary-value-xyz" {
		t.Fatalf("sealed env = %v", secrets.Env)
	}
}

// Malformed bodies answer 400; a name collision answers 409 with the
// derived slug claimed.
func TestCreateValidationAndCollision(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	for _, tc := range []struct{ name, body string }{
		{"bad name", `{"name":"Bad-Name!","transport":"stdio","command":"x"}`},
		{"bad transport", `{"name":"Ok","transport":"sse"}`},
		{"stdio no command", `{"name":"Ok","transport":"stdio"}`},
		{"http no url", `{"name":"Ok","transport":"http"}`},
		{"bad timeout", `{"name":"Ok","transport":"stdio","command":"x","timeoutMs":0}`},
	} {
		if rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d body %s, want 400", tc.name, rec.Code, body(rec))
		}
	}

	createHelper(t, sqlDB, "Test Helper")
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers",
		`{"name":"Test Helper","transport":"stdio","command":"/nonexistent","timeoutMs":300}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("collision status = %d, want 409", rec.Code)
	}
}

// A present env map replaces the stored one wholesale; a
// connection-breaking patch verifies first and writes nothing.
func TestUpdateReplaceSecretsAndReverify(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	createHelper(t, sqlDB, "Test Helper")

	// Replace env, keep headers (none): the blob is rewritten whole.
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/test-helper",
		`{"env":{"OTHER_KEY":"second-value"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch env status = %d body %s", rec.Code, body(rec))
	}
	if strings.Contains(body(rec), "second-value") || strings.Contains(body(rec), "canary-value-xyz") {
		t.Fatal("patch leaked a secret value")
	}
	blob, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("test-helper"))
	if err != nil {
		t.Fatalf("blob after patch: %v", err)
	}
	var secrets mcpserver.Secrets
	if err := json.Unmarshal(blob, &secrets); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if _, still := secrets.Env["MCP_HELPER_PROBE"]; still {
		t.Fatal("replace did not drop the old key")
	}
	if secrets.Env["OTHER_KEY"] != "second-value" {
		t.Fatalf("replaced env = %v", secrets.Env)
	}
	row, err := queries.GetMcpServer(t.Context(), "test-helper")
	if err != nil || row.EnvKeys != `["OTHER_KEY"]` {
		t.Fatalf("env_keys column = %q err %v", row.EnvKeys, err)
	}

	// A breaking command is rejected before any write.
	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/test-helper",
		`{"command":"/nonexistent-ava-mcp-helper"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("breaking patch status = %d body %s", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), "test-helper")
	if err != nil || !row.Command.Valid || row.Command.String != helperBin {
		t.Fatalf("rejected patch changed the row: %+v err %v", row, err)
	}

	// A rename keeps the id and needs no re-verify.
	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/test-helper", `{"name":"Renamed Helper"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body %s", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), "test-helper")
	if err != nil || row.Name != "Renamed Helper" {
		t.Fatalf("rename row = %+v err %v", row, err)
	}
}

// Delete removes the row, its cascading tools and grants, and the
// sealed blob.
func TestDeleteCascadesEverything(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	createHelper(t, sqlDB, "Test Helper")
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "research", Name: "Research", Icon: "globe"})
	if _, err := queries.AddAgentTool(t.Context(), db.AddAgentToolParams{AgentID: "research", ToolID: "test-helper:echo"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	rec := do(t, sqlDB, http.MethodDelete, "/mcp/servers/test-helper", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body %s", rec.Code, body(rec))
	}
	if _, err := queries.GetMcpServer(t.Context(), "test-helper"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("row survived delete: %v", err)
	}
	if _, err := queries.GetTool(t.Context(), "test-helper:echo"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("tool survived delete: %v", err)
	}
	rows, err := queries.ListAgentTools(t.Context(), "research")
	if err != nil {
		t.Fatalf("list agent tools: %v", err)
	}
	for _, at := range rows {
		if at.ID == "test-helper:echo" {
			t.Fatal("grant survived server delete")
		}
	}
	if _, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("test-helper")); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("sealed blob survived delete: %v", err)
	}
}

// Refresh re-discovers a healthy server; a dead one answers 502 and
// records the cause in its diagnostics.
func TestRefreshHealthyAndDead(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	createHelper(t, sqlDB, "Test Helper")

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers/test-helper/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body %s", rec.Code, body(rec))
	}
	var out apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode refresh body: %v", err)
	}
	if len(out.Tools) == 0 || out.Server.LastError != nil {
		t.Fatalf("refresh tools = %+v lastError = %v", out.Tools, out.Server.LastError)
	}

	// Break the row behind the API's back: refresh must report, not
	// crash, and the row must say why it is inert.
	argsJSON, _ := json.Marshal([]string{})
	if err := queries.UpdateMcpServer(context.Background(), db.UpdateMcpServerParams{
		Name: "Test Helper", Transport: "stdio",
		Command: sql.NullString{String: "/nonexistent-ava-mcp-helper", Valid: true},
		Args:    string(argsJSON), EnvKeys: "[]", HeaderKeys: "[]",
		TimeoutMs: sql.NullInt64{Int64: 300, Valid: true}, ID: "test-helper",
	}); err != nil {
		t.Fatalf("break row: %v", err)
	}
	rec = do(t, sqlDB, http.MethodPost, "/mcp/servers/test-helper/refresh", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("dead refresh status = %d body %s, want 502", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "test-helper")
	if err != nil || !row.LastError.Valid {
		t.Fatalf("dead refresh left no diagnostics: %+v err %v", row, err)
	}
}

// Unknown ids answer 404 everywhere.
func TestUnknownServerAnswers404(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	for _, tc := range []struct{ method, target string }{
		{http.MethodPatch, "/mcp/servers/ghost"},
		{http.MethodDelete, "/mcp/servers/ghost"},
		{http.MethodPost, "/mcp/servers/ghost/refresh"},
	} {
		if rec := do(t, sqlDB, tc.method, tc.target, `{}`); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", tc.method, tc.target, rec.Code)
		}
	}
}

// spawnCount counts the helper's -spawnlog lines (one per process start).
func spawnCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(data)))
}

// createHelperWith installs the helper through a caller-supplied
// manager, so the adopted client is observable afterwards.
func createHelperWith(t *testing.T, sqlDB *sql.DB, manager *mcp.Manager, name string) apitypes.McpServerWithToolsResponse {
	t.Helper()
	pidfile := filepath.Join(t.TempDir(), "pid")
	req := `{"name":"` + name + `","transport":"stdio","command":"` + helperBin +
		`","args":["-pidfile","` + pidfile + `"],"timeoutMs":8000}`
	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d (body %s)", rec.Code, body(rec))
	}
	var out apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	return out
}

// The global switch is a pure state flip: no verify, no config write,
// and disabling tears the live client down (the row's tools stay
// listed but inert). Re-enabling reconnects best-effort.
func TestPatchEnabledTogglesAndClosesClient(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)

	created := createHelperWith(t, sqlDB, manager, "Toggle Helper")
	id := created.Server.ID
	if _, ok := manager.Get(id); !ok {
		t.Fatalf("create did not adopt a client for %q", id)
	}

	rec := doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d: %s", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || row.Enabled != 0 {
		t.Fatalf("row after disable: enabled=%d err=%v", row.Enabled, err)
	}
	if _, ok := manager.Get(id); ok {
		t.Fatal("disabled server kept a live client")
	}
	var count int64
	if count, err = queries.CountMcpServerTools(t.Context(), sql.NullString{String: id, Valid: true}); err != nil || count == 0 {
		t.Fatalf("disable pruned the catalog: count=%d err=%v", count, err)
	}

	rec = doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id, `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d: %s", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), id)
	if err != nil || row.Enabled != 1 {
		t.Fatalf("row after enable: enabled=%d err=%v", row.Enabled, err)
	}
	if _, ok := manager.Get(id); !ok {
		t.Fatal("enable did not reconnect a client")
	}
}

// A pure switch patch must not dial: the stored config is untouched
// and the request answers immediately even when the stored command is
// unrunnable.
func TestPureEnabledPatchSkipsVerify(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	created := createHelper(t, sqlDB, "Pure Toggle")
	id := created.Server.ID

	manager := mcp.NewManager() // fresh: nothing cached, nothing to close
	t.Cleanup(manager.CloseAll)

	start := time.Now()
	rec := doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id, `{"enabled":false}`)
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, body(rec))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("pure switch took %v: it verified the connection", elapsed)
	}
	if _, ok := manager.Get(id); ok {
		t.Fatal("pure switch dialed a client")
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || row.Enabled != 0 {
		t.Fatalf("row after switch: enabled=%d err=%v", row.Enabled, err)
	}
}

// TestPatchFailedVerifyAnswersInTime pins the stuck-edit bug: a server
// whose process dies before the handshake used to wait the full default
// deadline (30s) -- past the http.Server write deadline (15s), so the
// 400 never reached the client. It must answer inside the verify
// budget and write nothing.
func TestPatchFailedVerifyAnswersInTime(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	created := createHelper(t, sqlDB, "Fast Fail")

	start := time.Now()
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+created.Server.ID, `{"command":"/bin/false"}`)
	elapsed := time.Since(start)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want 400 connection-check failure", rec.Code, body(rec))
	}
	if elapsed > mcp.VerifyTimeout+2*time.Second {
		t.Fatalf("failure answered after %v, want <= %v", elapsed, mcp.VerifyTimeout)
	}
	row, err := db.New(sqlDB).GetMcpServer(t.Context(), created.Server.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !row.Command.Valid || !strings.HasSuffix(row.Command.String, "mcpserver") {
		t.Fatalf("failed patch rewrote the stored command: %q", row.Command.String)
	}
}

// A discovery failure after the verified dial rolls the whole install
// back: no row, no sealed blob, no cached client, no catalog tools —
// and the name is free for a clean retry.
func TestCreateRollsBackWhenDiscoveryFails(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		`{"name":"Flaky","transport":"stdio","command":"`+helperBin+`","args":["-fail-second-list"],"timeoutMs":8000}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, body(rec))
	}
	if _, err := queries.GetMcpServer(t.Context(), "flaky"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rollback left a row: %v", err)
	}
	if _, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("flaky")); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("rollback left sealed secrets: %v", err)
	}
	if _, ok := manager.Get("flaky"); ok {
		t.Fatal("rollback left a cached client")
	}
	tools, err := queries.ListTools(t.Context())
	if err != nil || len(tools) != 0 {
		t.Fatalf("rollback left %d tools (err %v)", len(tools), err)
	}
	// The failed install left no trace: the same name installs cleanly.
	createHelper(t, sqlDB, "Flaky")
}

// Concurrent installs of the same name: one wins, the other reads 409
// (pre-check or the insert's unique constraint) — never a 500.
func TestCreateDuplicateNameRace(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store := newTestStore(t, sqlDB)
	logger := zerolog.Nop()

	const attempts = 3
	for round := range attempts {
		name := fmt.Sprintf("Race%d", round)
		req := `{"name":"` + name + `","transport":"stdio","command":"` + helperBin + `","timeoutMs":8000}`
		managers := [2]*mcp.Manager{mcp.NewManager(), mcp.NewManager()}
		for _, manager := range managers {
			t.Cleanup(manager.CloseAll)
		}
		codes := make(chan int, 2)
		var wg sync.WaitGroup
		for i := range managers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mux := http.NewServeMux()
				mux.Handle("POST /mcp/servers", api.Log(logger, Create(managers[i], sqlDB, queries, store)))
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/servers", strings.NewReader(req)))
				codes <- rec.Code
			}()
		}
		wg.Wait()
		close(codes)
		got := map[int]int{}
		for code := range codes {
			got[code]++
		}
		if got[http.StatusInternalServerError] != 0 {
			t.Fatalf("round %d: concurrent create answered 500 (%v)", round, got)
		}
		if got[http.StatusCreated] != 1 || got[http.StatusConflict] != 1 {
			t.Fatalf("round %d: codes = %v, want one 201 and one 409", round, got)
		}
	}
}

// mustExec applies one DDL statement; tests use triggers to fail a
// specific table's writes while the rest of the database stays healthy.
func mustExec(t *testing.T, sqlDB *sql.DB, statement string) {
	t.Helper()
	if _, err := sqlDB.Exec(statement); err != nil {
		t.Fatalf("exec %q: %v", statement, err)
	}
}

// unsealedEnv reads the runtime blob's env map — the values the next
// dial would use.
func unsealedEnv(t *testing.T, sqlDB *sql.DB, slug string) map[string]string {
	t.Helper()
	raw, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName(slug))
	if err != nil {
		t.Fatalf("unseal %q: %v", slug, err)
	}
	var secrets mcpserver.Secrets
	if err := json.Unmarshal(raw, &secrets); err != nil {
		t.Fatalf("decode secrets %q: %v", slug, err)
	}
	return secrets.Env
}

// A seal failure after the row insert must not orphan the install: the
// row would block the name while the vault holds nothing it promises.
// Nothing is left behind and a clean retry installs.
func TestCreateSealFailureLeavesNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)

	mustExec(t, sqlDB, `CREATE TRIGGER fail_vault_write BEFORE INSERT ON vault_entries BEGIN SELECT RAISE(ABORT, 'vault down'); END;`)
	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		`{"name":"Flaky","transport":"stdio","command":"`+helperBin+`","env":{"MCP_HELPER_PROBE":"canary-value-xyz"},"timeoutMs":8000}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, body(rec))
	}
	if _, err := queries.GetMcpServer(t.Context(), "flaky"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("seal failure left a row: %v", err)
	}
	if _, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("flaky")); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("seal failure left sealed secrets: %v", err)
	}
	if _, ok := manager.Get("flaky"); ok {
		t.Fatal("seal failure left a cached client")
	}
	tools, err := queries.ListTools(t.Context())
	if err != nil || len(tools) != 0 {
		t.Fatalf("seal failure left %d tools (err %v)", len(tools), err)
	}
	// The failed install left no trace: the same name installs cleanly.
	mustExec(t, sqlDB, `DROP TRIGGER fail_vault_write;`)
	createHelper(t, sqlDB, "Flaky")
}

// A seal failure during a secrets-replacing patch writes nothing: the
// row keeps its stored config and the blob keeps the stored values —
// key-name columns never promise values the vault lacks.
func TestUpdateSealFailureKeepsStoredState(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	id := createHelper(t, sqlDB, "Seal Fail").Server.ID

	mustExec(t, sqlDB, `CREATE TRIGGER fail_vault_write BEFORE INSERT ON vault_entries BEGIN SELECT RAISE(ABORT, 'vault down'); END;`)
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"env":{"MCP_OTHER":"rotated"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.EnvKeys != `["MCP_HELPER_PROBE"]` {
		t.Fatalf("row env keys = %s, want the stored key names untouched", row.EnvKeys)
	}
	if env := unsealedEnv(t, sqlDB, id); env["MCP_HELPER_PROBE"] != "canary-value-xyz" || env["MCP_OTHER"] != "" {
		t.Fatalf("sealed env = %v, want the stored values untouched", env)
	}
}

// A row-write failure after a successful re-seal restores the
// previously sealed values: row and blob move together or not at all.
func TestUpdateRowFailureRestoresSealedSecrets(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	id := createHelper(t, sqlDB, "Row Fail").Server.ID

	mustExec(t, sqlDB, `CREATE TRIGGER fail_row_write BEFORE UPDATE ON mcp_servers BEGIN SELECT RAISE(ABORT, 'db down'); END;`)
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"env":{"MCP_OTHER":"rotated"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.EnvKeys != `["MCP_HELPER_PROBE"]` {
		t.Fatalf("row env keys = %s, want the stored key names", row.EnvKeys)
	}
	if env := unsealedEnv(t, sqlDB, id); env["MCP_HELPER_PROBE"] != "canary-value-xyz" || env["MCP_OTHER"] != "" {
		t.Fatalf("sealed env = %v, want the stored values restored", env)
	}
}
