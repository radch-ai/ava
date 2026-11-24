package mcpservers

// Endpoint tests over a live streamable-HTTP fixture: verify-before-save,
// globally-off catalog rows, write-only secrets (canary never in any
// response), wholesale replace semantics for stored secrets, delete
// cascade, non-disruptive refresh with diagnostics, and destination
// policy.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/radch-ai/ava/backend/internal/mcptest"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

var testMasterKey = []byte("0123456789abcdef0123456789abcdef")

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
	return doWithCanceler(t, sqlDB, manager, nil, method, target, body)
}

// doWithCanceler is doWith with the admission cancellation registry the
// real router passes in, so a test can observe what a write canceled.
func doWithCanceler(t *testing.T, sqlDB *sql.DB, manager *mcp.Manager, canceler api.Canceler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	queries, vaultStore := db.New(sqlDB), newTestStore(t, sqlDB)
	throttle := api.NewThrottle(time.Nanosecond)
	mux := http.NewServeMux()
	mux.Handle("GET /mcp/servers", api.Log(zerolog.Nop(), List(queries)))
	mux.Handle("POST /mcp/servers", api.Log(zerolog.Nop(), Create(manager, sqlDB, queries, vaultStore)))
	mux.Handle("PATCH /mcp/servers/{id}", api.Log(zerolog.Nop(), Update(throttle, manager, sqlDB, queries, vaultStore, canceler, zerolog.Nop())))
	mux.Handle("DELETE /mcp/servers/{id}", api.Log(zerolog.Nop(), Delete(throttle, manager, queries, vaultStore, nil)))
	mux.Handle("POST /mcp/servers/{id}/refresh", api.Log(zerolog.Nop(), Refresh(throttle, manager, sqlDB, queries, vaultStore, canceler, zerolog.Nop())))
	mux.Handle("GET /mcp/servers/{id}/tools/{toolId}/definition", api.Log(zerolog.Nop(), ToolDefinition(queries)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func body(rec *httptest.ResponseRecorder) string { return strings.TrimSpace(rec.Body.String()) }

// createBody builds a create request for one fixture endpoint. The
// fixture listens on loopback, so the destination and plaintext approvals
// are part of the request the way an operator would send them.
func createBody(name, url, extra string) string {
	return `{"name":"` + name + `","url":"` + url + `",` +
		`"allowPrivateNetwork":true,"allowPlaintext":true,"timeoutMs":8000` + extra + `}`
}

// createServer installs one fixture and returns the decoded response.
func createServer(t *testing.T, sqlDB *sql.DB, name string, srv *mcptest.Server, extra string) apitypes.McpServerWithToolsResponse {
	t.Helper()
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", createBody(name, srv.URL, extra))
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

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", createBody("Dead", "http://127.0.0.1:1/mcp", ""))
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want 400 connection-check failure", rec.Code, body(rec))
	}
	if servers, err := queries.ListMcpServers(t.Context()); err != nil || len(servers) != 0 {
		t.Fatalf("failed verify wrote %d rows (err %v)", len(servers), err)
	}
	if _, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("dead")); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("failed verify sealed something: %v", err)
	}
	if tools, err := queries.ListTools(t.Context()); err != nil || len(tools) != 0 {
		t.Fatalf("failed verify wrote %d tools (err %v)", len(tools), err)
	}
}

// One dial per save: the verified client is adopted, so discovery reuses
// the same session instead of dialing a second time.
func TestOneDialPerSave(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)

	createServer(t, sqlDB, "Single Dial", srv, "")
	if got := srv.Initializes(); got != 1 {
		t.Fatalf("create dialed %d times, want 1", got)
	}

	// A connection-affecting patch dials once (the verify client) and
	// reuses it for discovery.
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/single-dial", `{"timeoutMs":9000}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body %s", rec.Code, body(rec))
	}
	if got := srv.Initializes(); got != 2 {
		t.Fatalf("after one connection patch %d dials, want 2", got)
	}
}

// A same-config connection edit (headers sent identical to stored)
// re-verifies and re-publishes. The catalog definition and the live
// client must stay one generation: the manager may keep its cached
// same-config client, so publishing the verification client's listing
// and then discarding that client leaves the catalog ahead of the
// implementation.
func TestSameConfigPatchKeepsCatalogAndClientCoherent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		createBody("Coherent", srv.URL, `,"headers":{"Authorization":"Bearer tok"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	srv.SetEcho("changed instruction", "")

	rec = doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/coherent",
		`{"headers":{"Authorization":"Bearer tok"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body %s", rec.Code, body(rec))
	}

	tool, err := queries.GetTool(t.Context(), "coherent:echo")
	if err != nil {
		t.Fatalf("read tool: %v", err)
	}
	client, ok := manager.Get("coherent")
	if !ok {
		t.Fatal("no live client after patch")
	}
	live, ok := client.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not live after patch")
	}
	info, err := live.Info(t.Context())
	if err != nil {
		t.Fatalf("live info: %v", err)
	}
	if tool.Description != "changed instruction" || info.Desc != "changed instruction" {
		t.Fatalf("catalog=%q live=%q: definition and implementation diverged",
			tool.Description, info.Desc)
	}
}

// A rename is not a connection edit: no new dial, the vault is not
// touched, and the stored key names and listing survive.
func TestRenameSkipsDialAndKeepsKeys(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)
	created := createServer(t, sqlDB, "Rename Me", srv,
		`,"headers":{"Authorization":"Bearer rename-canary"}`)
	dials := srv.Initializes()

	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/rename-me", `{"name":"Renamed Server"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body %s", rec.Code, body(rec))
	}
	if got := srv.Initializes(); got != dials {
		t.Fatalf("rename dialed %d extra times", got-dials)
	}
	var out apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode rename body: %v", err)
	}
	if out.Server.Name != "Renamed Server" || out.Server.ID != "rename-me" {
		t.Fatalf("renamed server = %+v", out.Server)
	}
	if len(out.Server.HeaderKeys) != 1 || out.Server.HeaderKeys[0] != "Authorization" {
		t.Fatalf("rename dropped key names: %v", out.Server.HeaderKeys)
	}
	if len(out.Tools) != 1 || out.Tools[0].Name != "echo" {
		t.Fatalf("rename response tools = %+v, want the stored listing", out.Tools)
	}
	if out.Server.LastError != nil {
		t.Fatalf("rename recorded diagnostics: %v", *out.Server.LastError)
	}
	if strings.Contains(body(rec), "rename-canary") {
		t.Fatal("rename echoed the header value")
	}
	if created.Server.CatalogState != "active" {
		t.Fatalf("catalog state = %q, want active", created.Server.CatalogState)
	}
}

// A healthy create answers 201 with the slug id, key names only, the
// discovered catalog tools — and the credential sealed, never echoed.
func TestCreateSealsSecretsAndDiscovers(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)

	out := createServer(t, sqlDB, "Test Helper", srv, `,"headers":{"Authorization":"Bearer canary-value-xyz"}`)
	if out.Server.ID != "test-helper" {
		t.Fatalf("slug = %q, want test-helper", out.Server.ID)
	}
	if len(out.Tools) == 0 || out.Tools[0].ID != "test-helper:echo" {
		t.Fatalf("tools = %+v, want test-helper:echo first", out.Tools)
	}
	raw := body(do(t, sqlDB, http.MethodGet, "/mcp/servers", ""))
	if strings.Contains(raw, "canary-value-xyz") {
		t.Fatal("credential value leaked into a response")
	}
	if !strings.Contains(raw, `"headerKeys":["Authorization"]`) {
		t.Fatalf("list missing header key names: %s", raw)
	}
	if !strings.Contains(raw, `"allowPrivateNetwork":true`) {
		t.Fatalf("list dropped the destination approval: %s", raw)
	}

	tool, err := queries.GetTool(t.Context(), "test-helper:echo")
	if err != nil || tool.Kind != "mcp" || !tool.ServerID.Valid || tool.ServerID.String != "test-helper" {
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
	if secrets.Headers["Authorization"] != "Bearer canary-value-xyz" {
		t.Fatalf("sealed headers = %v", secrets.Headers)
	}
}

// A tool whose model name the provider cannot carry is skipped with a
// safe reason — reported, never renamed or silently dropped. The rest of
// the catalog installs.
func TestCreateSkipsUnrepresentableToolName(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	long := strings.Repeat("x", 60)
	srv := mcptest.Start(t, mcptest.WithToolNames(long))

	out := createServer(t, sqlDB, "Long Helper", srv, "")
	if len(out.SkippedTools) != 1 || out.SkippedTools[0].Name != long {
		t.Fatalf("skipped = %+v, want the over-long name reported", out.SkippedTools)
	}
	if !strings.Contains(out.SkippedTools[0].Reason, "exceeds") {
		t.Fatalf("skip reason %q does not explain the cap", out.SkippedTools[0].Reason)
	}
	if _, err := queries.GetTool(t.Context(), "long-helper:echo"); err != nil {
		t.Fatalf("representable tool missing: %v", err)
	}
	if _, err := queries.GetTool(t.Context(), mcp.CatalogID("long-helper", long)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("skipped tool landed in the catalog: %v", err)
	}
}

// Malformed bodies answer 400; a name collision answers 409 with the
// derived slug claimed.
func TestCreateValidationAndCollision(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)
	for _, tc := range []struct{ name, body string }{
		{"bad name", `{"name":"Bad-Name!","url":"http://127.0.0.1:1/mcp"}`},
		{"no url", `{"name":"Ok"}`},
		{"url userinfo", `{"name":"Ok","url":"http://user:pass@example.test/mcp"}`},
		{"bad timeout", createBody("Ok", srv.URL, "")},
	} {
		if tc.name == "bad timeout" {
			tc.body = strings.Replace(createBody("Ok", srv.URL, ""), `"timeoutMs":8000`, `"timeoutMs":0`, 1)
		}
		if rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d body %s, want 400", tc.name, rec.Code, body(rec))
		}
	}

	createServer(t, sqlDB, "Test Helper", srv, "")
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", createBody("Test Helper", srv.URL, ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("collision status = %d, want 409", rec.Code)
	}
}

// A present headers map replaces the stored one wholesale; a
// connection-breaking patch verifies first and writes nothing.
func TestUpdateReplaceSecretsAndReverify(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	createServer(t, sqlDB, "Test Helper", srv, `,"headers":{"Authorization":"Bearer canary-value-xyz"}`)

	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/test-helper",
		`{"headers":{"X-Other-Key":"second-value"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch headers status = %d body %s", rec.Code, body(rec))
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
	if _, still := secrets.Headers["Authorization"]; still {
		t.Fatal("replace did not drop the old key")
	}
	if secrets.Headers["X-Other-Key"] != "second-value" {
		t.Fatalf("replaced headers = %v", secrets.Headers)
	}
	row, err := queries.GetMcpServer(t.Context(), "test-helper")
	if err != nil || row.HeaderKeys != `["X-Other-Key"]` {
		t.Fatalf("header_keys column = %q err %v", row.HeaderKeys, err)
	}

	// A breaking destination is rejected before any write.
	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/test-helper",
		`{"url":"http://127.0.0.1:1/mcp"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("breaking patch status = %d body %s", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), "test-helper")
	if err != nil || !row.Url.Valid || row.Url.String != srv.URL {
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

// A URL change invalidates both approvals; the same patch may re-approve
// them, and an explicit false clears one.
func TestUpdateURLChangeResetsApprovals(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	createServer(t, sqlDB, "Approvals", srv, "")

	// Same endpoint, re-approved in the same request.
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/approvals",
		`{"url":"`+srv.URL+`","allowPrivateNetwork":true,"allowPlaintext":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-approve status = %d body %s", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "approvals")
	if err != nil || row.AllowPrivateNetwork != 1 || row.AllowPlaintext != 1 {
		t.Fatalf("row after re-approve = %+v err %v", row, err)
	}

	// Clearing plaintext touches no destination: no dial, flag cleared.
	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/approvals", `{"allowPlaintext":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear plaintext status = %d body %s", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), "approvals")
	if err != nil || row.AllowPlaintext != 0 || row.AllowPrivateNetwork != 1 {
		t.Fatalf("row after clearing plaintext = %+v err %v", row, err)
	}

	// A destination change alone drops both approvals — the new endpoint
	// was never approved. The dial then fails (no private approval), and
	// nothing is written.
	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/approvals",
		`{"url":"`+strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)+`"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("unapproved destination status = %d body %s, want 400", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), "approvals")
	if err != nil || row.Url.String != srv.URL || row.AllowPrivateNetwork != 1 {
		t.Fatalf("rejected url change wrote the row: %+v err %v", row, err)
	}
}

// Delete removes the row, its cascading tools and agent opt-outs, and
// the sealed blob.
func TestDeleteCascadesEverything(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	createServer(t, sqlDB, "Test Helper", srv, "")
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "research", Name: "Research", Icon: "globe"})
	if err := queries.AddAgentDisabledTool(t.Context(), db.AddAgentDisabledToolParams{
		AgentID: "research", ToolID: "test-helper:echo",
	}); err != nil {
		t.Fatalf("opt out: %v", err)
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
			t.Fatal("tool survived server delete")
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
	srv := mcptest.Start(t)
	createServer(t, sqlDB, "Test Helper", srv, "")

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
	if err := queries.UpdateMcpServer(context.Background(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:                "Test Helper",
		Url:                 sql.NullString{String: "http://127.0.0.1:1/mcp", Valid: true},
		HeaderKeys:          "[]",
		TimeoutMs:           sql.NullInt64{Int64: 300, Valid: true},
		AllowPrivateNetwork: 1, ID: "test-helper",
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

// createServerWith installs one fixture through a caller-supplied
// manager, so the adopted client is observable afterwards.
func createServerWith(t *testing.T, sqlDB *sql.DB, manager *mcp.Manager, name string, srv *mcptest.Server) apitypes.McpServerWithToolsResponse {
	t.Helper()
	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers", createBody(name, srv.URL, ""))
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
	srv := mcptest.Start(t)

	created := createServerWith(t, sqlDB, manager, "Toggle Helper", srv)
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

// A stored URL that predates today's validator (credential userinfo)
// must not block its own restriction: a pure disable persists with no
// dial and no 400 from the stored shape.
func TestPatchDisableSurvivesLegacyStoredURL(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)

	testdb.SeedMcpServer(t, sqlDB, "legacy", "https://user:pass@example.test/mcp")

	rec := doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/legacy", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d: %s", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "legacy")
	if err != nil || row.Enabled != 0 {
		t.Fatalf("row after disable: enabled=%d err=%v", row.Enabled, err)
	}
}

// A disable bundled with a URL move is not withdrawal-only: the new
// endpoint must verify like any other connection edit, and a failed
// check persists neither the URL nor the disable.
func TestDisableWithURLMoveVerifies(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)

	created := createServerWith(t, sqlDB, manager, "Move Helper", srv)
	id := created.Server.ID
	before, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}

	rec := doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id,
		`{"enabled":false,"url":"http://127.0.0.1:9/mcp"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("disable+move status = %d body %s, want 400 connection check", rec.Code, body(rec))
	}
	after, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("reread row: %v", err)
	}
	if after.Url != before.Url {
		t.Fatalf("failed patch persisted url %+v, want %+v", after.Url, before.Url)
	}
	if after.Enabled != before.Enabled {
		t.Fatalf("failed patch flipped enabled to %d", after.Enabled)
	}
}

// A create without headers always seals the effective (empty) set: an
// orphaned blob at the slug can never ride into the new install's
// requests.
func TestCreateWithoutHeadersClearsOrphanedBlob(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	store := newTestStore(t, sqlDB)

	orphan, err := json.Marshal(mcpserver.Secrets{
		Headers: map[string]string{"Authorization": "Bearer orphan-value"},
	})
	if err != nil {
		t.Fatalf("encode orphan blob: %v", err)
	}
	if err := store.Put(t.Context(), vault.NameRuntime,
		mcpserver.EntryName("orphan-helper"), mcpserver.Kind, orphan, nil); err != nil {
		t.Fatalf("seed orphan blob: %v", err)
	}

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		createBody("Orphan Helper", srv.URL, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	if got := srv.Header("Authorization"); got != "" {
		t.Fatalf("orphaned credential rode the dial: %q", got)
	}
	sealed, _, err := store.Get(t.Context(), vault.NameRuntime, mcpserver.EntryName("orphan-helper"))
	if err != nil {
		t.Fatalf("sealed blob: %v", err)
	}
	var secrets mcpserver.Secrets
	if err := json.Unmarshal(sealed, &secrets); err != nil {
		t.Fatalf("decode sealed blob: %v", err)
	}
	if len(secrets.Headers) != 0 {
		t.Fatalf("sealed headers = %v, want empty", secrets.Headers)
	}
	row, err := queries.GetMcpServer(t.Context(), "orphan-helper")
	if err != nil || row.HeaderKeys != `[]` {
		t.Fatalf("header_keys = %q err %v, want []", row.HeaderKeys, err)
	}
}

// Creation is an activation: the install commits with its server_enabled
// record, atomically.
func TestCreateRecordsServerEnabled(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		createBody("Enabled Helper", srv.URL, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	var enabled int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM tool_audit_events WHERE server_id = ? AND reason_code = 'server_enabled'`,
		"enabled-helper").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("server_enabled records = %d err %v, want 1", enabled, err)
	}
}

// A pure switch patch must not dial: the stored config is untouched and
// the request answers immediately even when the stored endpoint is dead.
func TestPureEnabledPatchSkipsVerify(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	created := createServer(t, sqlDB, "Pure Toggle", srv, "")
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

// TestPatchFailedVerifyAnswersInTime: an endpoint that never answers must
// fail the patch inside the verify budget, not the configured timeout,
// and write nothing.
func TestPatchFailedVerifyAnswersInTime(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)
	created := createServer(t, sqlDB, "Fast Fail", srv, "")

	slow := mcptest.Start(t, mcptest.WithDelay(20*time.Second))
	start := time.Now()
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+created.Server.ID,
		`{"url":"`+slow.URL+`"}`)
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
	if row.Url.String != srv.URL {
		t.Fatalf("failed patch rewrote the stored url: %q", row.Url.String)
	}
}

// A catalog-write failure after the verified dial rolls the whole install
// back: no row, no sealed blob, no cached client, no catalog tools —
// and the name is free for a clean retry.
func TestCreateRollsBackWhenDiscoveryFails(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	// Verification's listing is published as-is, so the failure has
	// to come from the catalog write itself.
	srv := mcptest.Start(t)
	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_tool_insert BEFORE INSERT ON tools
		BEGIN SELECT RAISE(ABORT, 'catalog down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers", createBody("Flaky", srv.URL, ""))
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
	if tools, err := queries.ListTools(t.Context()); err != nil || len(tools) != 0 {
		t.Fatalf("rollback left %d tools (err %v)", len(tools), err)
	}
	if _, err := sqlDB.Exec(`DROP TRIGGER fail_tool_insert;`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	// The failed install left no trace: the same name installs cleanly.
	createServer(t, sqlDB, "Flaky", mcptest.Start(t), "")
}

// Concurrent installs of the same name: one wins, the other reads 409
// (pre-check or the insert's unique constraint) — never a 500.
func TestCreateDuplicateNameRace(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store := newTestStore(t, sqlDB)
	logger := zerolog.Nop()
	srv := mcptest.Start(t)

	const attempts = 3
	for round := range attempts {
		name := fmt.Sprintf("Race%d", round)
		req := createBody(name, srv.URL, "")
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

// unsealedHeaders reads the runtime blob's headers — the values the next
// dial would use.
func unsealedHeaders(t *testing.T, sqlDB *sql.DB, slug string) map[string]string {
	t.Helper()
	raw, _, err := newTestStore(t, sqlDB).Get(t.Context(), vault.NameRuntime, mcpserver.EntryName(slug))
	if err != nil {
		t.Fatalf("unseal %q: %v", slug, err)
	}
	var secrets mcpserver.Secrets
	if err := json.Unmarshal(raw, &secrets); err != nil {
		t.Fatalf("decode secrets %q: %v", slug, err)
	}
	return secrets.Headers
}

// A seal failure after the row insert must not orphan the install: the
// row would block the name while the vault holds nothing it promises.
// Nothing is left behind and a clean retry installs.
func TestCreateSealFailureLeavesNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)

	mustExec(t, sqlDB, `CREATE TRIGGER fail_vault_write BEFORE INSERT ON vault_entries BEGIN SELECT RAISE(ABORT, 'vault down'); END;`)
	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		createBody("Flaky", srv.URL, `,"headers":{"Authorization":"Bearer canary-value-xyz"}`))
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
	if tools, err := queries.ListTools(t.Context()); err != nil || len(tools) != 0 {
		t.Fatalf("seal failure left %d tools (err %v)", len(tools), err)
	}
	// The failed install left no trace: the same name installs cleanly.
	mustExec(t, sqlDB, `DROP TRIGGER fail_vault_write;`)
	createServer(t, sqlDB, "Flaky", srv, "")
}

// A seal failure during a secrets-replacing patch writes nothing: the
// row keeps its stored config and the blob keeps the stored values —
// key-name columns never promise values the vault lacks.
func TestUpdateSealFailureKeepsStoredState(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	id := createServer(t, sqlDB, "Seal Fail", srv, `,"headers":{"Authorization":"Bearer canary-value-xyz"}`).Server.ID

	mustExec(t, sqlDB, `CREATE TRIGGER fail_vault_write BEFORE INSERT ON vault_entries BEGIN SELECT RAISE(ABORT, 'vault down'); END;`)
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"headers":{"X-Other":"rotated"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.HeaderKeys != `["Authorization"]` {
		t.Fatalf("row header keys = %s, want the stored key names untouched", row.HeaderKeys)
	}
	if headers := unsealedHeaders(t, sqlDB, id); headers["Authorization"] != "Bearer canary-value-xyz" || headers["X-Other"] != "" {
		t.Fatalf("sealed headers = %v, want the stored values untouched", headers)
	}
}

// A row-write failure after a successful re-seal restores the previously
// sealed values: row and blob move together or not at all.
func TestUpdateRowFailureRestoresSealedSecrets(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	id := createServer(t, sqlDB, "Row Fail", srv, `,"headers":{"Authorization":"Bearer canary-value-xyz"}`).Server.ID

	mustExec(t, sqlDB, `CREATE TRIGGER fail_row_write BEFORE UPDATE ON mcp_servers BEGIN SELECT RAISE(ABORT, 'db down'); END;`)
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"headers":{"X-Other":"rotated"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.HeaderKeys != `["Authorization"]` {
		t.Fatalf("row header keys = %s, want the stored key names", row.HeaderKeys)
	}
	if headers := unsealedHeaders(t, sqlDB, id); headers["Authorization"] != "Bearer canary-value-xyz" || headers["X-Other"] != "" {
		t.Fatalf("sealed headers = %v, want the stored values restored", headers)
	}
}

// The definition endpoint returns the stored, sanitized definition for
// inspection, and nothing across server bounds.
func TestToolDefinitionEndpoint(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)
	createServer(t, sqlDB, "Defs", srv, "")

	rec := do(t, sqlDB, http.MethodGet, "/mcp/servers/defs/tools/defs:echo/definition", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, body(rec))
	}
	var out apitypes.McpToolDefinitionResponse
	if err := json.Unmarshal([]byte(body(rec)), &out); err != nil {
		t.Fatalf("decode definition: %v", err)
	}
	if !strings.Contains(out.DefinitionJSON, `"echo"`) || !strings.Contains(out.DefinitionJSON, `"inputSchema"`) {
		t.Fatalf("definition = %s, want the advertised tool", out.DefinitionJSON)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.DefinitionJSON), &decoded); err != nil {
		t.Fatalf("definition is not a JSON object: %v", err)
	}
	// The inspector needs the authority beside the definition: the
	// endpoint and what the connection carries.
	if out.Endpoint != srv.URL {
		t.Fatalf("endpoint = %q, want %q", out.Endpoint, srv.URL)
	}
	if out.HasCredentials {
		t.Fatal("hasCredentials = true for a header-less server")
	}
	if !out.AllowPlaintext {
		t.Fatal("allowPlaintext = false, want the stored exception")
	}

	// A server with sealed headers reports them as credential-bearing.
	const credential = "canary-definition-value"
	withCreds := createServer(t, sqlDB, "Creds", mcptest.Start(t),
		`,"headers":{"Authorization":"Bearer `+credential+`"}`)
	rec = do(t, sqlDB, http.MethodGet,
		"/mcp/servers/"+withCreds.Server.ID+"/tools/"+withCreds.Server.ID+":echo/definition", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("credentialed definition status = %d body %s", rec.Code, body(rec))
	}
	var credentialed apitypes.McpToolDefinitionResponse
	if err := json.Unmarshal([]byte(body(rec)), &credentialed); err != nil {
		t.Fatalf("decode credentialed definition: %v", err)
	}
	if !credentialed.HasCredentials {
		t.Fatal("hasCredentials = false for a server with sealed headers")
	}
	if strings.Contains(body(rec), credential) {
		t.Fatalf("definition response leaked the credential: %s", body(rec))
	}

	// A tool from another server is a 404, never a cross-server read.
	other := createServer(t, sqlDB, "Other", mcptest.Start(t), "")
	if rec := do(t, sqlDB, http.MethodGet,
		"/mcp/servers/defs/tools/"+other.Server.ID+":echo/definition", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-server definition = %d, want 404", rec.Code)
	}
	if rec := do(t, sqlDB, http.MethodGet, "/mcp/servers/ghost/tools/x/definition", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown server definition = %d, want 404", rec.Code)
	}
}

// Credential-bearing plaintext to a public destination is denied before
// any write — by the dial policy, not by a UI-only rule.
func TestCreateRejectsPublicPlaintextCredentials(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)

	// A literal public address: the class check runs before connecting,
	// so no network is involved.
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", `{
		"name": "Public Plain",
		"url": "http://93.184.216.34/mcp",
		"headers": {"Authorization": "Bearer canary"},
		"allowPrivateNetwork": true,
		"allowPlaintext": true
	}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want the connection-check 400", rec.Code, body(rec))
	}
	if !strings.Contains(body(rec), "insecure transport denied") {
		t.Fatalf("body %s does not name the plaintext-credential denial", body(rec))
	}
	if servers, err := queries.ListMcpServers(t.Context()); err != nil || len(servers) != 0 {
		t.Fatalf("denied create wrote %d rows (err %v)", len(servers), err)
	}
}

// A link-local / metadata destination is denied even with the private
// exception.
func TestCreateRejectsMetadataDestination(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", `{
		"name": "Metadata",
		"url": "http://169.254.169.254/mcp",
		"allowPrivateNetwork": true,
		"timeoutMs": 300
	}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "link-local") {
		t.Fatalf("status = %d body %s, want the link-local denial", rec.Code, body(rec))
	}
}

// A loopback endpoint without its destination approval fails verification:
// the policy is enforced before anything is written.
func TestCreateRejectsUnapprovedDestination(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers",
		`{"name":"No Approval","url":"`+srv.URL+`","timeoutMs":1000}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want the connection-check 400", rec.Code, body(rec))
	}
	if !strings.Contains(body(rec), "non-public address") {
		t.Fatalf("body %s does not name the destination denial", body(rec))
	}
	if servers, err := queries.ListMcpServers(t.Context()); err != nil || len(servers) != 0 {
		t.Fatalf("denied create wrote %d rows (err %v)", len(servers), err)
	}

	// The same endpoint with its approval installs.
	createServer(t, sqlDB, "With Approval", srv, "")
}

// A refresh that returns a security-invalid generation answers 502, pauses
// the catalog, and keeps the previous tools listed.
func TestRefreshPausesOnRejectedGeneration(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	good := mcptest.Start(t)
	createServer(t, sqlDB, "Paused Helper", good, "")

	bomb := mcptest.Start(t, mcptest.WithSchemaDepth(64))
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:                "Paused Helper",
		Url:                 sql.NullString{String: bomb.URL, Valid: true},
		HeaderKeys:          "[]",
		TimeoutMs:           sql.NullInt64{Int64: 5000, Valid: true},
		AllowPrivateNetwork: 1, AllowPlaintext: 1, ID: "paused-helper",
	}); err != nil {
		t.Fatalf("repoint row: %v", err)
	}

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers/paused-helper/refresh", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("refresh status = %d body %s, want 502", rec.Code, body(rec))
	}
	if !strings.Contains(body(rec), mcp.CodeCatalogInvalidSchema) {
		t.Fatalf("body %s does not carry the catalog code", body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "paused-helper")
	if err != nil || row.CatalogState != "paused" {
		t.Fatalf("catalog state = %+v err %v, want paused", row.CatalogState, err)
	}
	if _, err := queries.GetTool(t.Context(), "paused-helper:echo"); err != nil {
		t.Fatalf("paused catalog lost its previous rows: %v", err)
	}

	// A healthy refresh unpauses.
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:                "Paused Helper",
		Url:                 sql.NullString{String: good.URL, Valid: true},
		HeaderKeys:          "[]",
		TimeoutMs:           sql.NullInt64{Int64: 5000, Valid: true},
		AllowPrivateNetwork: 1, AllowPlaintext: 1, ID: "paused-helper",
	}); err != nil {
		t.Fatalf("restore row: %v", err)
	}
	rec = do(t, sqlDB, http.MethodPost, "/mcp/servers/paused-helper/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("recovery refresh = %d body %s, want 200", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), "paused-helper")
	if err != nil || row.CatalogState != "active" {
		t.Fatalf("catalog state after recovery = %q err %v", row.CatalogState, err)
	}
}

// One listing per install — verification's listing is published, not
// repeated.
func TestCreateVerifiesWithSingleListing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)

	createServer(t, sqlDB, "One Listing", srv, "")
	if got := srv.Lists(); got != 1 {
		t.Fatalf("tools/list requests = %d, want 1", got)
	}
}

// The whole check shares one 10-second budget across every listing
// page, so a slow server answers the request instead of hanging it.
func TestVerifyListingBudgetCapped(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t,
		mcptest.WithPageSize(1),
		mcptest.WithToolCount(2),
		mcptest.WithListPageDelay(6*time.Second))

	start := time.Now()
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", createBody("Slow Pages", srv.URL, ""))
	elapsed := time.Since(start)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want 400 connection-check failure", rec.Code, body(rec))
	}
	if elapsed > mcp.VerifyTimeout+5*time.Second {
		t.Fatalf("slow listing answered after %v, want <= %v", elapsed, mcp.VerifyTimeout)
	}
	if servers, err := db.New(sqlDB).ListMcpServers(t.Context()); err != nil || len(servers) != 0 {
		t.Fatalf("failed verify wrote %d rows (err %v)", len(servers), err)
	}
}

// The approval record precedes the credential transmission that
// verification implies — an unauditable install never dials.
func TestCreateAuditsBeforeCredentialTransmission(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)
	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", createBody("Unaudited", srv.URL,
		`,"headers":{"Authorization":"Bearer canary-install"}`))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body(rec), "audit unavailable") {
		t.Fatalf("status = %d body %s, want 500 audit unavailable", rec.Code, body(rec))
	}
	if got := srv.Initializes(); got != 0 {
		t.Fatalf("unaudited install dialed %d times, want 0", got)
	}
	if servers, err := db.New(sqlDB).ListMcpServers(t.Context()); err != nil || len(servers) != 0 {
		t.Fatalf("unaudited install wrote %d rows (err %v)", len(servers), err)
	}
}

// Withdrawing destination/plaintext authority needs no successful
// connection check — it persists and cancels against a dead endpoint.
func TestUpdateWithdrawalNeedsNoDial(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers", createBody("Restricted", srv.URL, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	if _, ok := manager.Get("restricted"); !ok {
		t.Fatal("create did not install the live client")
	}
	// The endpoint is gone: a withdrawal must not care.
	srv.Close()
	dials := srv.Initializes()

	rec = doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/restricted",
		`{"allowPrivateNetwork":false,"allowPlaintext":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("withdrawal status = %d body %s", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "restricted")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.AllowPrivateNetwork != 0 || row.AllowPlaintext != 0 {
		t.Fatalf("withdrawal did not persist: private=%d plaintext=%d",
			row.AllowPrivateNetwork, row.AllowPlaintext)
	}
	if _, ok := manager.Get("restricted"); ok {
		t.Fatal("withdrawal left the old client serving under the removed policy")
	}
	if got := srv.Initializes(); got != dials {
		t.Fatalf("withdrawal dialed %d times, want 0", got-dials)
	}

	var reasons []string
	if rows, err := queries.ListMcpServerTools(t.Context(), sql.NullString{String: "restricted", Valid: true}); err != nil || len(rows) == 0 {
		t.Fatalf("catalog rows after withdrawal = %d (err %v)", len(rows), err)
	}
	audit, err := sqlDB.Query(`SELECT reason_code FROM tool_audit_events WHERE server_id = ? ORDER BY id`,
		"restricted")
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	defer func() { _ = audit.Close() }()
	for audit.Next() {
		var reason string
		if err := audit.Scan(&reason); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		reasons = append(reasons, reason)
	}
	for _, want := range []string{"destination_revoked", "plaintext_revoked"} {
		found := false
		for _, reason := range reasons {
			if reason == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("audit reasons = %v, want %q", reasons, want)
		}
	}
}

// Newly added authority is recorded before it activates; a failed
// record writes nothing and dials nothing.
func TestUpdateAuthorityAddAuditFailureBlocks(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)

	// Install without the plaintext exception, then add it.
	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers",
		`{"name":"Add Later","url":"`+srv.URL+`",`+
			`"allowPrivateNetwork":true,"allowPlaintext":false,"timeoutMs":8000}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	dials := srv.Initializes()
	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/add-later", `{"allowPlaintext":true}`)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body(rec), "audit unavailable") {
		t.Fatalf("status = %d body %s, want 500 audit unavailable", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "add-later")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.AllowPlaintext != 0 {
		t.Fatal("unaudited authority activated")
	}
	if got := srv.Initializes(); got != dials {
		t.Fatalf("unaudited patch dialed %d times, want 0", got-dials)
	}
}

// An authority addition that fails verification is withdrawn with an
// explicit record, and the row keeps its previous state.
func TestUpdateVerifyFailureCompensates(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers",
		`{"name":"Will Fail","url":"`+srv.URL+`",`+
			`"allowPrivateNetwork":true,"allowPlaintext":false,"timeoutMs":8000}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}

	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/will-fail",
		`{"url":"http://127.0.0.1:1/mcp","allowPrivateNetwork":true,"allowPlaintext":true}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want 400 connection-check failure", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "will-fail")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.Url.String != srv.URL || row.AllowPlaintext != 0 {
		t.Fatalf("failed patch persisted: url=%q plaintext=%d", row.Url.String, row.AllowPlaintext)
	}
	var notActivated bool
	rows, err := sqlDB.Query(`SELECT reason_code FROM tool_audit_events WHERE server_id = ?`, "will-fail")
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		if reason == "plaintext_not_activated" {
			notActivated = true
		}
	}
	if !notActivated {
		t.Fatal("failed verification did not withdraw the approval it wrote")
	}
}

// A missing server answers exactly one body: the "already answered" signal
// must stop the handler before a second write appends to the response.
func TestMissingServerAnswersOneBody(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	for _, tc := range []struct {
		method, target, body string
	}{
		{http.MethodPatch, "/mcp/servers/ghost", `{}`},
		{http.MethodDelete, "/mcp/servers/ghost", ""},
		{http.MethodPost, "/mcp/servers/ghost/refresh", ""},
		{http.MethodGet, "/mcp/servers/ghost/tools/ghost:x/definition", ""},
	} {
		rec := do(t, sqlDB, tc.method, tc.target, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d body %s, want 404", tc.method, tc.target, rec.Code, body(rec))
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(body(rec)), &decoded); err != nil {
			t.Fatalf("%s %s body = %s, want exactly one JSON object", tc.method, tc.target, body(rec))
		}
		if want := `MCP server "ghost" not found`; decoded["message"] != want {
			t.Fatalf("%s %s message = %v, want %q", tc.method, tc.target, decoded["message"], want)
		}
	}
}

// A stored URL carrying credentials in its userinfo is never echoed
// back — the runtime rejects it, and the view masks it.
func TestUserinfoURLRowIsMasked(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	testdb.SeedMcpServer(t, sqlDB, "userinfo", "http://u:secret-pass@127.0.0.1:3333/mcp")
	testdb.SeedMcpServer(t, sqlDB, "passonly", "http://:only-pass@127.0.0.1:3333/mcp")

	rec := do(t, sqlDB, http.MethodGet, "/mcp/servers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d body %s", rec.Code, body(rec))
	}
	for _, leaked := range []string{"secret-pass", "only-pass", "u:", "u@", ":only-pass@"} {
		if strings.Contains(body(rec), leaked) {
			t.Fatalf("view leaked %q: %s", leaked, body(rec))
		}
	}
	if !strings.Contains(body(rec), url.UserPassword("[redacted]", "[redacted]").String()) {
		t.Fatalf("view did not mask the userinfo: %s", body(rec))
	}

	// The runtime refuses to connect such a row at all.
	if _, err := managerConnect(t, sqlDB); err == nil {
		t.Fatal("a userinfo endpoint connected")
	}
}

// managerConnect dials one stored server the way boot discovery would.
func managerConnect(t *testing.T, sqlDB *sql.DB) (*mcp.Client, error) {
	t.Helper()
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	row, err := db.New(sqlDB).GetMcpServer(t.Context(), "userinfo")
	if err != nil {
		t.Fatalf("read userinfo row: %v", err)
	}
	cfg, err := mcpserver.Config(t.Context(), row, newTestStore(t, sqlDB))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return manager.Connect(t.Context(), cfg)
}

// A tightening persists even when its own audit record fails —
// the restriction is not held hostage by the trail it degrades,
// and it still needs no dial.
func TestTighteningAppliesWhenAuditFails(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers", createBody("Unlogged", srv.URL, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}
	// The endpoint is gone and every audit write fails: the tightening
	// must not care about either.
	srv.Close()
	dials := srv.Initializes()
	mustExec(t, sqlDB, `CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`)

	rec = doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/unlogged",
		`{"allowPrivateNetwork":false,"allowPlaintext":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("withdrawal status = %d body %s, want 200 despite the failed audit", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), "unlogged")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.AllowPrivateNetwork != 0 || row.AllowPlaintext != 0 {
		t.Fatalf("tightening did not persist: private=%d plaintext=%d",
			row.AllowPrivateNetwork, row.AllowPlaintext)
	}
	if _, ok := manager.Get("unlogged"); ok {
		t.Fatal("tightening left the live client serving under the removed policy")
	}
	if got := srv.Initializes(); got != dials {
		t.Fatalf("withdrawal dialed %d times, want 0", got-dials)
	}
	// The revoke records are exactly what failed: none may have landed.
	var revoked int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events
		WHERE server_id = ? AND reason_code IN ('destination_revoked', 'plaintext_revoked')`,
		"unlogged").Scan(&revoked); err != nil {
		t.Fatalf("count revoke records: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("failed audit still wrote %d revoke records, want 0", revoked)
	}
}

// A URL move's revocation tracks the persisted outcome: a failed
// verify writes nothing (the old endpoint keeps its approvals, so no
// revoke record may claim otherwise), a successful move records the
// old authority's end.
func TestUrlMoveRevocationMatchesPersistedOutcome(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t)
	id := createServer(t, sqlDB, "Moving", srv, "").Server.ID

	// Failed move: dead endpoint, nothing persisted.
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"url":"http://127.0.0.1:1/mcp"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want 400 connection-check failure", rec.Code, body(rec))
	}
	var revoked int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events
		WHERE server_id = ? AND reason_code IN ('destination_revoked', 'plaintext_revoked')`, id).
		Scan(&revoked); err != nil || revoked != 0 {
		t.Fatalf("failed move wrote %d revoke records, want 0 (err %v)", revoked, err)
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || row.Url.String != srv.URL || row.AllowPrivateNetwork != 1 || row.AllowPlaintext != 1 {
		t.Fatalf("failed move changed the row: %+v err %v", row, err)
	}

	// Successful move: the old endpoint's authority ends after the write.
	// The new loopback endpoint is only reachable re-approved in the same
	// patch, so the flags stay set — the revoke records cover the old
	// endpoint, not the flags.
	other := mcptest.Start(t)
	rec = do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id,
		`{"url":"`+other.URL+`","allowPrivateNetwork":true,"allowPlaintext":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("move status = %d body %s", rec.Code, body(rec))
	}
	for _, want := range []string{"destination_revoked", "plaintext_revoked"} {
		var found int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events
			WHERE server_id = ? AND reason_code = ?`, id, want).Scan(&found); err != nil || found < 1 {
			t.Fatalf("reason %q recorded %d times, want >= 1 (err %v)", want, found, err)
		}
	}
	moved, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || moved.Url.String != other.URL {
		t.Fatalf("move did not persist: %+v err %v", moved, err)
	}
}

// A URL move that re-approves already-set flags records the NEW
// endpoint's approvals before its credentials are transmitted — an
// unauditable one dials nothing.
func TestUrlMoveReapproveRecordsNewEndpoint(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	srv := mcptest.Start(t)
	other := mcptest.Start(t)
	id := createServer(t, sqlDB, "Moving Too", srv, "").Server.ID // flags start 1/1

	patch := `{"url":"` + other.URL + `","allowPrivateNetwork":true,"allowPlaintext":true}`
	rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, patch)
	if rec.Code != http.StatusOK {
		t.Fatalf("move status = %d body %s", rec.Code, body(rec))
	}
	for _, want := range []string{"destination_approved", "plaintext_approved"} {
		var found int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events
			WHERE server_id = ? AND reason_code = ?`, id, want).Scan(&found); err != nil || found < 1 {
			t.Fatalf("reason %q recorded %d times, want >= 1 (err %v)", want, found, err)
		}
	}

	// Same patch with auditing down: no record, no dial, nothing written.
	sqlDB2 := testdb.Open(t, true)
	id2 := createServer(t, sqlDB2, "Unauditable", srv, "").Server.ID
	dials := other.Initializes()
	mustExec(t, sqlDB2, `CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`)
	rec = do(t, sqlDB2, http.MethodPatch, "/mcp/servers/"+id2,
		`{"url":"`+other.URL+`","allowPrivateNetwork":true,"allowPlaintext":true}`)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body(rec), "audit unavailable") {
		t.Fatalf("status = %d body %s, want 500 audit unavailable", rec.Code, body(rec))
	}
	if got := other.Initializes(); got != dials {
		t.Fatalf("unaudited move dialed %d times, want 0", got-dials)
	}
}

// An install that fails verification compensates the approvals it
// wrote first — the append-only trail keeps both, so every approve
// record must be paired by a not_activated revocation; no approval
// stands unpaired as if authority had been granted.
func TestCreateVerifyFailureWithdrawsApprovals(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	rec := do(t, sqlDB, http.MethodPost, "/mcp/servers", createBody("Denied", "http://127.0.0.1:1/mcp", ""))
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), "Connection check failed") {
		t.Fatalf("status = %d body %s, want 400 connection-check failure", rec.Code, body(rec))
	}
	var approved int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events
		WHERE server_id = ? AND reason_code IN ('destination_approved', 'plaintext_approved')`,
		"denied").Scan(&approved); err != nil {
		t.Fatalf("count approve records: %v", err)
	}
	if approved != 2 {
		t.Fatalf("failed verify left %d approve records, want 2 (the approvals were written)", approved)
	}
	var withdrawn int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events
		WHERE server_id = ? AND reason_code IN ('destination_not_activated', 'plaintext_not_activated')`,
		"denied").Scan(&withdrawn); err != nil {
		t.Fatalf("count withdrawal records: %v", err)
	}
	if withdrawn != 2 {
		t.Fatalf("failed verify wrote %d withdrawal records, want 2", withdrawn)
	}
}

// recordingCanceler records the catalog ids a write canceled (and the
// servers a whole-server revocation hit).
type recordingCanceler struct {
	mu      sync.Mutex
	tools   []string
	servers []string
}

func (c *recordingCanceler) record(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools = append(c.tools, id)
	return 1
}

func (c *recordingCanceler) canceled() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.tools...)
}

func (c *recordingCanceler) canceledServers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.servers...)
}

func (c *recordingCanceler) CancelServerTools(serverID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.servers = append(c.servers, serverID)
	return 1
}

func (c *recordingCanceler) CancelToolCalls(_, toolID string) int { return c.record(toolID) }

func (c *recordingCanceler) CancelAgentServerTools(string, string) int { return 0 }

func (c *recordingCanceler) CancelAgentToolCalls(_, _, toolID string) int { return c.record(toolID) }

// A newly discovered tool is globally off: discovery publishes the
// definition, the operator keeps the switch.
func TestNewServerToolsGloballyOff(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	out := createServer(t, sqlDB, "Fresh", mcptest.Start(t), "")
	if len(out.Tools) == 0 {
		t.Fatal("create published no tools")
	}
	for _, tool := range out.Tools {
		row, err := queries.GetTool(t.Context(), tool.ID)
		if err != nil {
			t.Fatalf("read %q: %v", tool.ID, err)
		}
		if row.Enabled != 0 {
			t.Fatalf("%q enabled = %d, want 0 (opt in, never assume)", tool.ID, row.Enabled)
		}
		if row.DefinitionJson == "" {
			t.Fatalf("%q carries no definition", tool.ID)
		}
	}
}

// A refresh reuses the healthy connection: a call already on the wire
// survives it and completes exactly once on the connection it started
// on. A later definition change lands in the catalog without touching
// any per-agent state.
func TestRefreshKeepsInFlightCall(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t, mcptest.WithBlockingEcho())
	if rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		createBody("Held", srv.URL, "")); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}

	client, ok := manager.Get("held")
	if !ok {
		t.Fatal("create installed no client")
	}
	inv, ok := client.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after create")
	}

	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := inv.InvokableRun(context.Background(), `{"text":"kept"}`)
		done <- struct {
			out string
			err error
		}{out, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for srv.EchoCalls() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if srv.EchoCalls() == 0 {
		t.Fatal("the call never reached the fixture")
	}

	// A refresh lands while the call is on the wire.
	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers/held/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body %s", rec.Code, body(rec))
	}
	if got := srv.Initializes(); got != 1 {
		t.Fatalf("refresh dialed %d times, want the healthy connection reused", got)
	}
	select {
	case out := <-done:
		t.Fatalf("the refresh aborted the in-flight call: (%q, %v)", out.out, out.err)
	case <-time.After(100 * time.Millisecond):
	}

	srv.ReleaseEcho()
	select {
	case out := <-done:
		if out.err != nil || out.out != "kept" {
			t.Fatalf("in-flight call = (%q, %v), want it to complete on its own connection",
				out.out, out.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight call never returned")
	}
	if got := srv.EchoCalls(); got != 1 {
		t.Fatalf("fixture saw %d echo calls, want exactly one (no replay)", got)
	}

	// The endpoint now advertises a changed definition; the next refresh
	// publishes it, and no per-agent state moves: availability is the
	// switches, not a definition binding.
	srv.SetEcho("Echoes its input text, loudly.", "")
	rec = doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers/held/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh after change status = %d body %s", rec.Code, body(rec))
	}
	tool, err := queries.GetTool(t.Context(), "held:echo")
	if err != nil {
		t.Fatalf("read tool: %v", err)
	}
	if !strings.Contains(tool.DefinitionJson, "loudly") {
		t.Fatalf("definition = %s, want the refreshed text", tool.DefinitionJson)
	}
	var agentRows int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT (SELECT COUNT(*) FROM agent_disabled_tools) + (SELECT COUNT(*) FROM agent_mcp_preferences)`).
		Scan(&agentRows); err != nil {
		t.Fatalf("count per-agent rows: %v", err)
	}
	if agentRows != 0 {
		t.Fatalf("a refresh wrote %d per-agent rows, want none", agentRows)
	}
}

// A refresh cancels the calls of the tools it pruned, and nothing else:
// a surviving tool's call keeps running.
func TestRemovedToolCancels(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	if rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers",
		createBody("Rem", srv.URL, "")); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body %s", rec.Code, body(rec))
	}

	// A row the server no longer reports, as a prior discovery left it.
	if _, err := sqlDB.Exec(
		`INSERT INTO tools (id, name, description, kind, server_id) VALUES ('rem:vanished', 'vanished', 'gone', 'mcp', 'rem')`); err != nil {
		t.Fatalf("seed vanished tool: %v", err)
	}

	canceler := &recordingCanceler{}
	rec := doWithCanceler(t, sqlDB, manager, canceler, http.MethodPost, "/mcp/servers/rem/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body %s", rec.Code, body(rec))
	}
	if got := canceler.canceled(); len(got) != 1 || got[0] != "rem:vanished" {
		t.Fatalf("canceled = %v, want only the vanished tool", got)
	}
	if _, err := queries.GetTool(t.Context(), "rem:vanished"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("vanished tool survived the refresh: %v", err)
	}
	if _, err := queries.GetTool(t.Context(), "rem:echo"); err != nil {
		t.Fatalf("surviving tool lost: %v", err)
	}
}

// An endpoint or credential change retires admitted work: the old client
// drains without aborting its calls, so the patch revokes them. A rename
// or a policy change leaves them running.
func TestConnectionEditCancelsAdmittedCalls(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	created := createServerWith(t, sqlDB, manager, "Cancel Helper", mcptest.Start(t))
	id := created.Server.ID
	canceler := &recordingCanceler{}

	patch := func(payload string) *httptest.ResponseRecorder {
		return doWithCanceler(t, sqlDB, manager, canceler, http.MethodPatch, "/mcp/servers/"+id, payload)
	}
	wantRevoked := func(step string, want int) {
		t.Helper()
		got := canceler.canceledServers()
		if len(got) != want {
			t.Fatalf("%s: revoked %v, want %d whole-server cancellation(s)", step, got, want)
		}
		for _, serverID := range got {
			if serverID != id {
				t.Fatalf("%s: revoked server %q, want %q", step, serverID, id)
			}
		}
	}

	if rec := patch(`{"name":"Renamed"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body %s", rec.Code, body(rec))
	}
	wantRevoked("rename", 0)

	// Narrowing an approval is a withdrawal: the client goes and every
	// admitted call with it.
	if rec := patch(`{"allowPrivateNetwork":false,"allowPlaintext":false}`); rec.Code != http.StatusOK {
		t.Fatalf("narrow status = %d body %s", rec.Code, body(rec))
	}
	wantRevoked("approval withdrawal", 1)

	// Widening them back dials nothing and revokes nothing.
	if rec := patch(`{"allowPrivateNetwork":true,"allowPlaintext":true}`); rec.Code != http.StatusOK {
		t.Fatalf("widen status = %d body %s", rec.Code, body(rec))
	}
	wantRevoked("approval widening", 1)

	// A credential change moves to a new connection: the calls admitted
	// on the old one are revoked.
	if rec := patch(`{"headers":{"X-Trace":"abc"}}`); rec.Code != http.StatusOK {
		t.Fatalf("header status = %d body %s", rec.Code, body(rec))
	}
	wantRevoked("credential change", 2)

	// An endpoint move likewise.
	moved := mcptest.Start(t)
	if rec := patch(`{"url":"` + moved.URL + `","allowPrivateNetwork":true,"allowPlaintext":true}`); rec.Code != http.StatusOK {
		t.Fatalf("url status = %d body %s", rec.Code, body(rec))
	}
	wantRevoked("endpoint move", 3)
	// A timeout change re-verifies the same authority: the connection
	// shape moved, but no credential or endpoint did, so nothing is
	// revoked.
	if rec := patch(`{"timeoutMs":15000}`); rec.Code != http.StatusOK {
		t.Fatalf("timeout status = %d body %s", rec.Code, body(rec))
	}
	wantRevoked("timeout change", 3)

	// Headers are write-only: a resubmission equal to the stored map is
	// still a credential change, so the admitted work is revoked again.
	if rec := patch(`{"headers":{"X-Trace":"abc"}}`); rec.Code != http.StatusOK {
		t.Fatalf("identical headers status = %d body %s", rec.Code, body(rec))
	}
	wantRevoked("identical headers", 4)
}

// A connection check lists the catalog once and that listing is what
// publishes — a same-config edit included, where a cached client with an
// older committed generation must not serve the new definition.
func TestConnectionCheckListsOnce(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	created := createServerWith(t, sqlDB, manager, "Once Helper", srv)
	id := created.Server.ID

	// A headers map equal to the stored (empty) one: a credential patch
	// that does not move the connection. The verified client replaces the
	// cached one — Replace retires it by draining, never aborting — and
	// the verified listing is what publishes.
	before := srv.Lists()
	rec := doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id, `{"headers":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-config patch status = %d body %s", rec.Code, body(rec))
	}
	if got := srv.Lists() - before; got != 1 {
		t.Fatalf("same-config patch listed the catalog %d times, want exactly verification's", got)
	}
	if _, ok := manager.Get(id); !ok {
		t.Fatalf("patch left no live client for %q", id)
	}
	var published apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal([]byte(body(rec)), &published); err != nil {
		t.Fatalf("decode patch body: %v", err)
	}
	if len(published.Tools) != len(created.Tools) {
		t.Fatalf("published %d tools, want the verified listing's %d",
			len(published.Tools), len(created.Tools))
	}

	// A URL move publishes the verified client's listing; neither endpoint
	// is listed twice.
	before = srv.Lists()
	moved := mcptest.Start(t)
	rec = doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id,
		`{"url":"`+moved.URL+`","allowPrivateNetwork":true,"allowPlaintext":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("url patch status = %d body %s", rec.Code, body(rec))
	}
	if got := srv.Lists() - before; got != 0 {
		t.Fatalf("old endpoint listed %d times after the move, want 0", got)
	}
	if got := moved.Lists(); got != 1 {
		t.Fatalf("new endpoint listed %d times, want exactly verification's", got)
	}
}

// A rejected generation pauses the catalog in place: the remote answered
// with a listing the catalog refuses, so the healthy client stays
// installed and no reconnect is attempted to re-list the same rejection.
func TestRejectedGenerationKeepsCachedClient(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	created := createServerWith(t, sqlDB, manager, "Reject Helper", srv)
	id := created.Server.ID
	cached, ok := manager.Get(id)
	if !ok {
		t.Fatalf("create did not adopt a client for %q", id)
	}

	deep := strings.Repeat(`{"type":"array","items":`, 64) + `{"type":"string"}` + strings.Repeat("}", 64)
	srv.SetEcho("", deep)
	dials := srv.Initializes()

	rec := doWith(t, sqlDB, manager, http.MethodPost, "/mcp/servers/"+id+"/refresh", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(body(rec), mcp.CodeCatalogInvalidSchema) {
		t.Fatalf("refresh status = %d body %s, want 502 carrying %s",
			rec.Code, body(rec), mcp.CodeCatalogInvalidSchema)
	}
	if got := srv.Initializes() - dials; got != 0 {
		t.Fatalf("rejected generation reconnected %d time(s), want none", got)
	}
	if kept, ok := manager.Get(id); !ok || kept != cached {
		t.Fatal("rejected generation replaced the healthy client")
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || row.CatalogState != mcpserver.CatalogPaused {
		t.Fatalf("catalog state = %q err %v, want paused", row.CatalogState, err)
	}
	if _, err := queries.GetTool(t.Context(), id+":echo"); err != nil {
		t.Fatalf("paused catalog lost its previous rows: %v", err)
	}
}

// An edit whose verification rejects the listing writes nothing: the
// persisted configuration, its tools and the catalog state all stand.
func TestEditVerifyRejectionWritesNothing(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	created := createServerWith(t, sqlDB, manager, "Reject Edit", srv)
	id := created.Server.ID
	before, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read server: %v", err)
	}
	storedKeys := before.HeaderKeys

	// The endpoint now answers with a listing the catalog refuses.
	deep := strings.Repeat(`{"type":"array","items":`, 64) + `{"type":"string"}` + strings.Repeat("}", 64)
	srv.SetEcho("", deep)

	rec := doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id, `{"headers":{"X-Trace":"abc"}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(body(rec), mcp.CodeCatalogInvalidSchema) {
		t.Fatalf("edit status = %d body %s, want 400 carrying %s",
			rec.Code, body(rec), mcp.CodeCatalogInvalidSchema)
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || row.CatalogState != mcpserver.CatalogActive {
		t.Fatalf("catalog state = %q err %v, want active", row.CatalogState, err)
	}
	if row.HeaderKeys != storedKeys {
		t.Fatalf("rejected edit stored header keys %q, want the stored %q", row.HeaderKeys, storedKeys)
	}
	if _, err := queries.GetTool(t.Context(), id+":echo"); err != nil {
		t.Fatalf("rejected edit lost its tool rows: %v", err)
	}
	if !strings.Contains(echoDefinition(t, queries, id), "Echoes its input text.") {
		t.Fatal("rejected edit published the poisoned definition")
	}
}

// echoDefinition returns the stored definition of a server's echo tool.
func echoDefinition(t *testing.T, queries *db.Queries, id string) string {
	t.Helper()
	tool, err := queries.GetTool(t.Context(), id+":echo")
	if err != nil {
		t.Fatalf("read %s:echo: %v", id, err)
	}
	return tool.DefinitionJson
}

// A publish fault after a successful verification answers 500, pauses
// nothing, and leaves the row explaining itself.
func TestEditPublishFaultAnswers500(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	manager := mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	created := createServerWith(t, sqlDB, manager, "Fault Edit", srv)
	id := created.Server.ID

	// A changed description forces the tool upsert to UPDATE, which the
	// trigger refuses: a database fault, not a generation rejection.
	srv.SetEcho("Changed description.", "")
	mustExec(t, sqlDB, `CREATE TRIGGER fail_tool_update BEFORE UPDATE ON tools
		BEGIN SELECT RAISE(ABORT, 'catalog write down'); END;`)

	rec := doWith(t, sqlDB, manager, http.MethodPatch, "/mcp/servers/"+id, `{"headers":{"X-Trace":"abc"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("edit status = %d body %s, want 500", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || row.CatalogState != mcpserver.CatalogActive {
		t.Fatalf("catalog state = %q err %v, want active", row.CatalogState, err)
	}
	if !row.LastError.Valid || !strings.Contains(row.LastError.String, "catalog write down") {
		t.Fatalf("last error = %+v, want the publish fault recorded", row.LastError)
	}
}

// Enabling a server is an activation: the switch and its
// approval-change record commit together, so a failing record rolls the
// enable back. Disabling is a restriction: it commits even when its
// record cannot be written.
func TestServerEnableAuditIsAtomic(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	created := createServer(t, sqlDB, "Audit Helper", mcptest.Start(t), "")
	id := created.Server.ID

	// The restriction lands with its record.
	if rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body %s", rec.Code, body(rec))
	}
	mustExec(t, sqlDB, `CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`)

	if rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("unauditable disable status = %d body %s, want the restriction to stand", rec.Code, body(rec))
	}
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil || row.Enabled != 0 {
		t.Fatalf("unauditable disable left enabled = %d err %v", row.Enabled, err)
	}

	if rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"enabled":true}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("unauditable enable status = %d body %s, want 500", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), id)
	if err != nil || row.Enabled != 0 {
		t.Fatalf("unauditable enable left enabled = %d err %v, want 0", row.Enabled, err)
	}

	// A recorded enable lands both halves.
	mustExec(t, sqlDB, `DROP TRIGGER fail_audit`)
	if rec := do(t, sqlDB, http.MethodPatch, "/mcp/servers/"+id, `{"enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d body %s", rec.Code, body(rec))
	}
	row, err = queries.GetMcpServer(t.Context(), id)
	if err != nil || row.Enabled != 1 {
		t.Fatalf("enable left enabled = %d err %v, want 1", row.Enabled, err)
	}
	var decision, reason string
	if err := sqlDB.QueryRowContext(t.Context(), `
SELECT decision, reason_code FROM tool_audit_events
WHERE server_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&decision, &reason); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if decision != "approve" || reason != "server_enabled" {
		t.Fatalf("enable record = %s/%s, want approve/server_enabled", decision, reason)
	}
}
