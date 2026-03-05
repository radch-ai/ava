package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/rs/zerolog"

	"github.com/nevaria-ai/gateway/internal/websocket"
)

// TestServerHealthEndpoint tests the health check endpoint without auth.
func TestServerHealthEndpoint(t *testing.T) {
	// Create a simple test server without auth middleware
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

	testSrv := httptest.NewServer(mux)
	defer testSrv.Close()

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
			req, _ := http.NewRequest(tt.method, testSrv.URL+"/health", nil)
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

// TestServerHealthResponseBody tests that health endpoint returns "OK".
func TestServerHealthResponseBody(t *testing.T) {
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

	testSrv := httptest.NewServer(mux)
	defer testSrv.Close()

	resp, err := testSrv.Client().Get(testSrv.URL + "/health")
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
}

// TestServerInvalidPath tests that unknown paths return 404.
func TestServerInvalidPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testSrv := httptest.NewServer(mux)
	defer testSrv.Close()

	resp, err := testSrv.Client().Get(testSrv.URL + "/invalid")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

// TestWebSocketHandlerWithoutAuth tests the WebSocket handler directly without auth.
func TestWebSocketHandlerWithoutAuth(t *testing.T) {
	logger := zerolog.Nop()
	wsHandler := websocket.NewHandler(logger, []string{"*"}, 100, &agent.UserAgentManagerOptions{})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler.HandleWebSocket)

	testSrv := httptest.NewServer(mux)
	defer testSrv.Close()

	// Test WebSocket endpoint
	wsURL := "ws" + strings.TrimPrefix(testSrv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := coderws.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket connection failed: %v", err)
	}
	defer func() { _ = ws.CloseNow() }()

	// Read connected event
	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, reader, err := ws.Reader(readCtx)
	readCancel()
	if err != nil {
		t.Fatalf("failed to read connected event: %v", err)
	}
	p, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read connected event data: %v", err)
	}

	var connected map[string]any
	if err := parseJSON(p, &connected); err != nil {
		t.Fatalf("failed to unmarshal connected event: %v", err)
	}

	if connected["type"] != "connected" {
		t.Errorf("expected connected event, got %v", connected["type"])
	}

	// Send a valid prompt message with required agent_id
	promptMsg := map[string]string{"type": "prompt", "agent_id": "test-agent-1", "text": "hello"}
	data, _ := serializeJSON(promptMsg)
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	err = ws.Write(writeCtx, coderws.MessageText, data)
	writeCancel()
	if err != nil {
		t.Errorf("failed to write message: %v", err)
	}

	// Note: agent_start event won't be sent because LLM client is not configured
	readCtx2, readCancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, _, err = ws.Reader(readCtx2)
	readCancel2()
	if err != nil {
		// Timeout is expected - no LLM client configured
		t.Log("no message received (expected - no LLM client configured)")
	}
}

// Note: Auth integration tests (WebSocket with auth, auth middleware, etc.)
// are in tests/integration/ and require a running Zitadel instance or mock server.

// Helper functions for JSON operations
func parseJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func serializeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
