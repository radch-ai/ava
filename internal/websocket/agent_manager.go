package websocket

import (
	"sync"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/rs/zerolog"
)

// AgentManager manages per-connection agents with thread-safe access.
type AgentManager struct {
	mu     sync.RWMutex
	agents map[string]*agent.Agent
	logger zerolog.Logger
}

// NewAgentManager creates a new AgentManager.
func NewAgentManager(logger zerolog.Logger) *AgentManager {
	return &AgentManager{
		agents: make(map[string]*agent.Agent),
		logger: logger,
	}
}

// GetOrCreateAgent returns an existing agent for the session or creates a new one.
func (m *AgentManager) GetOrCreateAgent(sessionID string) *agent.Agent {
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

	newAgent := agent.NewAgent(agent.AgentOptions{
		SessionID: sessionID,
	})
	m.agents[sessionID] = newAgent

	return newAgent
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
