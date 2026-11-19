package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Shared tool definitions for the in-process and HTTP tests; the stdio
// helper under backend/testdata registers the same names.

func echoTool() mcpsdk.Tool {
	return mcpsdk.NewTool("echo",
		mcpsdk.WithDescription("Echoes its input text."),
		mcpsdk.WithString("text", mcpsdk.Required()))
}

func echoHandler(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	text, _ := req.GetArguments()["text"].(string)
	return mcpsdk.NewToolResultText(text), nil
}

// newInProcess starts a client over mcp-go's in-process transport — real
// MCP handshake and dispatch, no subprocess. Per-call deadlines are covered
// over a real transport (stdio_test.go): the in-process transport dispatches
// synchronously, so the client's ctx wait never interleaves there.
func newInProcess(t *testing.T, slug string, timeout time.Duration, extra ...mcpsdk.Tool) *Client {
	t.Helper()
	s := mcpserver.NewMCPServer("ava-test-helper", "1.0.0")
	s.AddTool(echoTool(), echoHandler)
	for _, tl := range extra {
		s.AddTool(tl, echoHandler)
	}
	c, err := newClient(context.Background(), ServerConfig{
		ID:        slug,
		Transport: TransportStdio,
		Timeout:   timeout,
	}, transport.NewInProcessTransport(s), nil)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func findTool(t *testing.T, tools []tool.BaseTool, remote string) tool.BaseTool {
	t.Helper()
	for _, b := range tools {
		info, err := b.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if info.Name == remote {
			return b
		}
	}
	t.Fatalf("tool %q not listed", remote)
	return nil
}

func TestInProcessRoundTripCompositeName(t *testing.T) {
	c := newInProcess(t, "helper", 5*time.Second)

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	inv, err := c.Invokable(findTool(t, tools, "echo"))
	if err != nil {
		t.Fatalf("invokable: %v", err)
	}

	info, err := inv.Info(context.Background())
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.Name != "helper_echo" {
		t.Fatalf("model name = %q, want helper_echo", info.Name)
	}

	out, err := inv.InvokableRun(context.Background(), `{"text":"hello mcp"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// eino-ext returns the marshaled CallToolResult, not the bare text.
	if !strings.Contains(out, `"text":"hello mcp"`) {
		t.Fatalf("result = %q", out)
	}
}

func TestSyncCachesForPerTurnLookup(t *testing.T) {
	c := newInProcess(t, "helper", 5*time.Second)

	listed, rejected, err := c.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("rejected = %v, want none", rejected)
	}
	if len(listed) != 1 || listed[0].Remote != "echo" {
		t.Fatalf("listing = %+v, want one echo tool", listed)
	}
	if listed[0].Description == "" {
		t.Fatal("listing carries no description")
	}

	inv, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after sync")
	}
	info, err := inv.Info(context.Background())
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.Name != "helper_echo" {
		t.Fatalf("model name = %q", info.Name)
	}
	out, err := inv.InvokableRun(context.Background(), `{"text":"cached"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `"text":"cached"`) {
		t.Fatalf("result = %q", out)
	}

	if _, ok := c.InvokableByRemote("missing"); ok {
		t.Fatal("uncached remote resolved")
	}
}

func TestInvokableRejectsUnrepresentableName(t *testing.T) {
	long := strings.Repeat("x", 60) // 6+1+60 > 64 with slug "helper"
	c := newInProcess(t, "helper", 5*time.Second, mcpsdk.NewTool(long, mcpsdk.WithDescription("long")))

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	_, err = c.Invokable(findTool(t, tools, long))
	if err == nil {
		t.Fatal("over-64-char composite name was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error %q does not explain the length cap", err)
	}
}

func TestSyncReportsUnrepresentableNames(t *testing.T) {
	long := strings.Repeat("x", 60)
	c := newInProcess(t, "helper", 5*time.Second,
		mcpsdk.NewTool("echo", mcpsdk.WithDescription("ok")),
		mcpsdk.NewTool(long, mcpsdk.WithDescription("long")))

	listed, rejected, err := c.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(listed) != 1 || listed[0].Remote != "echo" {
		t.Fatalf("listing = %+v, want only echo", listed)
	}
	if len(rejected) != 1 || rejected[0] != long {
		t.Fatalf("rejected = %v, want [%s]", rejected, long)
	}
	if _, ok := c.InvokableByRemote(long); ok {
		t.Fatal("rejected tool cached as invokable")
	}
}

func TestNameHelpers(t *testing.T) {
	if got := CatalogID("gh", "search_issues"); got != "gh:search_issues" {
		t.Fatalf("CatalogID = %q", got)
	}
	if got := ModelName("gh", "search_issues"); got != "gh_search_issues" {
		t.Fatalf("ModelName = %q", got)
	}
}

func TestServerConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		cfg  ServerConfig
		ok   bool
	}{
		{"stdio ok", ServerConfig{Transport: TransportStdio, Command: "x"}, true},
		{"stdio no command", ServerConfig{Transport: TransportStdio}, false},
		{"http ok", ServerConfig{Transport: TransportHTTP, URL: "http://x"}, true},
		{"http no url", ServerConfig{Transport: TransportHTTP}, false},
		{"sse rejected", ServerConfig{Transport: "sse"}, false},
	}
	for _, tc := range cases {
		err := tc.cfg.validate()
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestSameConfig(t *testing.T) {
	base := ServerConfig{ID: "s", Transport: TransportStdio, Command: "c", Args: []string{"-a"}, Env: map[string]string{"A": "1", "B": "2"}, Timeout: 0}

	same := base
	same.Env = map[string]string{"B": "2", "A": "1"} // map order is irrelevant
	same.Timeout = DefaultTimeout                    // effective timeout is the same
	if !sameConfig(base, same) {
		t.Fatal("equivalent configs compare unequal")
	}

	changed := base
	changed.Args = []string{"-a", "-b"}
	if sameConfig(base, changed) {
		t.Fatal("different args compare equal")
	}

	changed = base
	changed.Env = map[string]string{"A": "1"}
	if sameConfig(base, changed) {
		t.Fatal("different env compares equal")
	}
}

func TestManagerUnknownIds(t *testing.T) {
	m := NewManager()
	if _, ok := m.Get("missing"); ok {
		t.Fatal("Get on empty manager found a client")
	}
	m.Close("missing") // must not panic
	m.CloseAll()
	m.CloseAll() // idempotent
}
