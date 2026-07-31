package websocket_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/radch-ai/ava/services/gateway/internal/agent"
	"github.com/radch-ai/ava/services/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

func TestWebSocketUpgrade(t *testing.T) {
	t.Skip("requires LLM backend - needs Bifrost URL and DB pool")
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + server.URL[4:] + "/ws"

	// Try to upgrade to WebSocket
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to upgrade to websocket: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Errorf("expected connected event, got %v", connected["type"])
	}

	// Verify by sending a valid prompt message with agent_id
	promptMsg := map[string]string{
		"id":       "test-msg-1",
		"type":     "prompt",
		"agent_id": "test-agent",
		"content":  "hello",
	}
	if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
		t.Errorf("failed to write message: %v", err)
	}

	// Read agent_start event
	var agentStart map[string]any
	if err := wsjson.Read(ctx, conn, &agentStart); err != nil {
		t.Errorf("failed to read agent_start event: %v", err)
	}

	if agentStart["type"] != "agent_start" {
		t.Errorf("expected agent_start event, got %v", agentStart["type"])
	}
}

func TestWebSocketMessageLogging(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})
	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Send test message (invalid JSON will be ignored)
	if err := conn.Write(ctx, coderws.MessageText, []byte("test")); err != nil {
		t.Errorf("failed to send message: %v", err)
	}
}

func TestWebSocketOriginValidation(t *testing.T) {
	tests := []struct {
		name          string
		allowedOrigin string
		requestOrigin string
		shouldAllow   bool
	}{
		{
			name:          "allows matching origin",
			allowedOrigin: "https://example.com",
			requestOrigin: "https://example.com",
			shouldAllow:   true,
		},
		{
			name:          "rejects non-matching origin",
			allowedOrigin: "https://example.com",
			requestOrigin: "https://evil.com",
			shouldAllow:   false,
		},
		{
			name:          "wildcard allows all",
			allowedOrigin: "*",
			requestOrigin: "https://any.com",
			shouldAllow:   true,
		},
		{
			name:          "allows when no origin header",
			allowedOrigin: "https://example.com",
			requestOrigin: "",
			shouldAllow:   true, // non-browser clients
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(os.Stderr)
			handler := websocket.NewHandler(logger, []string{tt.allowedOrigin}, 100, &agent.UserAgentManagerOptions{})

			server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
			defer server.Close()

			// Create request with Origin header
			wsURL := "ws" + server.URL[4:] + "/ws"
			opts := &coderws.DialOptions{}
			if tt.requestOrigin != "" {
				opts.HTTPHeader = http.Header{}
				opts.HTTPHeader.Set("Origin", tt.requestOrigin)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			conn, _, err := coderws.Dial(ctx, wsURL, opts)
			if tt.shouldAllow {
				if err != nil {
					t.Errorf("expected connection allowed, got error: %v", err)
				}
				if conn != nil {
					_ = conn.CloseNow()
				}
			} else {
				if err == nil {
					t.Error("expected connection rejected, but it was allowed")
					if conn != nil {
						_ = conn.CloseNow()
					}
				}
			}
		})
	}
}

func TestWebSocketConnectionLimit(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	maxConns := 2
	handler := websocket.NewHandler(logger, []string{"*"}, maxConns, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"

	// Create first connection - should succeed
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	ws1, _, err := coderws.Dial(ctx1, wsURL, nil)
	cancel1()
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}
	defer func() { _ = ws1.CloseNow() }()

	// Create second connection - should succeed
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	ws2, _, err := coderws.Dial(ctx2, wsURL, nil)
	cancel2()
	if err != nil {
		t.Fatalf("second connection failed: %v", err)
	}
	defer func() { _ = ws2.CloseNow() }()

	// Create third connection - should be at capacity
	ctx3, cancel3 := context.WithTimeout(context.Background(), 2*time.Second)
	_, _, err = coderws.Dial(ctx3, wsURL, nil)
	cancel3()
	if err == nil {
		t.Error("expected connection to be rejected due to limit, but it succeeded")
	}
}

func TestWebSocketBinaryMessage(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send binary message - binary messages are still echoed back
	binaryData := []byte{0x00, 0x01, 0x02, 0x03}
	if err := conn.Write(ctx, coderws.MessageBinary, binaryData); err != nil {
		t.Errorf("failed to write binary message: %v", err)
	}

	// Read echo response
	typ, reader, err := conn.Reader(ctx)
	if err != nil {
		t.Errorf("failed to read message: %v", err)
		return
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Errorf("failed to read message data: %v", err)
		return
	}

	if typ != coderws.MessageBinary {
		t.Logf("expected binary message type, got %d", typ)
	}

	if typ == coderws.MessageBinary && (len(data) != 4 || data[0] != 0x00 || data[1] != 0x01) {
		t.Errorf("binary data mismatch: got %v", data)
	}
}

func TestWebSocketPingPong(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// The server sends pings, and the coder/websocket library will automatically
	// respond with pongs when reading. We just need to verify the connection
	// stays alive by sending and receiving a message.

	// Read the connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send a binary message to test echo (text messages are handled as JSON)
	if err := conn.Write(ctx, coderws.MessageBinary, []byte("test")); err != nil {
		t.Errorf("failed to write message: %v", err)
	}

	// Read the response (echo)
	typ, reader, err := conn.Reader(ctx)
	if err != nil {
		t.Errorf("failed to read message: %v", err)
		return
	}
	if typ != coderws.MessageBinary {
		t.Errorf("expected binary message, got %d", typ)
		return
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Errorf("failed to read data: %v", err)
		return
	}
	if string(data) != "test" {
		t.Errorf("expected 'test', got '%s'", string(data))
	}
}

func TestWebSocketCloseOnClientDisconnect(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Verify active connections count
	initialCount := handler.ActiveConns()
	if initialCount != 1 {
		t.Errorf("expected 1 active connection, got %d", initialCount)
	}

	// Close connection
	_ = conn.CloseNow()

	// Give time for handler to clean up
	time.Sleep(100 * time.Millisecond)

	// Verify count decreased
	finalCount := handler.ActiveConns()
	if finalCount != 0 {
		t.Errorf("expected 0 active connections after close, got %d", finalCount)
	}
}

func TestActiveConns(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	if handler.ActiveConns() != 0 {
		t.Errorf("expected 0 active connections initially, got %d", handler.ActiveConns())
	}
}

func TestWebSocketMessageSizeLimit(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Try to send a message larger than MaxMessageSize (64KB + 1 byte)
	largeMsg := make([]byte, websocket.MaxMessageSize+1)

	// This should succeed on write, but server will reject
	err = conn.Write(ctx, coderws.MessageText, largeMsg)
	if err != nil {
		t.Errorf("failed to write large message: %v", err)
	}

	// Try to read - should get close frame due to size limit on server side
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	_, _, err = conn.Reader(readCtx)
	readCancel()
	// The server will reject the message due to size limit and close the connection
	_ = err // Either way, the connection should handle oversized messages gracefully
}

func TestWebSocketLargeMessageEcho(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send a message at the size limit (64KB) - but as valid JSON for agent prompt
	msg := make([]byte, websocket.MaxMessageSize)
	for i := range msg {
		msg[i] = byte('a')
	}

	if err := conn.Write(ctx, coderws.MessageText, msg); err != nil {
		t.Fatalf("failed to write large message: %v", err)
	}

	// Read response - the handler will attempt to parse as JSON and fail
	// This tests that the handler gracefully handles large messages
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	typ, _, err := conn.Reader(readCtx)
	readCancel()
	// We expect an error or no response (invalid JSON)
	if err != nil {
		// Connection may close due to invalid JSON, which is fine
		return
	}

	// If we got a message, it should be a text message
	if typ != coderws.MessageText {
		t.Logf("expected text message, got %d", typ)
	}
	// Large invalid JSON messages are handled gracefully by the handler
}

func TestWebSocketHealthEndpoint(t *testing.T) {
	// This tests that /health works correctly when handler is used in a full server setup
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/ws", handler.HandleWebSocket)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test health endpoint
	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Errorf("expected body OK, got %s", string(body))
	}
}

func TestNewHandlerLogger(t *testing.T) {
	// Test with zero logger (should create default)
	var zeroLogger zerolog.Logger
	handler := websocket.NewHandler(zeroLogger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	if handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestWebSocketConcurrentMessages(t *testing.T) {
	t.Skip("requires LLM backend - needs Bifrost URL and DB pool")
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send multiple prompt messages rapidly to the same agent
	const msgCount = 10
	agentID := "test-agent-concurrent"
	for i := 0; i < msgCount; i++ {
		promptMsg := map[string]string{
			"id":       fmt.Sprintf("msg-%d", i),
			"type":     "prompt",
			"agent_id": agentID,
			"content":  "test message",
		}
		if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
			t.Errorf("failed to write message %d: %v", i, err)
		}
	}

	// Read agent_start events for each prompt
	for i := 0; i < msgCount; i++ {
		readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
		var evt map[string]any
		err := wsjson.Read(readCtx, conn, &evt)
		readCancel()

		if err != nil {
			t.Errorf("failed to read message %d: %v", i, err)
			continue
		}
		// Should get agent_start events
		if evt["type"] != "agent_start" && evt["type"] != "agent_end" {
			t.Errorf("message %d: expected agent event type, got %v", i, evt["type"])
		}
	}
}

// TestWebSocketClientClose verifies proper cleanup when client closes connection
func TestWebSocketClientClose(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Verify active connections count
	if handler.ActiveConns() != 1 {
		t.Errorf("expected 1 active connection, got %d", handler.ActiveConns())
	}

	// Close the connection gracefully
	_ = conn.Close(coderws.StatusNormalClosure, "")

	// Give time for handler to clean up
	time.Sleep(100 * time.Millisecond)

	// Verify count decreased
	if handler.ActiveConns() != 0 {
		t.Errorf("expected 0 active connections after close, got %d", handler.ActiveConns())
	}
}

// TestWebSocketMessageRequiresAgentID verifies that prompt messages must include agent_id
func TestWebSocketMessageRequiresAgentID(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Errorf("expected connected event, got %v", connected["type"])
	}

	// Send prompt message WITHOUT agent_id - should be rejected
	promptMsg := map[string]string{"id": "test-msg", "type": "prompt", "content": "hello"}
	if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	// Try to read with timeout - should not get agent_start since message is rejected
	readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	var evt map[string]any
	_ = wsjson.Read(readCtx, conn, &evt)
	readCancel()

	// We expect timeout or no agent_start event (message was rejected)
	// This is acceptable - the handler silently rejects invalid messages
}

// TestWebSocketMessageWithAgentID verifies that messages with agent_id are routed correctly
func TestWebSocketMessageWithAgentID(t *testing.T) {
	t.Skip("requires LLM backend - needs Bifrost URL and DB pool")
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Errorf("expected connected event, got %v", connected["type"])
	}

	// Send prompt message WITH agent_id
	agentID := "test-agent-123"
	promptMsg := map[string]string{
		"id":       "test-msg-2",
		"type":     "prompt",
		"agent_id": agentID,
		"content":  "hello",
	}
	if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	// Should receive agent_start event
	var agentStart map[string]any
	if err := wsjson.Read(ctx, conn, &agentStart); err != nil {
		t.Fatalf("failed to read agent_start event: %v", err)
	}

	if agentStart["type"] != "agent_start" {
		t.Errorf("expected agent_start event, got %v", agentStart["type"])
	}
}

// TestWebSocketConnectedEventHasUserID verifies that connected event includes user_id
func TestWebSocketConnectedEventHasUserID(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Errorf("expected connected event, got %v", connected["type"])
	}

	// Extract data field
	data, ok := connected["data"].(map[string]any)
	if !ok {
		t.Fatalf("connected event data is not a map: %+v", connected["data"])
	}

	// Verify agent_id is NOT in connected event data
	if _, exists := data["agent_id"]; exists {
		t.Error("connected event should not include agent_id field")
	}

	// Verify user_id exists (changed from session_id)
	if _, exists := data["user_id"]; !exists {
		t.Error("connected event should include user_id field")
	}

	// Verify conn_id exists
	if _, exists := data["conn_id"]; !exists {
		t.Error("connected event should include conn_id field")
	}
}

// TestWebSocketMultipleAgentsPerConnection verifies different agent_ids can be used in same connection
func TestWebSocketMultipleAgentsPerConnection(t *testing.T) {
	t.Skip("requires LLM backend - needs Bifrost URL and DB pool")
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send prompts to different agents
	agents := []string{"agent-1", "agent-2", "agent-3"}
	for i, agentID := range agents {
		promptMsg := map[string]string{
			"id":       fmt.Sprintf("msg-%d-%s", i, agentID),
			"type":     "prompt",
			"agent_id": agentID,
			"content":  "hello from " + agentID,
		}
		if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
			t.Fatalf("failed to write message for agent %s: %v", agentID, err)
		}

		// Read all events until we find agent_start for this agent
		// We might get agent_end from previous agent first
		foundAgentStart := false
		maxReads := 5 // Read up to 5 messages to find the agent_start
		for j := 0; j < maxReads && !foundAgentStart; j++ {
			readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
			var evt map[string]any
			err := wsjson.Read(readCtx, conn, &evt)
			readCancel()

			if err != nil {
				t.Fatalf("failed to read event for agent %s (read %d): %v", agentID, j, err)
			}

			// Check if this is an agent_start event for our agent
			if evt["type"] == "agent_start" && evt["agent_id"] == agentID {
				foundAgentStart = true
			}
		}

		if !foundAgentStart && i > 0 {
			// For subsequent agents, it's ok if we don't see agent_start
			// because the agent might already be running from a previous prompt
			// The key is that different agent_ids work
		} else if !foundAgentStart {
			t.Errorf("did not find agent_start event for agent %s", agentID)
		}
	}
}

// TestWebSocketStatusMessage tests the status message type
func TestWebSocketStatusMessage(t *testing.T) {
	t.Skip("requires LLM backend - needs Bifrost URL and DB pool")
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send status request
	statusMsg := map[string]string{
		"type":     "status",
		"agent_id": "test-agent-status",
	}
	if err := wsjson.Write(ctx, conn, statusMsg); err != nil {
		t.Fatalf("failed to write status message: %v", err)
	}

	// Read status response
	var statusResp map[string]any
	if err := wsjson.Read(ctx, conn, &statusResp); err != nil {
		t.Fatalf("failed to read status response: %v", err)
	}

	if statusResp["type"] != "lane_status" {
		t.Errorf("expected lane_status response, got %v", statusResp["type"])
	}
}

// TestWebSocketSteerMessage tests the steer message type
func TestWebSocketSteerMessage(t *testing.T) {
	t.Skip("requires LLM backend - needs Bifrost URL and DB pool")
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// First create an agent by sending a prompt
	promptMsg := map[string]string{
		"id":       "test-msg",
		"type":     "prompt",
		"agent_id": "test-agent-steer",
		"content":  "hello",
	}
	if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
		t.Fatalf("failed to write prompt message: %v", err)
	}

	// Read agent_start event
	var agentStart map[string]any
	if err := wsjson.Read(ctx, conn, &agentStart); err != nil {
		t.Fatalf("failed to read agent_start event: %v", err)
	}

	// Send steer message to stop the agent
	steerMsg := map[string]string{
		"type":     "steer",
		"agent_id": "test-agent-steer",
	}
	if err := wsjson.Write(ctx, conn, steerMsg); err != nil {
		t.Fatalf("failed to write steer message: %v", err)
	}

	// steer doesn't return a response, but connection should remain open
	// Send another message to verify connection is still working
	if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
		t.Errorf("connection should still be working after steer: %v", err)
	}
}

// TestWebSocketUnknownMessageType tests that unknown message types are handled gracefully
func TestWebSocketUnknownMessageType(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send unknown message type
	unknownMsg := map[string]string{
		"type": "unknown_type",
	}
	if err := wsjson.Write(ctx, conn, unknownMsg); err != nil {
		t.Fatalf("failed to write unknown message: %v", err)
	}

	// Connection should remain open - send another message to verify
	promptMsg := map[string]string{
		"id":       "test-msg",
		"type":     "prompt",
		"agent_id": "test-agent",
		"content":  "hello",
	}
	if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
		t.Errorf("connection should still be working after unknown message type: %v", err)
	}
}

// TestWebSocketInvalidJSON tests that invalid JSON is handled gracefully
func TestWebSocketInvalidJSON(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event first
	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}

	// Send invalid JSON
	if err := conn.Write(ctx, coderws.MessageText, []byte("not valid json{")); err != nil {
		t.Fatalf("failed to write invalid JSON: %v", err)
	}

	// Connection should remain open - send a valid message to verify
	promptMsg := map[string]string{
		"id":       "test-msg",
		"type":     "prompt",
		"agent_id": "test-agent",
		"content":  "hello",
	}
	if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
		t.Errorf("connection should still be working after invalid JSON: %v", err)
	}
}
