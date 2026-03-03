package websocket_test

import (
	"context"
	"testing"

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

	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn)
	if ag == nil {
		t.Fatal("GetOrAdd returned nil")
	}

	ag2 := mgr.GetOrAdd(context.Background(), userID, agentID, conn)
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

	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn)
	if ag == nil {
		t.Fatal("GetOrAdd returned nil")
	}

	mgr.Remove(userID, agentID)

	// After removal, GetOrAdd should create a new agent
	ag2 := mgr.GetOrAdd(context.Background(), userID, agentID, conn)
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

	ag := mgr.GetOrAdd(context.Background(), userID, agentID, conn)
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

	ag1 := mgr.GetOrAdd(context.Background(), userID, agentID, conn)
	if ag1 == nil {
		t.Fatal("GetOrAdd returned nil")
	}

	ag2 := mgr.GetOrAdd(context.Background(), userID, agentID, conn)
	if ag1 != ag2 {
		t.Error("Expected same agent for same (userID, agentID)")
	}

	ag3 := mgr.GetOrAdd(context.Background(), userID, "agent-789", conn)
	if ag1 == ag3 {
		t.Error("Expected different agent for different agentID")
	}

	ag4 := mgr.GetOrAdd(context.Background(), "user-456", agentID, conn)
	if ag1 == ag4 {
		t.Error("Expected different agent for different userID")
	}
}
