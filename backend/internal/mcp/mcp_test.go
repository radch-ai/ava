package mcp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radch-ai/ava/backend/internal/mcptest"
	"github.com/radch-ai/ava/backend/internal/security"
)

// newInProcess starts a client over mcp-go's in-process transport — real
// MCP handshake and dispatch, no sockets. Transport-hardening behavior is
// covered over real HTTP (http_test.go); this fixture pins the client's
// own wrapping rules fast.
func newInProcess(t *testing.T, slug string, timeout time.Duration, extra ...mcpsdk.Tool) *Client {
	t.Helper()
	s := mcpserver.NewMCPServer("ava-test-helper", "1.0.0")
	s.AddTool(mcptest.EchoTool(), mcptest.EchoHandler)
	for _, tl := range extra {
		s.AddTool(tl, mcptest.EchoHandler)
	}
	c, err := newClient(context.Background(), ServerConfig{
		ID:      slug,
		URL:     "http://in-process.test/mcp",
		Timeout: timeout,
	}, transport.NewInProcessTransport(s), security.NewScrubber(nil))
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// syncOnce runs one discovery pass and returns the listing.
func syncOnce(t *testing.T, c *Client) ([]ListedTool, []Skipped) {
	t.Helper()
	listed, skipped, err := c.SyncStaged(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	c.Commit()
	return listed, skipped
}

func TestInProcessRoundTripCompositeName(t *testing.T) {
	c := newInProcess(t, "helper", 5*time.Second)

	syncOnce(t, c)
	inv, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after sync")
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
	// A single text block is delivered as text, not wrapped in a
	// CallToolResult envelope.
	if out != "hello mcp" {
		t.Fatalf("result = %q, want the bare text", out)
	}
}

// The native text path is what the admission chain bounds — one text
// block never arrives JSON-enveloped.
func TestTextResultNotJsonEnveloped(t *testing.T) {
	c := newInProcess(t, "helper", 5*time.Second)
	syncOnce(t, c)
	inv, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after sync")
	}
	out, err := inv.InvokableRun(context.Background(), `{"text":"plain answer"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.HasPrefix(out, `{"content":`) || strings.Contains(out, `"type":"text"`) {
		t.Fatalf("result is still a JSON envelope: %q", out)
	}
	if out != "plain answer" {
		t.Fatalf("result = %q, want the text verbatim", out)
	}
}

func TestSyncCachesForPerTurnLookup(t *testing.T) {
	c := newInProcess(t, "helper", 5*time.Second)

	listed, skipped := syncOnce(t, c)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if len(listed) != 1 || listed[0].Remote != "echo" {
		t.Fatalf("listing = %+v, want one echo tool", listed)
	}
	if listed[0].Description == "" {
		t.Fatal("listing carries no description")
	}
	if !strings.Contains(listed[0].DefinitionJSON, `"inputSchema"`) {
		t.Fatalf("definition = %s, want the advertised schema", listed[0].DefinitionJSON)
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
	if out != "cached" {
		t.Fatalf("result = %q, want the bare text", out)
	}

	if _, ok := c.InvokableByRemote("missing"); ok {
		t.Fatal("uncached remote resolved")
	}
}

// A name the provider wire contract cannot carry is skipped with a safe
// reason: never renamed, never truncated, never cached.
func TestSyncSkipsUnrepresentableNames(t *testing.T) {
	long := strings.Repeat("x", 60) // 6+1+60 > 64 with slug "helper"
	c := newInProcess(t, "helper", 5*time.Second,
		mcpsdk.NewTool("echo", mcpsdk.WithDescription("ok")),
		mcpsdk.NewTool(long, mcpsdk.WithDescription("long")))

	listed, skipped := syncOnce(t, c)
	if len(listed) != 1 || listed[0].Remote != "echo" {
		t.Fatalf("listing = %+v, want only echo", listed)
	}
	if len(skipped) != 1 || skipped[0].Remote != long {
		t.Fatalf("skipped = %v, want [%s]", skipped, long)
	}
	if !strings.Contains(skipped[0].Reason, "exceeds") {
		t.Fatalf("skip reason %q does not explain the length cap", skipped[0].Reason)
	}
	if _, ok := c.InvokableByRemote(long); ok {
		t.Fatal("skipped tool cached as invokable")
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
		{"http ok", ServerConfig{URL: "http://x"}, true},
		{"no url", ServerConfig{}, false},
		{"no host", ServerConfig{URL: "http://"}, false},
		{"userinfo", ServerConfig{URL: "http://u:p@x/mcp"}, false},
		{"non-http scheme", ServerConfig{URL: "ftp://x/mcp"}, false},
	}
	for _, tc := range cases {
		err := tc.cfg.validate()
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestSameConfig(t *testing.T) {
	base := ServerConfig{
		ID: "s", URL: "http://x/mcp",
		Headers: map[string]string{"A": "1", "B": "2"},
	}

	same := base
	same.Headers = map[string]string{"B": "2", "A": "1"} // map order is irrelevant
	same.Timeout = DefaultTimeout                        // effective timeout is the same
	if !sameConfig(base, same) {
		t.Fatal("equivalent configs compare unequal")
	}

	changed := base
	changed.URL = "http://y/mcp"
	if sameConfig(base, changed) {
		t.Fatal("different url compares equal")
	}

	changed = base
	changed.Headers = map[string]string{"A": "1"}
	if sameConfig(base, changed) {
		t.Fatal("different headers compare equal")
	}

	// Policy approvals are part of a connection's identity: widening one
	// must redial, never silently reuse a client that skipped the check.
	changed = base
	changed.AllowPrivateNetwork = true
	if sameConfig(base, changed) {
		t.Fatal("changed destination approval compares equal")
	}
	changed = base
	changed.AllowPlaintext = true
	if sameConfig(base, changed) {
		t.Fatal("changed plaintext approval compares equal")
	}
}

// A replaced connection is retired, not aborted: a call already on the
// old wire finishes there, and only then is the old client torn down.
func TestAdoptRetiresInsteadOfAborting(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)
	cfg := ServerConfig{ID: "helper", URL: "http://in-process.test/mcp", Timeout: 5 * time.Second}
	first := newInProcess(t, "helper", 5*time.Second)
	m.Adopt(cfg, first)
	syncOnce(t, first)
	inv, ok := first.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after sync")
	}

	// The call takes its slot on the live client, then the refresh
	// replaces that client.
	held := make(chan string, 1)
	go func() {
		out, err := inv.InvokableRun(context.Background(), `{"text":"held"}`)
		if err != nil {
			held <- "error: " + err.Error()
			return
		}
		held <- out
	}()
	// The borrow happens inside InvokableRun; wait for it before swapping.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		first.callsMu.Lock()
		n := first.calls
		first.callsMu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	// A changed config is what forces the swap: an equivalent one wins in
	// Adopt and leaves the caller's client closed instead.
	replacement := newInProcess(t, "helper", 5*time.Second)
	m.Adopt(ServerConfig{ID: "helper", URL: "http://in-process.test/mcp/v2", Timeout: 5 * time.Second}, replacement)

	if got := <-held; got != "held" {
		t.Fatalf("call through the retired client = %q, want it to complete", got)
	}
	// Retirement closes the connection once the drain returns.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		first.callsMu.Lock()
		closed := first.closed
		first.callsMu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("retired client was never closed after its call drained")
}

// KnownSecrets covers the header values and their scheme-prefixed token
// forms — what a reflected credential actually looks like.
func TestKnownSecrets(t *testing.T) {
	cfg := ServerConfig{Headers: map[string]string{
		"Authorization": "Bearer tok-123",
		"X-Api-Key":     "key-456",
	}}
	got := cfg.KnownSecrets()
	for _, want := range []string{"Bearer tok-123", "tok-123", "key-456"} {
		if !slices.Contains(got, want) {
			t.Errorf("KnownSecrets() = %v, missing %q", got, want)
		}
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

// Lock scope: tearing one client down never blocks another server's
// lookup, and a concurrent Get/Close is race-free.
func TestConcurrentGetDuringClose(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)
	first := newInProcess(t, "first", 5*time.Second)
	second := newInProcess(t, "second", 5*time.Second)
	m.Adopt(ServerConfig{ID: "first", URL: "http://in-process.test/mcp"}, first)
	m.Adopt(ServerConfig{ID: "second", URL: "http://in-process.test/mcp"}, second)

	var (
		wg     sync.WaitGroup
		closed atomic.Bool
		gets   atomic.Int64
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.Close("first")
		closed.Store(true)
	}()
	go func() {
		defer wg.Done()
		for !closed.Load() || gets.Load() < 100 {
			if _, ok := m.Get("second"); ok {
				gets.Add(1)
				continue
			}
			// The unrelated server is never dropped by the other's close.
			t.Errorf("unrelated client vanished")
			return
		}
	}()
	wg.Wait()
	if _, ok := m.Get("first"); ok {
		t.Fatal("closed client still cached")
	}
}

// refuseBorrows seals the drain race: after it returns, borrow refuses
// even with zero in-flight calls, so waitDrained then sees every
// dispatch that can ever exist.
func TestRefuseBorrowsBlocksNewDispatches(t *testing.T) {
	c := newInProcess(t, "helper", 5*time.Second)
	if err := c.borrow(); err != nil {
		t.Fatalf("borrow on live client: %v", err)
	}
	c.release()
	c.refuseBorrows()
	if err := c.borrow(); !errors.Is(err, errClientClosed) {
		t.Fatalf("borrow after refuseBorrows = %v, want errClientClosed", err)
	}
}

// A failed listing pass drops the previous pass's staging: StagedListing
// never reports a generation this pass did not validate.
func TestFailedSyncClearsStaleStaging(t *testing.T) {
	srv := mcptest.Start(t)
	c, err := Dial(context.Background(), ServerConfig{
		ID: "helper", URL: srv.URL, Timeout: 5 * time.Second,
		AllowPrivateNetwork: true, AllowPlaintext: true,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, _, err := c.SyncStaged(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if _, _, ok := c.StagedListing(); !ok {
		t.Fatal("staged listing missing after sync")
	}
	// A restarted server forgets the session: the next listing pass
	// fails on the dead session.
	srv.ExpireSession()
	if _, _, err := c.SyncStaged(context.Background()); err == nil {
		t.Fatal("sync on a dead session succeeded")
	}
	if _, _, ok := c.StagedListing(); ok {
		t.Fatal("stale staging survived a failed sync")
	}
}
