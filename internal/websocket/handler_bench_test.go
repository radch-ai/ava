package websocket

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

func BenchmarkWebSocketEcho(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 10000)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("connection failed: %v", err)
		}

		// Send a message
		msg := []byte("benchmark test message")
		if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
			_ = ws.Close()
			b.Fatalf("write failed: %v", err)
		}

		// Read response
		if _, _, err := ws.ReadMessage(); err != nil {
			_ = ws.Close()
			b.Fatalf("read failed: %v", err)
		}

		_ = ws.Close()
	}
}

func BenchmarkWebSocketSmallMessages(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 10000)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		b.Fatalf("connection failed: %v", err)
	}
	defer func() { _ = ws.Close() }()

	b.ResetTimer()
	msg := []byte("test")

	for i := 0; i < b.N; i++ {
		if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}

		if _, _, err := ws.ReadMessage(); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}

func BenchmarkWebSocketMediumMessages(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 10000)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		b.Fatalf("connection failed: %v", err)
	}
	defer func() { _ = ws.Close() }()

	b.ResetTimer()
	msg := make([]byte, 1024) // 1KB message

	for i := 0; i < b.N; i++ {
		if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}

		if _, _, err := ws.ReadMessage(); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}

func BenchmarkWebSocketLargeMessages(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 10000)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		b.Fatalf("connection failed: %v", err)
	}
	defer func() { _ = ws.Close() }()

	b.ResetTimer()
	msg := make([]byte, 8192) // 8KB message (buffer size)

	for i := 0; i < b.N; i++ {
		if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}

		if _, _, err := ws.ReadMessage(); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}

func BenchmarkActiveConns(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := NewHandler(logger, []string{"*"}, 10000)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = handler.ActiveConns()
	}
}
