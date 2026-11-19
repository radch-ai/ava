package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

// TestHTTPRoundTrip drives the streamable-HTTP transport against an
// in-process server and proves configured headers reach the wire.
func TestHTTPRoundTrip(t *testing.T) {
	s := mcpserver.NewMCPServer("ava-test-helper", "1.0.0")
	s.AddTool(echoTool(), echoHandler)
	hs := mcpserver.NewStreamableHTTPServer(s)

	var (
		mu       sync.Mutex
		probeHit bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ava-Probe") == "on" {
			mu.Lock()
			probeHit = true
			mu.Unlock()
		}
		hs.ServeHTTP(w, r)
	}))
	// Cleanups run LIFO: the manager closes (and sends the MCP session
	// delete) before the test server goes away.
	t.Cleanup(srv.Close)

	m := NewManager()
	t.Cleanup(m.CloseAll)

	c, err := m.Connect(context.Background(), ServerConfig{
		ID:        "http-helper",
		Transport: TransportHTTP,
		URL:       srv.URL,
		Headers:   map[string]string{"X-Ava-Probe": "on"},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	inv, err := c.Invokable(findTool(t, tools, "echo"))
	if err != nil {
		t.Fatalf("invokable: %v", err)
	}
	out, err := inv.InvokableRun(context.Background(), `{"text":"hello http"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `"text":"hello http"`) {
		t.Fatalf("result = %q", out)
	}

	mu.Lock()
	hit := probeHit
	mu.Unlock()
	if !hit {
		t.Fatal("configured header never reached the server")
	}
}

// TestBoundedDialCapsConfiguredTimeout pins the install-time budget: a
// user-configured timeout larger than the verify cap must not stretch a
// connection check past the cap (the HTTP write deadline cannot wait).
func TestBoundedDialCapsConfiguredTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-time.After(20 * time.Second):
		case <-r.Context().Done():
		}
	}))
	// Cleanups run LIFO: releasing the blocked handler first lets the
	// server close immediately when the test is over.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := BoundedDial(context.Background(), ServerConfig{
			ID:        "slow",
			Transport: TransportHTTP,
			URL:       srv.URL,
			Timeout:   60 * time.Second,
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dial of a never-answering server succeeded")
		}
		if elapsed := time.Since(start); elapsed > VerifyTimeout+5*time.Second {
			t.Fatalf("bounded dial took %v, want <= %v", elapsed, VerifyTimeout)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("bounded dial ignored the verify cap")
	}
}
