package websocket_test

import (
	"context"
	"testing"

	agentpkg "github.com/nevaria-ai/gateway/internal/agent"

	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

func TestAgentManagerCreation(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	if mgr == nil {
		t.Fatal("NewAgentManager returned nil")
	}
}

func TestGetOrCreateAgentForUser(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"

	ag := mgr.GetOrCreateAgentForUser(context.Background(), userID, agentID, agentpkg.Task{})
	if ag == nil {
		t.Fatal("GetOrCreateAgentForUser returned nil")
	}

	ag2 := mgr.GetOrCreateAgentForUser(context.Background(), userID, agentID, agentpkg.Task{})
	if ag != ag2 {
		t.Error("Expected same agent instance")
	}
}

func TestRemoveAgentForUser(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"

	ag := mgr.GetOrCreateAgentForUser(context.Background(), userID, agentID, agentpkg.Task{})
	if ag == nil {
		t.Fatal("GetOrCreateAgentForUser returned nil")
	}

	mgr.RemoveAgentForUser(userID, agentID)

	ag2 := mgr.GetOrCreateAgentForUser(context.Background(), userID, agentID, agentpkg.Task{})
	if ag == ag2 {
		t.Error("Expected different agent instance after removal")
	}
}

func TestGetAgent(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"

	ag := mgr.GetAgent(context.Background(), userID, agentID, nil, agentpkg.Task{})
	if ag == nil {
		t.Fatal("GetAgent returned nil")
	}
}

func TestAgentKeyCompositeKey(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"
	agentID := "agent-456"

	ag1 := mgr.GetOrCreateAgentForUser(context.Background(), userID, agentID, agentpkg.Task{})
	if ag1 == nil {
		t.Fatal("GetOrCreateAgentForUser returned nil")
	}

	ag2 := mgr.GetOrCreateAgentForUser(context.Background(), userID, agentID, agentpkg.Task{})
	if ag1 != ag2 {
		t.Error("Expected same agent for same (userID, agentID)")
	}

	ag3 := mgr.GetOrCreateAgentForUser(context.Background(), userID, "agent-789", agentpkg.Task{})
	if ag1 == ag3 {
		t.Error("Expected different agent for different agentID")
	}

	ag4 := mgr.GetOrCreateAgentForUser(context.Background(), "user-456", agentID, agentpkg.Task{})
	if ag1 == ag4 {
		t.Error("Expected different agent for different userID")
	}
}
