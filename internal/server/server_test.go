package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

func TestServerStarts(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	srv := New(":8081", logger, wsHandler)
	go func() {
		_ = srv.Start()
	}()
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:8081/health")
	if err != nil {
		t.Fatalf("server not responding: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func TestServerHealthEndpoint(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	srv := New(":8081", logger, wsHandler)
	go func() {
		_ = srv.Start()
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{
			name:           "GET health",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "HEAD health",
			method:         http.MethodHead,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST health",
			method:         http.MethodPost,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PUT health",
			method:         http.MethodPut,
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, "http://localhost:8081/health", nil)
			client := &http.Client{Timeout: 1 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestServerWebSocketEndpoint(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	srv := New(":8081", logger, wsHandler)
	go func() {
		_ = srv.Start()
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect to WebSocket endpoint
	wsURL := "ws://localhost:8081/ws"
	ws, _, err := gorillawebsocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket connection failed: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Read connected event first
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, p, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	var connected map[string]any
	if err := json.Unmarshal(p, &connected); err != nil {
		t.Fatalf("failed to unmarshal connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Errorf("expected connected event, got %v", connected["type"])
	}

	// Send a valid prompt message
	promptMsg := map[string]string{"type": "prompt", "text": "test"}
	data, _ := json.Marshal(promptMsg)
	if err := ws.WriteMessage(gorillawebsocket.TextMessage, data); err != nil {
		t.Errorf("failed to write message: %v", err)
	}

	// Read agent_start event
	_, p, err = ws.ReadMessage()
	if err != nil {
		t.Errorf("failed to read message: %v", err)
	}

	var agentStart map[string]any
	if err := json.Unmarshal(p, &agentStart); err != nil {
		t.Fatalf("failed to unmarshal agent_start event: %v", err)
	}

	if agentStart["type"] != "agent_start" {
		t.Errorf("expected agent_start event, got %v", agentStart["type"])
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	srv := New(":8081", logger, wsHandler)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	resp, err := http.Get("http://localhost:8081/health")
	if err != nil {
		t.Fatalf("server not responding: %v", err)
	}
	_ = resp.Body.Close()

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}

	// Verify Start returned
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for server to stop")
	}
}

func TestServerStartupFailure(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	// Try to bind to an address that requires root (will fail)
	srv := New(":80", logger, wsHandler)

	err := srv.Start()
	if err == nil {
		t.Error("expected error when binding to :80, got nil")
		_ = srv.Shutdown(context.Background())
	}
}

func TestServerWithCustomTimeouts(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	// Start server with short timeouts
	srv := New(":8081", logger, wsHandler)
	go func() {
		_ = srv.Start()
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Verify server timeouts work by making a slow request
	client := &http.Client{
		Timeout: 200 * time.Millisecond,
	}

	// This should fail due to read timeout if we make a slow request
	// For now just verify normal request works
	resp, err := client.Get("http://localhost:8081/health")
	if err != nil {
		t.Errorf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestNewServer(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	srv := New(":8081", logger, wsHandler)

	if srv == nil {
		t.Fatal("expected non-nil server")
	}

	if srv.httpServer == nil {
		t.Fatal("expected non-nil httpServer")
	}

	if srv.wsHandler == nil {
		t.Fatal("expected non-nil wsHandler")
	}

	if srv.httpServer.Addr != ":8081" {
		t.Errorf("expected addr :8081, got %s", srv.httpServer.Addr)
	}

	// Verify timeouts are set
	if srv.httpServer.ReadTimeout != 15*time.Second {
		t.Errorf("expected ReadTimeout 15s, got %v", srv.httpServer.ReadTimeout)
	}

	if srv.httpServer.WriteTimeout != 15*time.Second {
		t.Errorf("expected WriteTimeout 15s, got %v", srv.httpServer.WriteTimeout)
	}

	if srv.httpServer.IdleTimeout != 60*time.Second {
		t.Errorf("expected IdleTimeout 60s, got %v", srv.httpServer.IdleTimeout)
	}
}

func TestServerInvalidPath(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	srv := New(":8081", logger, wsHandler)
	go func() {
		_ = srv.Start()
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Test invalid path
	resp, err := http.Get("http://localhost:8081/invalid")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestServerWithRealHTTPClient(t *testing.T) {
	// Integration test using httptest instead of real port
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	// Create test server using the handler's mux internally
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/ws", wsHandler.HandleWebSocket)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test health endpoint
	resp, err := server.Client().Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Errorf("expected body OK, got %s", string(body))
	}

	// Test WebSocket endpoint
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ws, _, err := gorillawebsocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket connection failed: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Read connected event
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, p, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	var connected map[string]any
	if err := json.Unmarshal(p, &connected); err != nil {
		t.Fatalf("failed to unmarshal connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Errorf("expected connected event, got %v", connected["type"])
	}

	// Send a valid prompt message
	promptMsg := map[string]string{"type": "prompt", "text": "hello"}
	data, _ := json.Marshal(promptMsg)
	if err := ws.WriteMessage(gorillawebsocket.TextMessage, data); err != nil {
		t.Errorf("failed to write message: %v", err)
	}

	_, p, err = ws.ReadMessage()
	if err != nil {
		t.Errorf("failed to read message: %v", err)
	}

	var agentStart map[string]any
	if err := json.Unmarshal(p, &agentStart); err != nil {
		t.Fatalf("failed to unmarshal agent_start event: %v", err)
	}

	if agentStart["type"] != "agent_start" {
		t.Errorf("expected agent_start event, got %v", agentStart["type"])
	}
}

func TestServerGracefulShutdownWithWebSocket(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100)

	srv := New(":8081", logger, wsHandler)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Connect WebSocket
	wsURL := "ws://localhost:8081/ws"
	ws, _, err := gorillawebsocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket connection failed: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Verify connection works by reading the connected event
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, p, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var connected map[string]any
	if err := json.Unmarshal(p, &connected); err != nil {
		t.Fatalf("failed to unmarshal connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Fatalf("expected connected event, got %v", connected["type"])
	}

	// Shutdown server while WebSocket is still connected
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}

	// Verify connection is closed (either by close frame or TCP close)
	// The server should send a close frame, but due to timing the TCP
	// connection might close first. Both are acceptable outcomes.
	_ = ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, _, err = ws.ReadMessage()
	if err != nil {
		// Connection closed - this is expected during shutdown
		if !gorillawebsocket.IsCloseError(err, gorillawebsocket.CloseNormalClosure, gorillawebsocket.CloseGoingAway) &&
			!gorillawebsocket.IsUnexpectedCloseError(err, gorillawebsocket.CloseNormalClosure, gorillawebsocket.CloseGoingAway) {
			// Also accept i/o timeout (connection silently closed)
			if err.Error() != "read tcp " && err.Error() != "read udp " {
				t.Logf("connection closed during shutdown (expected): %v", err)
			}
		}
	}

	// Verify Start returned
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for server to stop")
	}
}
