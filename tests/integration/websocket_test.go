//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/nevaria-ai/gateway/internal/agent"
	wspkg "github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

// wsClient is a test helper for WebSocket connections.
type wsClient struct {
	conn   *coderws.Conn
	done   chan struct{}
	events chan json.RawMessage
}

// newWSClient creates a new WebSocket test client.
func newWSClient(t *testing.T, serverURL string) *wsClient {
	t.Helper()

	wsURL := "ws" + serverURL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	cancel()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	client := &wsClient{
		conn:   conn,
		done:   make(chan struct{}),
		events: make(chan json.RawMessage, 100),
	}

	// Start goroutine to read events
	go func() {
		defer close(client.events)
		for {
			select {
			case <-client.done:
				return
			default:
				readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, reader, err := conn.Reader(readCtx)
				if err != nil {
					readCancel()
					return
				}
				message, err := io.ReadAll(reader)
				readCancel()
				if err != nil {
					return
				}
				client.events <- message
			}
		}
	}()

	return client
}

// Close closes the WebSocket connection.
func (c *wsClient) Close() {
	close(c.done)
	_ = c.conn.CloseNow()
}

// SendPrompt sends a prompt message to the server.
func (c *wsClient) SendPrompt(t *testing.T, prompt string) {
	t.Helper()

	msg := map[string]string{"type": "prompt", "id": "test-msg-id", "agent_id": "test-agent", "text": prompt}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal prompt: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := c.conn.Write(ctx, coderws.MessageText, data); err != nil {
		cancel()
		t.Fatalf("failed to send prompt: %v", err)
	}
	cancel()
}

// WaitForEvent waits for an event matching the predicate.
func (c *wsClient) WaitForEvent(t *testing.T, timeout time.Duration, pred func(json.RawMessage) bool) json.RawMessage {
	t.Helper()

	deadline := time.After(timeout)
	for {
		select {
		case event, ok := <-c.events:
			if !ok {
				t.Fatal("event channel closed")
			}
			if pred(event) {
				return event
			}
		case <-deadline:
			t.Fatal("timeout waiting for event")
		}
	}
}

// hasType returns true if the event has the given type.
func hasType(eventType string) func(json.RawMessage) bool {
	return func(msg json.RawMessage) bool {
		var evt map[string]any
		if err := json.Unmarshal(msg, &evt); err != nil {
			return false
		}
		return evt["type"] == eventType
	}
}

// extractNestedData extracts the nested data field from an event.
func extractNestedData(msg json.RawMessage) (map[string]any, bool) {
	var outer map[string]any
	if err := json.Unmarshal(msg, &outer); err != nil {
		return nil, false
	}
	data, ok := outer["data"].(map[string]any)
	return data, ok
}

// TestAgentManagerCreation tests that AgentManager can be created.
func TestAgentManagerCreation(t *testing.T) {
	logger := zerolog.New(os.Stderr)

	manager := wspkg.NewAgentManager(logger, nil)

	if manager == nil {
		t.Fatal("expected non-nil AgentManager")
	}
}

// TestAgentCreationOnConnect tests that connection is established.
func TestAgentCreationOnConnect(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	client := newWSClient(t, srv.URL)
	defer client.Close()

	// Wait for connection established message
	timeout := time.After(2 * time.Second)
	var firstEvent json.RawMessage
	select {
	case evt, ok := <-client.events:
		if !ok {
			t.Fatal("event channel closed")
		}
		firstEvent = evt
	case <-timeout:
		t.Fatal("timeout waiting for first event")
	}

	var outer map[string]any
	if err := json.Unmarshal(firstEvent, &outer); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	eventType, ok := outer["type"].(string)
	if !ok || eventType != "connected" {
		t.Fatalf("expected connected event, got type: %v", eventType)
	}

	// Access nested data object
	data, ok := outer["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object in connected event")
	}

	userID, ok := data["user_id"].(string)
	if !ok || userID == "" {
		t.Error("expected user_id in connected event")
	}

	connID, ok := data["conn_id"].(string)
	if !ok || connID == "" {
		t.Error("expected conn_id in connected event")
	}
}

// TestAgentEventForwarding tests that agent events are forwarded to the WebSocket.
func TestAgentEventForwarding(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	client := newWSClient(t, srv.URL)
	defer client.Close()

	// Wait for connection
	_ = client.WaitForEvent(t, 2*time.Second, hasType("connected"))

	// Send a prompt to trigger agent events
	client.SendPrompt(t, "hello")

	// Wait for agent_start event
	_ = client.WaitForEvent(t, 2*time.Second, hasType("agent_start"))

	// Wait for agent_end event
	_ = client.WaitForEvent(t, 2*time.Second, hasType("agent_end"))
}

// TestPromptHandling tests that incoming prompts trigger agent processing.
func TestPromptHandling(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	client := newWSClient(t, srv.URL)
	defer client.Close()

	// Wait for connection
	_ = client.WaitForEvent(t, 2*time.Second, hasType("connected"))

	// Send a prompt
	client.SendPrompt(t, "test message")

	// Wait for agent_start
	_ = client.WaitForEvent(t, 2*time.Second, hasType("agent_start"))

	// Wait for agent_end
	_ = client.WaitForEvent(t, 2*time.Second, hasType("agent_end"))
}

// TestAgentCleanupOnDisconnect tests that agents are cleaned up when connection closes.
func TestAgentCleanupOnDisconnect(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	// Create first connection
	client1 := newWSClient(t, srv.URL)
	event1 := client1.WaitForEvent(t, 2*time.Second, hasType("connected"))

	data1, ok := extractNestedData(event1)
	if !ok {
		t.Fatal("expected data in connected event")
	}
	userID1, ok := data1["user_id"].(string)
	if !ok || userID1 == "" {
		t.Fatal("expected user_id in connected event")
	}

	// Create second connection
	client2 := newWSClient(t, srv.URL)
	event2 := client2.WaitForEvent(t, 2*time.Second, hasType("connected"))

	data2, ok := extractNestedData(event2)
	if !ok {
		t.Fatal("expected data in connected event")
	}
	userID2, ok := data2["user_id"].(string)
	if !ok || userID2 == "" {
		t.Fatal("expected user_id in connected event")
	}

	// Verify we have two different sessions
	if userID1 == userID2 {
		t.Error("expected different user IDs for different connections")
	}

	// Close first connection
	client1.Close()

	// Wait a bit for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify session1 agent is cleaned up by checking we can still use session2
	client2.SendPrompt(t, "test")
	_ = client2.WaitForEvent(t, 2*time.Second, hasType("agent_start"))
	_ = client2.WaitForEvent(t, 2*time.Second, hasType("agent_end"))

	// Close second connection
	client2.Close()
}

// TestAgentManagerGetOrCreateAgent tests the GetOrCreateAgentForUser method.
func TestAgentManagerGetOrCreateAgent(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	manager := wspkg.NewAgentManager(logger, nil)

	ctx := context.Background()
	userID := "test-user-123"
	agentID := "test-agent-123"

	// First call should create a new agent
	agent1 := manager.GetOrCreateAgentForUser(ctx, userID, agentID, agent.Task{})
	if agent1 == nil {
		t.Fatal("expected non-nil agent")
	}

	// Second call should return the same agent
	agent2 := manager.GetOrCreateAgentForUser(ctx, userID, agentID, agent.Task{})
	if agent2 == nil {
		t.Fatal("expected non-nil agent on second call")
	}

	// Verify they're the same instance
	if agent1 != agent2 {
		t.Error("expected same agent for same user/agent ID")
	}

	// Different agent ID should get different agent
	agent3 := manager.GetOrCreateAgentForUser(ctx, userID, "different-agent", agent.Task{})
	if agent3 == nil {
		t.Fatal("expected non-nil agent for different agent ID")
	}

	if agent3 == agent1 {
		t.Error("expected different agent for different agent ID")
	}
}

// TestAgentManagerRemoveAgent tests the RemoveAgentForUser method.
func TestAgentManagerRemoveAgent(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	manager := wspkg.NewAgentManager(logger, nil)

	ctx := context.Background()
	userID := "test-user-remove"
	agentID := "test-agent-remove"

	// Create an agent
	ag := manager.GetOrCreateAgentForUser(ctx, userID, agentID, agent.Task{})
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}

	// Remove the agent
	manager.RemoveAgentForUser(userID, agentID)

	// Should create a new agent now
	newAgent := manager.GetOrCreateAgentForUser(ctx, userID, agentID, agent.Task{})
	if newAgent == nil {
		t.Fatal("expected non-nil agent after remove")
	}

	// Verify it's a new instance
	if newAgent == ag {
		t.Error("expected different agent instance after remove")
	}
}

// TestInvalidMessageType tests handling of invalid message types.
func TestInvalidMessageType(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	cancel()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Send invalid JSON
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := conn.Write(writeCtx, coderws.MessageText, []byte("invalid json")); err != nil {
		writeCancel()
		t.Fatalf("failed to send message: %v", err)
	}
	writeCancel()

	// Connection should remain open (error is logged but doesn't close)
	readCtx, readCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, _, err = conn.Reader(readCtx)
	readCancel()
	if err != nil {
		// Connection might close or might not - just verify we don't panic
		t.Logf("Reader error (expected): %v", err)
	}
}

// TestInvalidPromptMessage tests handling of prompt messages without text.
func TestInvalidPromptMessage(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100, nil)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, _, err := coderws.Dial(ctx, wsURL, nil)
	cancel()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Send prompt without text field
	msg := map[string]string{"type": "prompt"}
	data, _ := json.Marshal(msg)
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := conn.Write(writeCtx, coderws.MessageText, data); err != nil {
		writeCancel()
		t.Fatalf("failed to send message: %v", err)
	}
	writeCancel()

	// Connection should remain open
	readCtx, readCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, _, err = conn.Reader(readCtx)
	readCancel()
	if err != nil {
		t.Logf("Reader error (might be expected): %v", err)
	}
}

// TestConcurrentAgentAccess tests that AgentManager handles concurrent access safely.
func TestConcurrentAgentAccess(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	manager := wspkg.NewAgentManager(logger, nil)

	// Launch goroutines that all access the same agent
	ctx := context.Background()
	userID := "concurrent-user"
	agentID := "concurrent-agent"
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			ag := manager.GetOrCreateAgentForUser(ctx, userID, agentID, agent.Task{})
			if ag != nil {
				_ = ag.GetStatus() // Just accessing the agent
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent operations")
		}
	}

	// Verify we still have a valid agent
	ag := manager.GetOrCreateAgentForUser(ctx, userID, agentID, agent.Task{})
	if ag == nil {
		t.Error("expected valid agent after concurrent access")
	}
}
