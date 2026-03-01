package websocket

import (
	"context"
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
	QuotaChatModel agent.QuotaChatModelInterface
}

// AgentManager manages per-user ADKAgents with thread-safe access.
// Agents are keyed by composite (userID, agentID) to support multiple agents per user.
type AgentManager struct {
	mu             sync.Mutex
	agents         map[AgentKey]*agent.ADKAgent
	logger         zerolog.Logger
	quotaChatModel agent.QuotaChatModelInterface
}

// NewAgentManager creates a new AgentManager.
func NewAgentManager(logger zerolog.Logger, opts *AgentManagerOptions) *AgentManager {
	var quotaChatModel agent.QuotaChatModelInterface
	if opts != nil {
		quotaChatModel = opts.QuotaChatModel
	}

	return &AgentManager{
		agents:         make(map[AgentKey]*agent.ADKAgent),
		logger:         logger,
		quotaChatModel: quotaChatModel,
	}
}

// GetOrCreateAgentForUser returns an existing agent for (userID, agentID) pair or creates a new one.
func (m *AgentManager) GetOrCreateAgentForUser(ctx context.Context, userID, agentID string, firstTask agent.Task) *agent.ADKAgent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()

	existingAgent, exists := m.agents[key]
	if exists {
		m.mu.Unlock()
		return existingAgent
	}

	newAgent := agent.NewADKAgent(ctx, firstTask, agent.ADKAgentOptions{
		AgentID: agentID,
		UserID:  userID,
	})
	m.agents[key] = newAgent
	m.mu.Unlock()

	config := m.loadAgentConfig(userID, agentID)
	_ = newAgent.Start(agent.ADKAgentOptions{
		SystemPrompt:   config.SystemPrompt,
		QuotaChatModel: m.quotaChatModel,
		Memories:       m.loadMemories(userID),
	})

	return newAgent
}

// GetAgent returns an Agent interface for the given (userID, agentID) pair.
func (m *AgentManager) GetAgent(ctx context.Context, userID, agentID string, conn *WSConnWrapper, task agent.Task) Agent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()

	existingAgent, exists := m.agents[key]
	if exists {
		existingAgent.Submit(task)
		m.mu.Unlock()
		return &adkAgentAdapter{agent: existingAgent, conn: conn, agentID: agentID}
	}

	newAgent := agent.NewADKAgent(ctx, task, agent.ADKAgentOptions{
		AgentID: agentID,
		UserID:  userID,
	})
	m.agents[key] = newAgent
	m.mu.Unlock()

	config := m.loadAgentConfig(userID, agentID)
	_ = newAgent.Start(agent.ADKAgentOptions{
		SystemPrompt:   config.SystemPrompt,
		QuotaChatModel: m.quotaChatModel,
		Memories:       m.loadMemories(userID),
	})

	return &adkAgentAdapter{agent: newAgent, conn: conn, agentID: agentID}
}

type adkAgentAdapter struct {
	agent   *agent.ADKAgent
	conn    *WSConnWrapper
	agentID string
}

func (a *adkAgentAdapter) Submit(task agent.Task) {
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
	a.agent.Submit(task)
}

func (a *adkAgentAdapter) Stop() {
	a.agent.Stop()
}

// agentConfig holds configuration for an agent
type agentConfig struct {
	SystemPrompt string
}

// loadAgentConfig loads agent configuration
func (m *AgentManager) loadAgentConfig(userID, agentID string) agentConfig {
	return agentConfig{
		SystemPrompt: "You are a helpful assistant.",
	}
}

// loadMemories loads user memories
func (m *AgentManager) loadMemories(userID string) []string {
	return []string{}
}

// RemoveAgentForUser removes agent for given (userID, agentID) pair.
func (m *AgentManager) RemoveAgentForUser(userID, agentID string) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	defer m.mu.Unlock()

	if existingAgent, exists := m.agents[key]; exists {
		existingAgent.Stop()
		delete(m.agents, key)
	}
}

// GetAgentStatus returns the status of an agent if it exists.
func (m *AgentManager) GetAgentStatus(userID, agentID string) map[string]interface{} {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	ag := m.agents[key]
	m.mu.Unlock()

	if ag == nil {
		return map[string]interface{}{"status": "not_found"}
	}
	return map[string]interface{}{"status": "active", "agent_id": agentID}
}

// GetExistingAgent returns an existing agent without creating a new one.
func (m *AgentManager) GetExistingAgent(userID, agentID string) *agent.ADKAgent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.agents[key]
}
