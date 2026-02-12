package websocket

import (
	"sync"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/proxy"
	"github.com/rs/zerolog"
)

// AgentManagerOptions configures a new AgentManager.
type AgentManagerOptions struct {
	ProxyHandler *proxy.Handler
}

// AgentManager manages per-connection agents with thread-safe access.
type AgentManager struct {
	mu           sync.RWMutex
	agents       map[string]*agent.Agent
	logger       zerolog.Logger
	proxyHandler *proxy.Handler
}

// NewAgentManager creates a new AgentManager.
func NewAgentManager(logger zerolog.Logger, opts *AgentManagerOptions) *AgentManager {
	var proxyHandler *proxy.Handler
	if opts != nil {
		proxyHandler = opts.ProxyHandler
	}

	return &AgentManager{
		agents:       make(map[string]*agent.Agent),
		logger:       logger,
		proxyHandler: proxyHandler,
	}
}

// GetOrCreateAgent returns an existing agent for the session or creates a new one.
// If a proxy handler is configured, the agent will use proxy.Client for LLM calls.
func (m *AgentManager) GetOrCreateAgent(sessionID string, userID string) *agent.Agent {
	m.mu.RLock()
	existingAgent, exists := m.agents[sessionID]
	m.mu.RUnlock()

	if exists {
		return existingAgent
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if existingAgent, exists := m.agents[sessionID]; exists {
		return existingAgent
	}

	// Create new agent with appropriate LLM client
	newAgent := m.createAgent(sessionID, userID)
	m.agents[sessionID] = newAgent

	return newAgent
}

// createAgent creates a new agent with the appropriate LLM client.
func (m *AgentManager) createAgent(sessionID string, userID string) *agent.Agent {
	opts := agent.AgentOptions{
		SessionID: sessionID,
	}

	// If proxy handler is configured, create proxy client for this user
	if m.proxyHandler != nil {
		proxyClient := proxy.NewClient(m.proxyHandler, userID)
		opts.LLMClient = proxyClient
	} else {
		// Use mock client for backward compatibility/testing
		opts.LLMClient = &agent.MockLLMClient{}
	}

	return agent.NewAgent(opts)
}

// RemoveAgent removes the agent for the given session.
func (m *AgentManager) RemoveAgent(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existingAgent, exists := m.agents[sessionID]; exists {
		existingAgent.Unsubscribe(sessionID) // Unsubscribe any event channels
		delete(m.agents, sessionID)
	}
}
