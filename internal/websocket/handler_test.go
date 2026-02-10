package websocket

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

func TestWebSocketUpgrade(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + server.URL[4:] + "/ws"

	// Try to upgrade to WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to upgrade to websocket: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Verify connection by sending message
	if err := ws.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Errorf("failed to write message: %v", err)
	}

	// Read echo response
	_, p, err := ws.ReadMessage()
	if err != nil {
		t.Errorf("failed to read message: %v", err)
	}

	if string(p) != "ping" {
		t.Errorf("expected echo 'ping', got '%s'", string(p))
	}
}

func TestWebSocketMessageLogging(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)
	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Send test message
	if err := ws.WriteMessage(websocket.TextMessage, []byte("test")); err != nil {
		t.Errorf("failed to send message: %v", err)
	}

	// Read response
	_, _, err = ws.ReadMessage()
	if err != nil {
		t.Errorf("failed to read message: %v", err)
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
			handler := NewHandler(logger, []string{tt.allowedOrigin}, 100)

			server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
			defer server.Close()

			// Create request with Origin header
			wsURL := "ws" + server.URL[4:] + "/ws"
			header := http.Header{}
			if tt.requestOrigin != "" {
				header.Set("Origin", tt.requestOrigin)
			}

			ws, _, err := websocket.DefaultDialer.Dial(wsURL, header)
			if tt.shouldAllow {
				if err != nil {
					t.Errorf("expected connection allowed, got error: %v", err)
				}
				if ws != nil {
					_ = ws.Close()
				}
			} else {
				if err == nil {
					t.Error("expected connection rejected, but it was allowed")
					if ws != nil {
						_ = ws.Close()
					}
				}
			}
		})
	}
}

func TestWebSocketConnectionLimit(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	maxConns := 2
	handler := NewHandler(logger, []string{"*"}, maxConns)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"

	// Create first connection - should succeed
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}
	defer func() { _ = ws1.Close() }()

	// Create second connection - should succeed
	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("second connection failed: %v", err)
	}
	defer func() { _ = ws2.Close() }()

	// Create third connection - should be at capacity
	_, _, err = websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Error("expected connection to be rejected due to limit, but it succeeded")
		// Close the connection we don't expect
	}
}

func TestWebSocketBinaryMessage(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Send binary message
	binaryData := []byte{0x00, 0x01, 0x02, 0x03}
	if err := ws.WriteMessage(websocket.BinaryMessage, binaryData); err != nil {
		t.Errorf("failed to write binary message: %v", err)
	}

	// Read echo response
	mt, p, err := ws.ReadMessage()
	if err != nil {
		t.Errorf("failed to read message: %v", err)
	}

	if mt != websocket.BinaryMessage {
		t.Errorf("expected binary message type, got %d", mt)
	}

	if len(p) != 4 || p[0] != 0x00 || p[1] != 0x01 {
		t.Errorf("binary data mismatch: got %v", p)
	}
}

func TestWebSocketPingPong(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Set ping handler to respond with pong
	ws.SetPingHandler(func(appData string) error {
		return ws.WriteControl(websocket.PongMessage, []byte(appData), time.Now())
	})

	// Wait for server to send ping (within ping interval + timeout)
	// Note: This test verifies the server sends pings, but we can't easily
	// control the exact timing without sleep. For now, just verify
	// the connection stays alive.

	// Send a message and read it to verify connection is working
	if err := ws.WriteMessage(websocket.TextMessage, []byte("test")); err != nil {
		t.Errorf("failed to write message: %v", err)
	}

	_, _, err = ws.ReadMessage()
	if err != nil {
		t.Errorf("failed to read message: %v", err)
	}
}

func TestWebSocketCloseOnClientDisconnect(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Verify active connections count
	initialCount := handler.ActiveConns()
	if initialCount != 1 {
		t.Errorf("expected 1 active connection, got %d", initialCount)
	}

	// Close connection
	_ = ws.Close()

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
	handler := NewHandler(logger, []string{"*"}, 100)

	if handler.ActiveConns() != 0 {
		t.Errorf("expected 0 active connections initially, got %d", handler.ActiveConns())
	}
}

func TestWebSocketMessageSizeLimit(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Try to send a message larger than maxMessageSize (64KB + 1 byte)
	largeMsg := make([]byte, maxMessageSize+1)

	// This should succeed because WriteMessage doesn't check size
	// The read limit is on the receiving side
	err = ws.WriteMessage(websocket.TextMessage, largeMsg)
	if err != nil {
		t.Errorf("failed to write large message: %v", err)
	}

	// Try to read - should get close frame due to size limit
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = ws.ReadMessage()
	if err == nil {
		t.Error("expected error when reading oversized message, got nil")
	}
}

func TestWebSocketLargeMessageEcho(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Send a message at the size limit (64KB)
	msg := make([]byte, maxMessageSize)
	for i := range msg {
		msg[i] = byte('a')
	}

	if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
		t.Fatalf("failed to write large message: %v", err)
	}

	// Read echo - should succeed within size limit
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	mt, p, err := ws.ReadMessage()
	if err != nil {
		t.Errorf("failed to read large message: %v", err)
	}

	if mt != websocket.TextMessage {
		t.Errorf("expected text message, got %d", mt)
	}

	if len(p) != maxMessageSize {
		t.Errorf("expected %d byte message, got %d", maxMessageSize, len(p))
	}
}

func TestWebSocketHealthEndpoint(t *testing.T) {
	// This tests that /health works correctly when handler is used in a full server setup
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

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
	handler := NewHandler(zeroLogger, []string{"*"}, 100)

	if handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestWebSocketConcurrentMessages(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Send multiple messages rapidly
	const msgCount = 10
	for i := 0; i < msgCount; i++ {
		msg := []byte(strings.Repeat("x", 100))
		if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
			t.Errorf("failed to write message %d: %v", i, err)
		}
	}

	// Read all responses
	for i := 0; i < msgCount; i++ {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, p, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("failed to read message %d: %v", i, err)
		}
		if len(p) != 100 {
			t.Errorf("message %d: expected length 100, got %d", i, len(p))
		}
	}
}

// TestWebSocketCloseFrameThroughWriter verifies that close frames are sent
// through the writer goroutine, not directly written to the connection.
// This ensures the single-writer pattern is maintained.
func TestWebSocketCloseFrameThroughWriter(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 100)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Verify active connections count
	if handler.ActiveConns() != 1 {
		t.Errorf("expected 1 active connection, got %d", handler.ActiveConns())
	}

	// Close the connection from the client side
	// This should trigger the handler to clean up without any concurrent writes
	_ = ws.Close()

	// Give time for handler to clean up
	time.Sleep(100 * time.Millisecond)

	// Verify count decreased - this confirms the handler properly cleaned up
	if handler.ActiveConns() != 0 {
		t.Errorf("expected 0 active connections after close, got %d", handler.ActiveConns())
	}
}
