package websocket

import (
	"sync"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/rs/zerolog"
)

type Agent interface {
	Submit(task agent.Task)
	Stop()
}

// AgentKey is a composite key for agent lookup: (userID, agentID)
type AgentKey struct {
	UserID  string
	AgentID string
}

// AgentManagerOptions configures a new AgentManager.
type AgentManagerOptions struct {
}

// AgentManager manages per-user agents with thread-safe access.
// Agents are keyed by composite (userID, agentID) to support multiple agents per user.
type AgentManager struct {
	mu     sync.RWMutex
	agents map[AgentKey]*agent.Agent
	logger zerolog.Logger
}

// NewAgentManager creates a new AgentManager.
func NewAgentManager(logger zerolog.Logger, opts *AgentManagerOptions) *AgentManager {
	return &AgentManager{
		agents: make(map[AgentKey]*agent.Agent),
		logger: logger,
	}
}

// GetOrCreateAgentForUser returns an existing agent for (userID, agentID) pair or creates a new one.
func (m *AgentManager) GetOrCreateAgentForUser(userID, agentID string) *agent.Agent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.RLock()
	existingAgent, exists := m.agents[key]
	m.mu.RUnlock()

	if exists {
		return existingAgent
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if existingAgent, exists := m.agents[key]; exists {
		return existingAgent
	}

	// Create new agent with appropriate LLM client
	newAgent := m.createAgent(userID, agentID)
	m.agents[key] = newAgent

	return newAgent
}

// GetAgent returns an Agent interface for the given (userID, agentID) pair.
// If conn is provided, it wraps it for thread-safe writes through the writeCh.
func (m *AgentManager) GetAgent(userID, agentID string, conn *WSConnWrapper) Agent {
	ag := m.GetOrCreateAgentForUser(userID, agentID)
	return &agentAdapter{agent: ag, conn: conn, agentID: agentID}
}

type agentAdapter struct {
	agent   *agent.Agent
	conn    *WSConnWrapper
	agentID string
}

func (a *agentAdapter) Submit(task agent.Task) {
	if a.conn != nil {
		_ = a.conn.WriteJSON(map[string]interface{}{
			"type":     "agent_start",
			"agent_id": a.agentID,
		})

		_ = a.conn.WriteJSON(map[string]interface{}{
			"type":     "agent_end",
			"agent_id": a.agentID,
		})
	}
	a.agent.Lane().Submit(task.Message)
}

func (a *agentAdapter) Stop() {
}

// createAgent creates a new agent.
func (m *AgentManager) createAgent(userID, agentID string) *agent.Agent {
	opts := agent.AgentOptions{
		ID:        agentID,
		SessionID: userID + ":" + agentID,
	}

	return agent.NewAgent(opts)
}

// RemoveAgentForUser removes agent for given (userID, agentID) pair.
func (m *AgentManager) RemoveAgentForUser(userID, agentID string) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	defer m.mu.Unlock()

	if existingAgent, exists := m.agents[key]; exists {
		existingAgent.Unsubscribe(key.UserID)
		delete(m.agents, key)
	}
}

// GetOrCreateAgent is deprecated - use GetOrCreateAgentForUser instead.
func (m *AgentManager) GetOrCreateAgent(sessionID string, userID string) *agent.Agent {
	return m.GetOrCreateAgentForUser(userID, sessionID)
}

// RemoveAgent is deprecated - use RemoveAgentForUser instead.
// For backward compatibility, this removes any agent matching to sessionID as agentID.
func (m *AgentManager) RemoveAgent(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove any agent that has this sessionID as its agentID
	// This maintains backward compatibility with the old single-key system
	for key, agent := range m.agents {
		if key.AgentID == sessionID {
			agent.Unsubscribe(key.UserID)
			delete(m.agents, key)
			return // Only remove first match (old behavior)
		}
	}
}
