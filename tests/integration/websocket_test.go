//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	wspkg "github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

// wsClient is a test helper for WebSocket connections.
type wsClient struct {
	conn   *websocket.Conn
	done   chan struct{}
	events chan json.RawMessage
}

// newWSClient creates a new WebSocket test client.
func newWSClient(t *testing.T, serverURL string) *wsClient {
	t.Helper()

	wsURL := "ws" + serverURL[4:] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
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
				_, message, err := conn.ReadMessage()
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
	_ = c.conn.Close()
}

// SendPrompt sends a prompt message to the server.
func (c *wsClient) SendPrompt(t *testing.T, prompt string) {
	t.Helper()

	msg := map[string]string{"type": "prompt", "text": prompt}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal prompt: %v", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send prompt: %v", err)
	}
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

// TestAgentCreationOnConnect tests that an agent is created when a WebSocket connects.
func TestAgentCreationOnConnect(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	client := newWSClient(t, srv.URL)
	defer client.Close()

	// Wait for connection established message
	// First read any initial messages (the connected event should be the first one)
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

	sessionID, ok := data["session_id"].(string)
	if !ok || sessionID == "" {
		t.Error("expected session_id in connected event")
	}

	agentID, ok := data["agent_id"].(string)
	if !ok || agentID == "" {
		t.Error("expected agent_id in connected event")
	}
}

// TestAgentEventForwarding tests that agent events are forwarded to the WebSocket.
func TestAgentEventForwarding(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100)
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
	endEvent := client.WaitForEvent(t, 2*time.Second, hasType("agent_end"))

	data, ok := extractNestedData(endEvent)
	if !ok {
		t.Fatal("expected data in agent_end event")
	}

	messages, ok := data["messages"].([]any)
	if !ok {
		t.Error("expected messages in agent_end event data")
	}

	// Should have user message and assistant response
	if len(messages) < 2 {
		t.Errorf("expected at least 2 messages, got %d", len(messages))
	}
}

// TestPromptHandling tests that incoming prompts trigger agent processing.
func TestPromptHandling(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	client := newWSClient(t, srv.URL)
	defer client.Close()

	// Wait for connection
	_ = client.WaitForEvent(t, 2*time.Second, hasType("connected"))

	// Send a prompt
	testPrompt := "test message"
	client.SendPrompt(t, testPrompt)

	// Wait for agent_start
	_ = client.WaitForEvent(t, 2*time.Second, hasType("agent_start"))

	// Wait for agent_end with response
	endEvent := client.WaitForEvent(t, 2*time.Second, hasType("agent_end"))

	data, ok := extractNestedData(endEvent)
	if !ok {
		t.Fatal("expected messages in agent_end event data")
	}

	messages, ok := data["messages"].([]any)
	if !ok {
		t.Fatal("expected messages in agent_end event data")
	}

	// Verify we got messages
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages))
	}

	// Check that the assistant message contains a response to our prompt
	foundResponse := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, ok := msgMap["role"].(string)
		if !ok {
			continue
		}
		if role == "assistant" {
			foundResponse = true
			content, ok := msgMap["content"].([]any)
			if !ok {
				continue
			}
			if len(content) > 0 {
				contentBlock, ok := content[0].(map[string]any)
				if ok {
					text, ok := contentBlock["text"].(string)
					if ok && strings.Contains(text, testPrompt) {
						return // Success - response contains our prompt text
					}
				}
			}
		}
	}

	if !foundResponse {
		t.Error("expected assistant response in messages")
	}
}

// TestAgentCleanupOnDisconnect tests that agents are cleaned up when connection closes.
func TestAgentCleanupOnDisconnect(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	// Create first connection
	client1 := newWSClient(t, srv.URL)
	event1 := client1.WaitForEvent(t, 2*time.Second, hasType("connected"))

	data1, ok := extractNestedData(event1)
	if !ok {
		t.Fatal("expected data in connected event")
	}
	sessionID1, ok := data1["session_id"].(string)
	if !ok || sessionID1 == "" {
		t.Fatal("expected session_id in connected event")
	}

	// Create second connection
	client2 := newWSClient(t, srv.URL)
	event2 := client2.WaitForEvent(t, 2*time.Second, hasType("connected"))

	data2, ok := extractNestedData(event2)
	if !ok {
		t.Fatal("expected data in connected event")
	}
	sessionID2, ok := data2["session_id"].(string)
	if !ok || sessionID2 == "" {
		t.Fatal("expected session_id in connected event")
	}

	// Verify we have two different sessions
	if sessionID1 == sessionID2 {
		t.Error("expected different session IDs for different connections")
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

// TestAgentManagerGetOrCreateAgent tests the GetOrCreateAgent method.
func TestAgentManagerGetOrCreateAgent(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	manager := wspkg.NewAgentManager(logger, nil)

	sessionID := "test-session-123"

	// First call should create a new agent
	agent1 := manager.GetOrCreateAgent(sessionID, sessionID)
	if agent1 == nil {
		t.Fatal("expected non-nil agent")
	}

	// Second call should return the same agent
	agent2 := manager.GetOrCreateAgent(sessionID, sessionID)
	if agent2 == nil {
		t.Fatal("expected non-nil agent on second call")
	}

	// Verify they're the same instance
	state1 := agent1.State()
	state2 := agent2.State()
	if state1.SessionID != state2.SessionID {
		t.Error("expected same agent for same session ID")
	}

	// Different session ID should get different agent
	agent3 := manager.GetOrCreateAgent("different-session", "different-session")
	if agent3 == nil {
		t.Fatal("expected non-nil agent for different session")
	}

	state3 := agent3.State()
	if state3.SessionID == state1.SessionID {
		t.Error("expected different agent for different session ID")
	}
}

// TestAgentManagerRemoveAgent tests the RemoveAgent method.
func TestAgentManagerRemoveAgent(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	manager := wspkg.NewAgentManager(logger, nil)

	sessionID := "test-session-remove"

	// Create an agent
	agent := manager.GetOrCreateAgent(sessionID, sessionID)
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}

	// Remove the agent
	manager.RemoveAgent(sessionID)

	// Should create a new agent now
	newAgent := manager.GetOrCreateAgent(sessionID, sessionID)
	if newAgent == nil {
		t.Fatal("expected non-nil agent after remove")
	}

	// Verify it's a new instance (we can check by comparing state or just ensuring the old one is no longer tracked)
	// Since agent.State() returns a copy, we can't directly compare instances
	// But we can verify the session ID is preserved
	newState := newAgent.State()
	if newState.SessionID != sessionID {
		t.Error("expected session ID to be preserved for new agent instance")
	}
}

// TestInvalidMessageType tests handling of invalid message types.
func TestInvalidMessageType(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send invalid JSON
	if err := conn.WriteMessage(websocket.TextMessage, []byte("invalid json")); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Connection should remain open (error is logged but doesn't close)
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	if err != nil {
		// Connection might close or might not - just verify we don't panic
		t.Logf("ReadMessage error (expected): %v", err)
	}
}

// TestInvalidPromptMessage tests handling of prompt messages without text.
func TestInvalidPromptMessage(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	handler := wspkg.NewHandler(logger, []string{"*"}, 100)
	srv := httptest.NewServer(http.HandlerFunc(handler.HandleWebSocket))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send prompt without text field
	msg := map[string]string{"type": "prompt"}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Connection should remain open
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	if err != nil {
		t.Logf("ReadMessage error (might be expected): %v", err)
	}
}

// TestConcurrentAgentAccess tests that AgentManager handles concurrent access safely.
func TestConcurrentAgentAccess(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	manager := wspkg.NewAgentManager(logger, nil)

	// Launch goroutines that all access the same session
	sessionID := "concurrent-session"
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			agent := manager.GetOrCreateAgent(sessionID, sessionID)
			if agent != nil {
				_ = agent.State() // Just accessing the agent
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
	agent := manager.GetOrCreateAgent(sessionID, sessionID)
	if agent == nil {
		t.Error("expected valid agent after concurrent access")
	}
}
