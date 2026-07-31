package websocket_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/services/gateway/internal/websocket"
)

func BenchmarkWebSocketEcho(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 10000, nil)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, _, err := coderws.Dial(ctx, wsURL, nil)
		if err != nil {
			cancel()
			b.Fatalf("connection failed: %v", err)
		}

		// Read connected event
		var connected map[string]any
		_ = wsjson.Read(ctx, conn, &connected)

		// Send a message
		msg := []byte("benchmark test message")
		if err := conn.Write(ctx, coderws.MessageText, msg); err != nil {
			_ = conn.CloseNow()
			cancel()
			b.Fatalf("write failed: %v", err)
		}

		// Read response
		_, reader, err := conn.Reader(ctx)
		if err != nil {
			_ = conn.CloseNow()
			cancel()
			b.Fatalf("read failed: %v", err)
		}
		_, _ = io.ReadAll(reader)

		_ = conn.CloseNow()
		cancel()
	}
}

func BenchmarkWebSocketSmallMessages(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 10000, nil)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("connection failed: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event
	var connected map[string]any
	_ = wsjson.Read(ctx, conn, &connected)

	b.ResetTimer()
	msg := []byte("test")

	for i := 0; i < b.N; i++ {
		if err := conn.Write(ctx, coderws.MessageText, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}

		_, reader, err := conn.Reader(ctx)
		if err != nil {
			b.Fatalf("read failed: %v", err)
		}
		_, _ = io.ReadAll(reader)
	}
}

func BenchmarkWebSocketMediumMessages(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 10000, nil)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("connection failed: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event
	var connected map[string]any
	_ = wsjson.Read(ctx, conn, &connected)

	b.ResetTimer()
	msg := make([]byte, 1024) // 1KB message

	for i := 0; i < b.N; i++ {
		if err := conn.Write(ctx, coderws.MessageText, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}

		_, reader, err := conn.Reader(ctx)
		if err != nil {
			b.Fatalf("read failed: %v", err)
		}
		_, _ = io.ReadAll(reader)
	}
}

func BenchmarkWebSocketLargeMessages(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 10000, nil)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("connection failed: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event
	var connected map[string]any
	_ = wsjson.Read(ctx, conn, &connected)

	b.ResetTimer()
	msg := make([]byte, 8192) // 8KB message (buffer size)

	for i := 0; i < b.N; i++ {
		if err := conn.Write(ctx, coderws.MessageText, msg); err != nil {
			b.Fatalf("write failed: %v", err)
		}

		_, reader, err := conn.Reader(ctx)
		if err != nil {
			b.Fatalf("read failed: %v", err)
		}
		_, _ = io.ReadAll(reader)
	}
}

func BenchmarkActiveConns(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 10000, nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = handler.ActiveConns()
	}
}

func BenchmarkWebSocketJSONMessages(b *testing.B) {
	logger := zerolog.New(os.Stderr)
	handler := websocket.NewHandler(logger, []string{"*"}, 10000, nil)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("connection failed: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Read connected event
	var connected map[string]any
	_ = wsjson.Read(ctx, conn, &connected)

	b.ResetTimer()

	promptMsg := map[string]string{
		"id":       "bench-msg",
		"type":     "prompt",
		"agent_id": "bench-agent",
		"text":     "benchmark test message",
	}

	for i := 0; i < b.N; i++ {
		if err := wsjson.Write(ctx, conn, promptMsg); err != nil {
			b.Fatalf("write failed: %v", err)
		}

		var resp map[string]any
		if err := wsjson.Read(ctx, conn, &resp); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}
