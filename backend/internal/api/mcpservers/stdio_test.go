//go:build unix

package mcpservers

// Stdio endpoint tests over a real spawned process: the acknowledgment
// gate, the process-shape matrix, the reset-on-change rule, sealed env
// values, discovery and the definition's authority facts.

import (
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
	"syscall"
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcptest"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// stdioBin is the compiled testdata/mcpserver from the mcp package, built
// once per package run: these tests drive a real child process.
var stdioBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpserver-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp dir:", err)
		os.Exit(1)
	}
	stdioBin = filepath.Join(dir, "mcpserver")
	if out, err := exec.Command("go", "build", "-o", stdioBin, "../../mcp/testdata/mcpserver").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build helper: %v\n%s\n", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// sendJSON runs one route with a JSON payload.
func sendJSON(t *testing.T, sqlDB *sql.DB, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	return sendJSONWith(t, sqlDB, nil, method, target, payload)
}

// sendJSONWith runs one route over a caller-supplied manager, so a test can
// observe the live client across requests (nil = a fresh one per call).
func sendJSONWith(t *testing.T, sqlDB *sql.DB, manager *mcp.Manager, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if manager == nil {
		return do(t, sqlDB, method, target, string(raw))
	}
	return doWith(t, sqlDB, manager, method, target, string(raw))
}

// createStdioRequest is the install body one stdio server needs.
func createStdioRequest(name, command string, args []string, env map[string]string) apitypes.CreateMcpServerRequest {
	timeout := int64(8000)
	return apitypes.CreateMcpServerRequest{
		Name:                        name,
		Transport:                   "stdio",
		Command:                     command,
		Args:                        args,
		Env:                         env,
		TimeoutMs:                   &timeout,
		AcknowledgeProcessExecution: true,
	}
}

// TestStdioCreateRequiresAcknowledgement pins the install gate: nothing is
// written, spawned or published until the privilege is acknowledged.
func TestStdioCreateRequiresAcknowledgement(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	pidfile := filepath.Join(t.TempDir(), "pid")
	req := createStdioRequest("Local Notes", stdioBin, []string{"-pidfile", pidfile}, nil)
	req.AcknowledgeProcessExecution = false

	rec := sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers", req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "acknowledge required") {
		t.Fatalf("create without ack = %d body %s", rec.Code, body(rec))
	}
	if _, err := queries.GetMcpServer(t.Context(), "local-notes"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unacknowledged install wrote a row: err=%v", err)
	}
	if _, err := os.Stat(pidfile); err == nil {
		t.Fatal("unacknowledged install spawned the process")
	}
}

// TestProcessAckFingerprintIsInjective pins the encoding: an argument
// carrying the separator byte must not collide with the split shape it
// spoofed — a colliding argv still re-prompts the acknowledgment.
func TestProcessAckFingerprintIsInjective(t *testing.T) {
	split := processAck("npx", []string{"-y", "pkg"})
	glued := processAck("npx", []string{"-y\x1fpkg"})
	moved := processAck("npx\x1f-y", []string{"pkg"})
	if split == glued || split == moved || glued == moved {
		t.Fatalf("fingerprint collision: split=%s glued=%s moved=%s", split, glued, moved)
	}
}

// TestStdioAckAuditTrail pins the gate's durable-before-effect ordering:
// the acknowledgment lands with the install, and a verify failure that
// rolls the install back withdraws it — the trail never shows an
// acknowledged process that does not exist.
func TestStdioAckAuditTrail(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	count := func(id, reason string) int {
		t.Helper()
		var n int
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM tool_audit_events WHERE server_id = ? AND reason_code = ?`,
			id, reason).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", reason, err)
		}
		return n
	}

	rec := sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers",
		createStdioRequest("Local Notes", stdioBin, nil, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	if n := count("local-notes", "process_acknowledged"); n != 1 {
		t.Fatalf("acknowledged records = %d, want 1", n)
	}
	if n := count("local-notes", "process_ack_not_activated"); n != 0 {
		t.Fatalf("withdrawal records = %d, want 0", n)
	}

	// A command that cannot spawn still records the acknowledgment
	// first, then withdraws it when the verify failure rolls the
	// install back.
	rec = sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers",
		createStdioRequest("Dead Process", filepath.Join(t.TempDir(), "missing"), nil, nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dead create status = %d body %s", rec.Code, body(rec))
	}
	if n := count("dead-process", "process_acknowledged"); n != 1 {
		t.Fatalf("dead acknowledged records = %d, want 1", n)
	}
	if n := count("dead-process", "process_ack_not_activated"); n != 1 {
		t.Fatalf("dead withdrawal records = %d, want 1", n)
	}
}

// TestStdioCreatePublishesListing covers the happy path end to end: the
// process answers, its tools are published (the over-long name skipped with
// a reason), and the sealed env value never reaches the wire.
func TestStdioCreatePublishesListing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	args := []string{"-stderr-flood", "262144"}
	req := createStdioRequest("Local Notes", stdioBin, args,
		map[string]string{"NOTES_TOKEN": "canary-env-value"})

	rec := sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	if strings.Contains(body(rec), "canary-env-value") {
		t.Fatalf("create response leaked the sealed value: %s", body(rec))
	}
	var out apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if out.Server.Transport != "stdio" || out.Server.Command != stdioBin ||
		!equalStrings(out.Server.Args, args) {
		t.Fatalf("server shape = %+v", out.Server)
	}
	if !equalStrings(out.Server.EnvKeys, []string{"NOTES_TOKEN"}) {
		t.Fatalf("envKeys = %v, want the sealed key name", out.Server.EnvKeys)
	}
	names := make([]string, 0, len(out.Tools))
	for _, tool := range out.Tools {
		names = append(names, tool.Name)
	}
	if !equalStrings(names, []string{"echo", "report_env"}) {
		t.Fatalf("published tools = %v", names)
	}
	if len(out.SkippedTools) != 1 || !strings.HasPrefix(out.SkippedTools[0].Name, "xxxx") {
		t.Fatalf("skipped = %+v, want the over-long name", out.SkippedTools)
	}

	row, err := queries.GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.Transport != "stdio" || !row.Command.Valid || row.Url.Valid {
		t.Fatalf("row shape = %+v", row)
	}
	if !row.ProcessAck.Valid {
		t.Fatal("stdio install stored no acknowledgment fingerprint")
	}

	// The definition's authority is the command, with credentials from the
	// env keys and no endpoint facts.
	rec = do(t, sqlDB, http.MethodGet, "/mcp/servers/local-notes/tools/local-notes:echo/definition", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("definition status = %d body %s", rec.Code, body(rec))
	}
	var def apitypes.McpToolDefinitionResponse
	if err := json.Unmarshal([]byte(body(rec)), &def); err != nil {
		t.Fatalf("decode definition: %v", err)
	}
	if def.Command != stdioBin || def.Endpoint != "" || !def.HasCredentials ||
		def.AllowPlaintext || def.AllowPrivateNetwork {
		t.Fatalf("definition authority = %+v", def)
	}
}

// TestStdioCommandChangeRePrompts pins the reset rule: a changed command or
// args needs the acknowledgment again, and an unacknowledged patch leaves
// the row untouched.
func TestStdioCommandChangeRePrompts(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	pidfile := filepath.Join(t.TempDir(), "pid")
	rec := sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers",
		createStdioRequest("Local Notes", stdioBin, []string{"-pidfile", pidfile}, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	before, err := queries.GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}

	// Unchanged command and args: no re-prompt (a rename stays cheap).
	rec = sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Name: ptr("Local Notes Renamed")})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body %s", rec.Code, body(rec))
	}

	newArgs := []string{"-pidfile", pidfile, "-stderr-flood", "4096"}
	rec = sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Args: newArgs})
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "acknowledge required") {
		t.Fatalf("args change without ack = %d body %s", rec.Code, body(rec))
	}
	unchanged, err := queries.GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if unchanged.Args != before.Args || unchanged.ProcessAck != before.ProcessAck {
		t.Fatalf("rejected patch moved the row: %+v", unchanged)
	}

	rec = sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Args: newArgs, AcknowledgeProcessExecution: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("acknowledged args change = %d body %s", rec.Code, body(rec))
	}
	after, err := queries.GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if after.ProcessAck == before.ProcessAck || !after.ProcessAck.Valid {
		t.Fatalf("acknowledged change kept the old fingerprint: %+v", after.ProcessAck)
	}
}

// TestTransportFieldMatrix pins the request shape rules: a field of the
// other transport is a 400, never silently dropped.
func TestTransportFieldMatrix(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)

	for _, tc := range []struct {
		name    string
		payload apitypes.CreateMcpServerRequest
		want    string
	}{
		{
			name: "stdio with url",
			payload: apitypes.CreateMcpServerRequest{Name: "Stdio Url", Transport: "stdio",
				Command: stdioBin, URL: srv.URL, AcknowledgeProcessExecution: true},
			want: "takes no endpoint url",
		},
		{
			name: "stdio with destination approval",
			payload: apitypes.CreateMcpServerRequest{Name: "Stdio Flags", Transport: "stdio",
				Command: stdioBin, AllowPrivateNetwork: ptr(true), AcknowledgeProcessExecution: true},
			want: "no destination approvals",
		},
		{
			name: "stdio without command",
			payload: apitypes.CreateMcpServerRequest{Name: "Stdio Empty", Transport: "stdio",
				AcknowledgeProcessExecution: true},
			want: "stdio transport needs a command",
		},
		{
			name: "http with command",
			payload: apitypes.CreateMcpServerRequest{Name: "Http Command", URL: srv.URL,
				Command: "/bin/true"},
			want: "takes no command, args or environment",
		},
		{
			name: "http with process ack",
			payload: apitypes.CreateMcpServerRequest{Name: "Http Ack", URL: srv.URL,
				AcknowledgeProcessExecution: true},
			want: ackNotApplicable,
		},
		{
			name: "unknown transport",
			payload: apitypes.CreateMcpServerRequest{Name: "Sse Server", Transport: "sse",
				URL: srv.URL},
			want: "unsupported transport",
		},
	} {
		rec := sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers", tc.payload)
		if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), tc.want) {
			t.Errorf("%s = %d body %s, want 400 %q", tc.name, rec.Code, body(rec), tc.want)
		}
	}

	// The same matrix on the patch path: an established row cannot be
	// talked into the other transport's fields.
	rec := sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers",
		createStdioRequest("Local Notes", stdioBin, nil, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	for _, tc := range []struct {
		name    string
		payload apitypes.UpdateMcpServerRequest
		want    string
	}{
		{"stdio patch with url", apitypes.UpdateMcpServerRequest{URL: ptr(srv.URL)},
			"takes no endpoint url"},
		{"stdio patch with plaintext", apitypes.UpdateMcpServerRequest{AllowPlaintext: ptr(true)},
			"no destination approvals"},
	} {
		rec := sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes", tc.payload)
		if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), tc.want) {
			t.Errorf("%s = %d body %s, want 400 %q", tc.name, rec.Code, body(rec), tc.want)
		}
	}
	// The loopback fixture needs the destination approvals a transport
	// switch resets: the same patch may re-approve them.
	rec = sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Transport: ptr("http"), URL: ptr(srv.URL),
			AllowPrivateNetwork: ptr(true), AllowPlaintext: ptr(true)})
	if rec.Code != http.StatusOK {
		t.Fatalf("transport switch to http = %d body %s", rec.Code, body(rec))
	}
	row, err := db.New(sqlDB).GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.Transport != "http" || !row.Url.Valid || row.Command.Valid ||
		row.ProcessAck.Valid || row.EnvKeys != "[]" {
		t.Fatalf("switched row kept the old transport's fields: %+v", row)
	}
	// The acknowledgment flag is stdio-only on the patch path too: the
	// now-http row rejects it instead of silently dropping it.
	rec = sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{AcknowledgeProcessExecution: true})
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), ackNotApplicable) {
		t.Fatalf("http patch with process ack = %d body %s", rec.Code, body(rec))
	}
	// Switching into a process re-prompts too: the NULL→hash arm needs
	// the acknowledgment before anything spawns.
	rec = sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Transport: ptr("stdio"), Command: ptr(stdioBin)})
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), ackRequired) {
		t.Fatalf("switch to stdio without ack = %d body %s", rec.Code, body(rec))
	}
	rec = sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Transport: ptr("stdio"), Command: ptr(stdioBin),
			AcknowledgeProcessExecution: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("switch to stdio with ack = %d body %s", rec.Code, body(rec))
	}
	row, err = db.New(sqlDB).GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.Transport != "stdio" || !row.Command.Valid || !row.ProcessAck.Valid || row.Url.Valid {
		t.Fatalf("switched row lost the process shape: %+v", row)
	}
}

// TestStdioDisableKillsProcess pins the switch's teardown: disabling the
// server kills the spawned process, and a pure flip never dials (a removed
// binary cannot fail it).
func TestStdioDisableKillsProcess(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	// One manager across the requests: the switch must close the client
	// the install opened.
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	dir := t.TempDir()
	command := filepath.Join(dir, "mcpserver")
	copyBinary(t, stdioBin, command)
	pidfile := filepath.Join(dir, "pid")

	rec := sendJSONWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		createStdioRequest("Local Notes", command, []string{"-pidfile", pidfile}, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	pids := waitPids(t, pidfile)

	// The command is gone: only a pure flip (no verify) can still succeed.
	if err := os.Remove(command); err != nil {
		t.Fatalf("remove command: %v", err)
	}
	rec = sendJSONWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Enabled: ptr(false)})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body %s", rec.Code, body(rec))
	}
	waitPidsGone(t, pids)
	row, err := queries.GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.Enabled != 0 {
		t.Fatal("disable did not persist")
	}

	// Re-enabling degrades: the stored listing answers, diagnostics say why.
	rec = sendJSONWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/local-notes",
		apitypes.UpdateMcpServerRequest{Enabled: ptr(true)})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d body %s", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), "local-notes")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !row.LastError.Valid {
		t.Fatal("a failed reconnect recorded no diagnostics")
	}
}

// TestStdioRefresh publishes again over a live process: refresh is
// transport-blind.
func TestStdioRefresh(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	rec := sendJSON(t, sqlDB, http.MethodPost, "/mcp/servers",
		createStdioRequest("Local Notes", stdioBin, nil, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	rec = do(t, sqlDB, http.MethodPost, "/mcp/servers/local-notes/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body %s", rec.Code, body(rec))
	}
	var out apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode refresh body: %v", err)
	}
	if len(out.Tools) != 2 || out.Server.Transport != "stdio" {
		t.Fatalf("refresh listing = %+v", out)
	}
	if row, err := queries.GetMcpServer(t.Context(), "local-notes"); err != nil ||
		row.CatalogState != "active" {
		t.Fatalf("catalog state after refresh: %+v err=%v", row, err)
	}
}

// TestHTTPPatchRejectsStdioFields pins the other direction on the patch
// path: an http row rejects process fields outright.
func TestHTTPPatchRejectsStdioFields(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)
	createServer(t, sqlDB, "Notes MCP", srv, "")
	for _, tc := range []struct {
		name    string
		payload apitypes.UpdateMcpServerRequest
	}{
		// The ack flag on an http row is its own rejection, pinned in
		// TestTransportFieldMatrix: this case stays the pure field matrix.
		{"command", apitypes.UpdateMcpServerRequest{Command: ptr(stdioBin)}},
		{"args", apitypes.UpdateMcpServerRequest{Args: []string{"--stdio"}}},
		{"env", apitypes.UpdateMcpServerRequest{Env: map[string]string{"A": "1"}}},
		{"inheritEnv", apitypes.UpdateMcpServerRequest{InheritEnv: ptr(true)}},
	} {
		rec := sendJSON(t, sqlDB, http.MethodPatch, "/mcp/servers/notes-mcp", tc.payload)
		if rec.Code != http.StatusBadRequest ||
			!strings.Contains(body(rec), "takes no command, args or environment") {
			t.Errorf("%s on http row = %d body %s", tc.name, rec.Code, body(rec))
		}
	}
}

// ptr is the pointer form the wire's absent-keeps fields use (the module
// predates Go 1.26's new(expr)).
func ptr[T any](v T) *T { return &v }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyBinary writes src's bytes to dst with an executable mode.
func copyBinary(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}
}

// waitPids waits for the fixture's pidfile and returns the pids it lists.
func waitPids(t *testing.T, path string) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			var pids []int
			for _, field := range strings.Fields(string(data)) {
				var pid int
				if _, err := fmt.Sscanf(field, "%d", &pid); err != nil {
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

// waitPidsGone asserts every pid disappears within the deadline. A zombie
// counts as gone: the kill probe still finds it, but it holds no resources.
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
	t.Fatalf("processes %v still alive after teardown", pids)
}

// processGone reports a pid that is gone or already a zombie.
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
