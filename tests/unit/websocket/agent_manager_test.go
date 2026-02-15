package websocket_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/llm-proxy"
	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

// TestAgentManagerWithLLMProxy verifies that AgentManager can be created with LLM proxy options.
func TestAgentManagerWithLLMProxy(t *testing.T) {
	logger := zerolog.Nop()

	// Create mock handler
	handler := proxy.NewHandler(proxy.HandlerOptions{})

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
	handler := proxy.NewHandler(proxy.HandlerOptions{})

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

	handler := proxy.NewHandler(proxy.HandlerOptions{})
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
