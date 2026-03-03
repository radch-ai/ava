package websocket_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/nevaria-ai/gateway/internal/wsconn"
	"github.com/rs/zerolog"
)

func TestAgentManagerCreation(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	if mgr == nil {
		t.Fatal("NewAgentManager returned nil")
	}
}

func TestGetOrAdd(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"
	conn := wsconn.NewWrapper(nil)

	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag == nil {
		t.Fatal("GetOrAdd returned nil")
	}

	ag2 := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag != ag2 {
		t.Error("Expected same agent instance")
	}
}

func TestRemove(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"
	conn := wsconn.NewWrapper(nil)

	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag == nil {
		t.Fatal("GetOrAdd returned nil")
	}

	mgr.Remove(userID, agentID)

	// After removal, GetOrAdd should create a new agent
	ag2 := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag == ag2 {
		t.Error("Expected different agent instance after removal")
	}
}

func TestUpdateConn(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"
	conn := wsconn.NewWrapper(nil)

	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag == nil {
		t.Fatal("GetOrAdd returned nil")
	}

	// UpdateConn should not panic with nil
	mgr.UpdateConn(userID, agentID, nil)

	// UpdateConn on non-existent agent should not panic
	mgr.UpdateConn("nonexistent", "nonexistent", nil)
}

func TestAgentKeyCompositeKey(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"
	agentID := "agent-456"
	conn := wsconn.NewWrapper(nil)

	ag1 := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag1 == nil {
		t.Fatal("GetOrAdd returned nil")
	}

	ag2 := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag1 != ag2 {
		t.Error("Expected same agent for same (userID, agentID)")
	}

	ag3 := mgr.GetOrAdd(context.Background(), userID, "agent-789", conn) //nolint:staticcheck // Testing deprecated method
	if ag1 == ag3 {
		t.Error("Expected different agent for different agentID")
	}

	ag4 := mgr.GetOrAdd(context.Background(), "user-456", agentID, conn) //nolint:staticcheck // Testing deprecated method
	if ag1 == ag4 {
		t.Error("Expected different agent for different userID")
	}
}

func TestSubmit(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-submit"
	agentID := "test-agent-submit"
	conn := wsconn.NewWrapper(nil)

	msg := agent.Message{Content: "test message", ID: "msg-1"}

	ok := mgr.Submit(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Error("Expected Submit to return true for active agent")
	}
}

func TestSubmitAfterStop(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-stop"
	agentID := "test-agent-stop"
	conn := wsconn.NewWrapper(nil)

	// Create agent via Submit
	msg := agent.Message{Content: "first message", ID: "msg-1"}
	ok := mgr.Submit(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Fatal("Expected first Submit to return true")
	}

	// Get the agent and stop it
	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Need to get agent to stop it
	if ag == nil {
		t.Fatal("GetOrAdd returned nil")
	}
	ag.Stop()

	// Wait for exit to propagate
	time.Sleep(50 * time.Millisecond)

	// Submit should return false because agent was stopped
	msg2 := agent.Message{Content: "after stop", ID: "msg-2"}
	ok = mgr.Submit(context.Background(), userID, agentID, conn, msg2)
	if ok {
		t.Error("Expected Submit to return false for stopped agent")
	}
}

func TestSubmitCreatesAgent(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-create"
	agentID := "test-agent-create"
	conn := wsconn.NewWrapper(nil)

	// Submit without prior GetOrAdd should create agent
	msg := agent.Message{Content: "create and submit", ID: "msg-3"}
	ok := mgr.Submit(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Error("Expected Submit to create agent and return true")
	}

	// Verify agent was created
	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn) //nolint:staticcheck // Verifying agent creation
	if ag == nil {
		t.Error("Expected agent to be created by Submit")
	}
}
