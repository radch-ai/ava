package websocket

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/llm-proxy"
	"github.com/rs/zerolog"
)

// TestAgentManagerWithProxy verifies that AgentManager can be created with proxy options.
func TestAgentManagerWithProxy(t *testing.T) {
	logger := zerolog.Nop()

	// Create mock handler
	handler := proxy.NewHandler(proxy.HandlerOptions{})

	// Create agent manager with proxy handler
	mgr := NewAgentManager(logger, &AgentManagerOptions{
		ProxyHandler: handler,
	})

	if mgr == nil {
		t.Fatal("NewAgentManager returned nil")
	}
}

// TestGetOrCreateAgentWithProxy verifies that agent creation uses proxy client.
func TestGetOrCreateAgentWithProxy(t *testing.T) {
	logger := zerolog.Nop()

	// Create mock handler
	handler := proxy.NewHandler(proxy.HandlerOptions{})

	// Create agent manager with proxy handler
	mgr := NewAgentManager(logger, &AgentManagerOptions{
		ProxyHandler: handler,
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

// TestAgentManagerRemoveAgentWithProxy verifies agent removal with proxy.
func TestAgentManagerRemoveAgentWithProxy(t *testing.T) {
	logger := zerolog.Nop()

	handler := proxy.NewHandler(proxy.HandlerOptions{})
	mgr := NewAgentManager(logger, &AgentManagerOptions{
		ProxyHandler: handler,
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

// TestAgentManagerNilProxy verifies behavior when proxy handler is nil.
func TestAgentManagerNilProxy(t *testing.T) {
	logger := zerolog.Nop()

	// Create agent manager without proxy handler (backward compatibility)
	mgr := NewAgentManager(logger, nil)

	sessionID := "test-session-nil"
	userID := "test-user-nil"

	// Should still create agent (with mock client)
	agent := mgr.GetOrCreateAgent(sessionID, userID)
	if agent == nil {
		t.Fatal("GetOrCreateAgent returned nil")
	}
}
