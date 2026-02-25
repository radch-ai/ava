package websocket_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/websocket"
	llmproxy "github.com/nevaria-ai/llm-proxy-sdk"
	"github.com/nevaria-ai/llm-proxy-sdk/pkg/handler"
	"github.com/rs/zerolog"
)

// TestAgentManagerWithLLMProxy verifies that AgentManager can be created with LLM proxy options.
func TestAgentManagerWithLLMProxy(t *testing.T) {
	logger := zerolog.Nop()

	// Create mock handler
	handler := handler.NewHandler(handler.HandlerOptions{})

	// Create agent manager with LLM proxy handler
	mgr := websocket.NewAgentManager(logger, &websocket.AgentManagerOptions{
		LLMProxyHandler: handler,
	})

	if mgr == nil {
		t.Fatal("NewAgentManager returned nil")
	}
}

// TestGetOrCreateAgentWithLLMProxy verifies that agent creation uses LLM proxy client.
func TestGetOrCreateAgentWithLLMProxy(t *testing.T) {
	logger := zerolog.Nop()

	// Create mock handler
	handler := handler.NewHandler(handler.HandlerOptions{})

	// Create agent manager with LLM proxy handler
	mgr := websocket.NewAgentManager(logger, &websocket.AgentManagerOptions{
		LLMProxyHandler: handler,
	})

	userID := "test-user-123"
	sessionID := "test-session-456"

	// Create or get agent
	agent := mgr.GetOrCreateAgent(sessionID, userID)
	if agent == nil {
		t.Fatal("GetOrCreateAgent returned nil")
	}

	// Verify same agent is returned on second call
	agent2 := mgr.GetOrCreateAgent(sessionID, userID)
	if agent != agent2 {
		t.Error("Expected same agent instance")
	}
}

// TestAgentManagerRemoveAgentWithLLMProxy verifies agent removal with LLM proxy.
func TestAgentManagerRemoveAgentWithLLMProxy(t *testing.T) {
	logger := zerolog.Nop()

	handler := handler.NewHandler(handler.HandlerOptions{})
	mgr := websocket.NewAgentManager(logger, &websocket.AgentManagerOptions{
		LLMProxyHandler: handler,
	})

	sessionID := "test-session-789"
	userID := "test-user-123"

	// Create agent
	agent := mgr.GetOrCreateAgent(sessionID, userID)
	if agent == nil {
		t.Fatal("GetOrCreateAgent returned nil")
	}

	// Remove agent
	mgr.RemoveAgent(sessionID)

	// Verify new agent is created after removal
	agent2 := mgr.GetOrCreateAgent(sessionID, userID)
	if agent == agent2 {
		t.Error("Expected different agent instance after removal")
	}
}

// TestAgentManagerNilLLMProxy verifies behavior when LLM proxy handler is nil.
func TestAgentManagerNilLLMProxy(t *testing.T) {
	logger := zerolog.Nop()

	// Create agent manager without LLM proxy handler (backward compatibility)
	mgr := websocket.NewAgentManager(logger, nil)

	sessionID := "test-session-nil"
	userID := "test-user-nil"

	// Should still create agent (with mock client)
	agent := mgr.GetOrCreateAgent(sessionID, userID)
	if agent == nil {
		t.Fatal("GetOrCreateAgent returned nil")
	}
}

// TestAgentKeyCompositeKey verifies agents are keyed by (userID, agentID)
func TestAgentKeyCompositeKey(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"
	agentID := "agent-456"

	// Create agent with composite key
	agent1 := mgr.GetOrCreateAgentForUser(userID, agentID)
	if agent1 == nil {
		t.Fatal("GetOrCreateAgentForUser returned nil")
	}

	// Same composite key returns same agent
	agent2 := mgr.GetOrCreateAgentForUser(userID, agentID)
	if agent1 != agent2 {
		t.Error("Expected same agent for same (userID, agentID)")
	}

	// Different agentID returns different agent
	agent3 := mgr.GetOrCreateAgentForUser(userID, "agent-789")
	if agent1 == agent3 {
		t.Error("Expected different agent for different agentID")
	}

	// Different userID returns different agent
	agent4 := mgr.GetOrCreateAgentForUser("user-456", agentID)
	if agent1 == agent4 {
		t.Error("Expected different agent for different userID")
	}
}

// TestRemoveAgentForUser verifies agent removal by composite key
func TestRemoveAgentForUser(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"
	agentID := "agent-456"

	// Create agent
	agent1 := mgr.GetOrCreateAgentForUser(userID, agentID)

	// Remove agent
	mgr.RemoveAgentForUser(userID, agentID)

	// Verify new agent is created after removal
	agent2 := mgr.GetOrCreateAgentForUser(userID, agentID)
	if agent1 == agent2 {
		t.Error("Expected different agent after removal")
	}
}
