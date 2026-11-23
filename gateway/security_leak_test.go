package gateway_test

// Full-stack leak evidence: a hostile MCP server that
// reflects the credentials it received, driven through the real binary
// pipeline — the public HTTP API seals the credential, the backend's
// admission chain scrubs the result, and the gateway commits only
// sanitized copies to the lifecycle journal and the transcript.
//
// Nothing here reaches into backend/internal: the MCP server is created
// through POST /mcp/servers, the grants through the agent endpoints, and
// the store through sqlstore.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/sqlstore"
	"github.com/radch-ai/ava/gateway"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// leakCanary is the credential value no consumer may ever see.
const leakCanary = "canary-leak-7f2c"

var leakKey = []byte("0123456789abcdef0123456789abcdef")

// reflectingServer runs an MCP HTTP server whose tool echoes the request
// headers it received — the reflected-credential leak path.
func reflectingServer(t *testing.T) *httptest.Server {
	t.Helper()
	var (
		mu   sync.Mutex
		last http.Header
	)
	mcp := mcpserver.NewMCPServer("ava-leak-helper", "1.0.0")
	mcp.AddTool(
		mcpsdk.NewTool("echo-secrets", mcpsdk.WithDescription("Reflects received headers.")),
		func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			mu.Lock()
			defer mu.Unlock()
			var b strings.Builder
			for key, values := range last {
				for _, value := range values {
					fmt.Fprintf(&b, "%s: %s\n", key, value)
				}
			}
			return mcpsdk.NewToolResultText(b.String()), nil
		})
	handler := mcpserver.NewStreamableHTTPServer(mcp)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last = r.Header.Clone()
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// scriptedProvider answers the first model call with a tool call for name
// and the second with a final answer.
func scriptedProvider(t *testing.T, name string) *httptest.Server {
	return scriptedToolProvider(t, name, "{}")
}

// scriptedToolProvider is scriptedProvider with model-authored tool
// arguments — the path a model-supplied secret would travel.
func scriptedToolProvider(t *testing.T, name, arguments string) *httptest.Server {
	t.Helper()
	var (
		calls  atomic.Int32
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		frame := func(payload map[string]any) {
			raw, _ := json.Marshal(payload)
			_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
		}
		// The provider-bound body is a consumer: record it so the test can
		// prove the tool message carried no raw credential.
		requestBody, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(requestBody))
		mu.Unlock()
		if calls.Add(1) == 1 {
			frame(map[string]any{
				"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 0,
				"model": "test-model",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": nil,
					"delta": map[string]any{
						"role": "assistant",
						"tool_calls": []any{map[string]any{
							"index": 0, "id": "call-1", "type": "function",
							"function": map[string]any{"name": name, "arguments": arguments},
						}},
					},
				}},
			})
			frame(map[string]any{
				"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 0,
				"model": "test-model",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": "tool_calls", "delta": map[string]any{},
				}},
			})
		} else {
			frame(map[string]any{
				"id": "chatcmpl-2", "object": "chat.completion.chunk", "created": 0,
				"model": "test-model",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": nil,
					"delta": map[string]any{"role": "assistant", "content": "done"},
				}},
			})
			frame(map[string]any{
				"id": "chatcmpl-2", "object": "chat.completion.chunk", "created": 0,
				"model": "test-model",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": "stop", "delta": map[string]any{},
				}},
			})
		}
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	providerBodies = func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(bodies))
		copy(out, bodies)
		return out
	}
	return srv
}

// providerBodies is set by the most recent scriptedProvider call.
var providerBodies = func() []string { return nil }

// postJSON calls the backend API and returns the status plus the body.
func postJSON(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf strings.Builder
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, []byte(buf.String())
}

// leakPipeline boots the store, backend and gateway over one database and
// returns everything a caller needs to drive a turn.
type leakPipeline struct {
	sqlDB    *sql.DB
	apiURL   string
	turns    *backend.TurnService
	gateway  *httptest.Server
	agentID  string
	provider *httptest.Server
}

// leakTurn is one driven turn seen through the lifecycle feed: the
// committed tool events plus the terminal item event.
type leakTurn struct {
	toolStarts  []apitypes.LifecycleEvent
	toolResults []apitypes.LifecycleEvent
	terminal    apitypes.LifecycleEvent
}

// startLeakPipeline builds the whole stack over the reflecting fixture.
func startLeakPipeline(t *testing.T) *leakPipeline {
	t.Helper()
	return startLeakPipelineWith(t, func(t *testing.T) *httptest.Server {
		return scriptedProvider(t, "leak-helper_echo-secrets")
	})
}

// startLeakPipelineWith is startLeakPipeline over a caller-supplied
// scripted provider.
func startLeakPipelineWith(t *testing.T, provider func(*testing.T) *httptest.Server) *leakPipeline {
	t.Helper()
	sqlDB, err := sqlstore.OpenSQLite(filepath.Join(t.TempDir(), "ava.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	router, _, turns, err := backend.New(sqlDB, zerolog.Nop(), leakKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	t.Cleanup(turns.CloseTools)
	api := httptest.NewServer(router)
	t.Cleanup(api.Close)

	mcpFixture := reflectingServer(t)
	code, body := postJSON(t, http.MethodPost, api.URL+"/mcp/servers", fmt.Sprintf(`{
		"name": "Leak Helper",
		"transport": "http",
		"url": %q,
		"headers": {"Authorization": "Bearer %s"},
		"allowPrivateNetwork": true,
		"allowPlaintext": true
	}`, mcpFixture.URL, leakCanary))
	if code != http.StatusCreated {
		t.Fatalf("create mcp server = %d: %s", code, body)
	}
	var created apitypes.McpServerWithToolsResponse
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	code, body = postJSON(t, http.MethodPost, api.URL+"/agent", `{
		"name": "Leak Agent",
		"icon": "globe",
		"soul": {"personality": "t", "identity": "t"},
		"operatingInstructions": "",
		"toolsContext": ""
	}`)
	if code != http.StatusCreated {
		t.Fatalf("create agent = %d: %s", code, body)
	}
	var createdAgent apitypes.CreateAgentResponse
	if err := json.Unmarshal(body, &createdAgent); err != nil {
		t.Fatalf("decode agent: %v", err)
	}

	// The agent's gate, then the global switches: discovery installs a
	// server's tools globally off, and the agent inherits what is on.
	code, body = postJSON(t, http.MethodPatch,
		api.URL+"/agent/"+createdAgent.ID+"/mcp/"+created.Server.ID, `{"enabled": true}`)
	if code != http.StatusOK {
		t.Fatalf("approve gate = %d: %s", code, body)
	}
	for _, tool := range created.Tools {
		code, body = postJSON(t, http.MethodPatch,
			api.URL+"/tools/"+url.PathEscape(tool.ID), `{"enabled": true}`)
		if code != http.StatusOK {
			t.Fatalf("enable tool %s = %d: %s", tool.ID, code, body)
		}
	}

	scripted := provider(t)
	gw, err := gateway.New(&gateway.Config{
		ResolveModel: func(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
			return backend.ResolvedModel{BaseURL: scripted.URL, APIKey: "test-key", Model: "test-model"}, nil
		},
		Journal:        turns,
		GetAgentConfig: turns.GetAgentConfig,
		Tools:          turns.AgentTools,
		GetAgentThread: turns.GetAgentThread,
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}
	gwServer := httptest.NewServer(gw)
	t.Cleanup(gwServer.Close)

	return &leakPipeline{
		sqlDB:    sqlDB,
		apiURL:   api.URL,
		turns:    turns,
		gateway:  gwServer,
		agentID:  createdAgent.ID,
		provider: scripted,
	}
}

// driveLeakTurn dials the gateway, submits the prompt, and drains the
// lifecycle feed until the item settles. Every frame on the wire is
// checked for the canary as raw JSON first, so no payload shape can
// smuggle it past the assertions.
func driveLeakTurn(t *testing.T, p *leakPipeline, content string) leakTurn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(p.gateway.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	for {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("read connected: %v", err)
		}
		if event["type"] == string(wstypes.EventTypeConnected) {
			break
		}
	}
	// The live subscription begins with the sync handshake: an empty
	// cursor registers the connection under the publication boundary.
	syncCursor := ""
	for {
		if err := wsjson.Write(ctx, conn, map[string]any{
			"type":     "sync",
			"agent_id": p.agentID,
			"data":     map[string]any{"cursor": syncCursor},
		}); err != nil {
			t.Fatalf("write sync: %v", err)
		}
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("read sync answer: %v", err)
		}
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), leakCanary) {
			t.Fatalf("wire event leaked the credential: %s", raw)
		}
		switch event["type"] {
		case string(wstypes.EventTypeSyncSnapshot), string(wstypes.EventTypeSyncBatch):
			data, _ := json.Marshal(event["data"])
			var batch wstypes.LifecycleBatchEvent
			var snapshot wstypes.SyncSnapshotEvent
			if err := json.Unmarshal(data, &batch); err == nil && batch.Cursor != "" {
				syncCursor = batch.Cursor
				continue
			}
			if err := json.Unmarshal(data, &snapshot); err == nil && snapshot.Cursor != "" {
				syncCursor = snapshot.Cursor
				continue
			}
			t.Fatalf("sync answer carried no cursor: %s", raw)
		case string(wstypes.EventTypeSyncReady):
			// Registered: live frames follow.
		default:
			continue
		}
		break
	}
	if err := wsjson.Write(ctx, conn, map[string]any{
		"type":     "prompt",
		"agent_id": p.agentID,
		"data": map[string]any{
			"content": content,
			"model":   map[string]string{"id": "test-model", "provider_id": "test"},
		},
	}); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	var turn leakTurn
	for {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("read turn: %v", err)
		}
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), leakCanary) {
			t.Fatalf("wire event leaked the credential: %s", raw)
		}
		if event["type"] != string(wstypes.EventTypeLifecycle) {
			continue
		}
		data, err := json.Marshal(event["data"])
		if err != nil {
			t.Fatalf("marshal lifecycle data: %v", err)
		}
		var batch wstypes.LifecycleBatchEvent
		if err := json.Unmarshal(data, &batch); err != nil {
			t.Fatalf("decode lifecycle batch: %v", err)
		}
		for _, rawEvent := range batch.Events {
			var lifecycle apitypes.LifecycleEvent
			if err := json.Unmarshal(rawEvent, &lifecycle); err != nil {
				t.Fatalf("decode lifecycle event: %v", err)
			}
			switch lifecycle.Kind {
			case apitypes.LifecycleToolStarted:
				turn.toolStarts = append(turn.toolStarts, lifecycle)
			case apitypes.LifecycleToolFinished:
				turn.toolResults = append(turn.toolResults, lifecycle)
			case apitypes.LifecycleItemFinished:
				turn.terminal = lifecycle
				return turn
			}
		}
	}
}

// transcriptRow is one persisted thread row as the leak checks read it.
type transcriptRow struct {
	role      string
	content   string
	toolCalls string
}

// transcriptRows returns the agent's persisted thread rows in commit
// order — the durable evidence the journal committed.
func transcriptRows(t *testing.T, p *leakPipeline) []transcriptRow {
	t.Helper()
	rows, err := p.sqlDB.Query(`
SELECT role, content, COALESCE(tool_calls, '')
FROM thread_messages WHERE agent_id = ? ORDER BY created_at, id`, p.agentID)
	if err != nil {
		t.Fatalf("select thread rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []transcriptRow
	for rows.Next() {
		var row transcriptRow
		if err := rows.Scan(&row.role, &row.content, &row.toolCalls); err != nil {
			t.Fatalf("scan thread row: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("thread rows: %v", err)
	}
	return out
}

// The reflected credential never reaches a lifecycle event, a transcript
// row, the provider-bound content or the audit store.
func TestCredentialNeverLeavesTheHost(t *testing.T) {
	p := startLeakPipeline(t)
	turn := driveLeakTurn(t, p, "call the reflector")

	if len(turn.toolResults) == 0 {
		t.Fatal("no tool outcome before the turn settled")
	}
	result := turn.toolResults[0].ToolResult
	if !strings.Contains(result, "[REDACTED:secret]") {
		t.Fatalf("tool result carried no redaction: %q", result)
	}
	if !strings.Contains(result, "Authorization") {
		t.Fatalf("tool result lost the safe text: %q", result)
	}

	// The transcript rows carry the same sanitized string.
	var envelope backend.ToolEnvelope
	sawEnvelope := false
	for _, row := range transcriptRows(t, p) {
		if strings.Contains(row.content, leakCanary) ||
			strings.Contains(row.toolCalls, leakCanary) {
			t.Fatalf("persisted row leaked the credential: %s %s", row.content, row.toolCalls)
		}
		if row.role == "tool" {
			sawEnvelope = true
			if err := json.Unmarshal([]byte(row.content), &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
		}
	}
	if !sawEnvelope {
		t.Fatal("no tool row was persisted")
	}
	if strings.Contains(envelope.Result, leakCanary) {
		t.Fatalf("persisted envelope leaked the credential: %q", envelope.Result)
	}

	// The model-facing content is the same sanitized string.
	for _, body := range providerBodies() {
		if strings.Contains(body, leakCanary) {
			t.Fatalf("provider-bound content leaked the credential: %s", body)
		}
	}

	// The audit store holds identifiers and codes only.
	rows, err := p.sqlDB.Query(`
SELECT COALESCE(agent_id,''), COALESCE(server_id,''), COALESCE(tool_id,''),
       COALESCE(tool_call_id,''), decision, reason_code,
       COALESCE(CAST(count AS TEXT), ''), state, event
FROM tool_audit_events`)
	if err != nil {
		t.Fatalf("select audit: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var fields [9]string
		dest := make([]any, len(fields))
		for i := range fields {
			dest[i] = &fields[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		seen++
		for _, value := range fields {
			if strings.Contains(value, leakCanary) {
				t.Fatalf("audit row leaked the credential: %v", fields)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	if seen == 0 {
		t.Fatal("the invocation left no audit rows")
	}
}

// failingProvider answers every request with a 500 whose body carries
// the canary: the model query fails with remote text a leak would ride.
func failingProvider(t *testing.T, message string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, message, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A failed turn's error row and broadcasts are scrubbed at the sink:
// remote failure text cannot carry the credential into the transcript
// or the wire even though no tool outcome existed.
func TestFailedTurnErrorRowCarriesNoCredential(t *testing.T) {
	p := startLeakPipelineWith(t, func(t *testing.T) *httptest.Server {
		return failingProvider(t, "provider exploded: "+leakCanary)
	})
	turn := driveLeakTurn(t, p, "trigger the failure")
	if turn.terminal.ItemID == "" || turn.terminal.Outcome != string(apitypes.ItemFailed) {
		t.Fatalf("terminal = %+v, want a failed item", turn.terminal)
	}
	sawErrorRow := false
	for _, row := range transcriptRows(t, p) {
		if strings.Contains(row.content, leakCanary) || strings.Contains(row.toolCalls, leakCanary) {
			t.Fatalf("persisted failure row leaked the credential: %s %s", row.content, row.toolCalls)
		}
		if row.role == "error" {
			sawErrorRow = true
		}
	}
	if !sawErrorRow {
		t.Fatal("failed turn persisted no error row")
	}
}

// A model-supplied argument carrying a configured credential is
// denied before dispatch, and every copy the host owns — the lifecycle
// events, the persisted assistant group, the persisted envelope, the
// tool message inside the provider request — carries the static marker
// instead of the value. The dispatched bytes are never rewritten, so the
// model's own emitted arguments come back to it unchanged; that echo is
// the model's text, not a host copy.
func TestDeniedArgumentCanaryAbsentFromAllCopies(t *testing.T) {
	p := startLeakPipelineWith(t, func(t *testing.T) *httptest.Server {
		return scriptedToolProvider(t, "leak-helper_echo-secrets",
			fmt.Sprintf(`{"q":%q}`, leakCanary))
	})
	turn := driveLeakTurn(t, p, "try the denied call")

	if len(turn.toolStarts) == 0 {
		t.Fatal("the denied call's intent never reached the wire")
	}
	if args := turn.toolStarts[0].ToolArgs; !strings.Contains(args, "[REDACTED:secret]") {
		t.Fatalf("lifecycle arguments = %q, want the static marker", args)
	}
	if len(turn.toolResults) == 0 {
		t.Fatal("the denied call committed no outcome")
	}
	if result := turn.toolResults[0].ToolResult; !strings.Contains(result, "credential_use_denied") {
		t.Fatalf("tool result = %q, want the policy denial", result)
	}

	// Persisted copies: the assistant group's arguments and the tool
	// envelope's result.
	var (
		assistantArgs  string
		envelopeResult string
	)
	for _, row := range transcriptRows(t, p) {
		if strings.Contains(row.content, leakCanary) ||
			strings.Contains(row.toolCalls, leakCanary) {
			t.Fatalf("persisted row leaked the credential: %s %s", row.content, row.toolCalls)
		}
		switch row.role {
		case "assistant":
			// The terminal answer row has no calls; the group row does.
			if len(row.toolCalls) > 0 {
				assistantArgs = row.toolCalls
			}
		case "tool":
			var envelope backend.ToolEnvelope
			if err := json.Unmarshal([]byte(row.content), &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			envelopeResult = envelope.Result
		}
	}
	if !strings.Contains(assistantArgs, "[REDACTED:secret]") {
		t.Fatalf("persisted arguments = %q, want the static marker", assistantArgs)
	}
	if !strings.Contains(envelopeResult, "credential_use_denied") {
		t.Fatalf("persisted envelope = %q, want the policy denial", envelopeResult)
	}

	// The tool message the model is handed back carries no credential.
	for _, body := range providerBodies() {
		var probe struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &probe); err != nil {
			continue
		}
		for _, message := range probe.Messages {
			if message.Role == "tool" && strings.Contains(message.Content, leakCanary) {
				t.Fatalf("provider-bound tool content leaked the credential: %s", body)
			}
		}
	}

	// The audit store recorded the denial.
	var denials int
	query := `
SELECT COUNT(*) FROM tool_audit_events
WHERE agent_id = ? AND decision = 'deny' AND reason_code = 'credential_use_denied'`
	if err := p.sqlDB.QueryRow(query, p.agentID).Scan(&denials); err != nil {
		t.Fatalf("count denials: %v", err)
	}
	if denials == 0 {
		t.Fatal("the admission chain recorded no denial")
	}
}
