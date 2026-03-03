package websocket

import (
	"context"
	"sync"

	"github.com/coder/websocket"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/wsconn"
	"github.com/rs/zerolog"
)

// AgentManagerOptions configures a new AgentManager.
type AgentManagerOptions struct {
	QuotaChatModel agent.QuotaChatModelInterface
}

// AgentKey is a composite key for agent lookup: (userID, agentID).
// Prevents agent ID collisions across users.
type AgentKey struct {
	UserID  string
	AgentID string
}

// AgentManager manages per-user ADKAgents with thread-safe access.
//
// # One Connection Per Agent
// Each agent lane has its own WebSocket connection — no multiplexing.
// Agents stream independently; a slow or dead agent doesn't block others.
//
// # Persistence Across Reconnects
// Agents survive disconnections. On reconnect, call UpdateConn to swap
// the connection on the existing agent's WSConnWrapper atomically.
//
// # Thread Safety
// sync.Mutex with lock-then-check prevents races during agent creation.
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

// GetOrAdd returns an existing agent or creates a new one for (userID, agentID).
// conn is the WebSocket connection for this specific agent lane.
// On reconnect, use UpdateConn instead — this conn is only used at creation.
func (m *AgentManager) GetOrAdd(ctx context.Context, userID, agentID string, conn *wsconn.Wrapper) *agent.ADKAgent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	if ag, exists := m.agents[key]; exists {
		m.mu.Unlock()
		return ag
	}

	ag := agent.NewADKAgent(ctx, agent.Task{}, agent.ADKAgentOptions{
		AgentID: agentID,
		UserID:  userID,
		Conn:    conn,
	})
	m.agents[key] = ag
	m.mu.Unlock()

	config := m.loadAgentConfig(userID, agentID)
	_ = ag.StartWithModel(agent.ADKAgentOptions{
		SystemPrompt:   config.SystemPrompt,
		QuotaChatModel: m.quotaChatModel,
		Memories:       m.loadMemories(userID),
	})

	return ag
}

// UpdateConn swaps the WebSocket connection for a specific agent lane on reconnect.
// The agent continues running; new writes transparently use the new connection.
func (m *AgentManager) UpdateConn(userID, agentID string, conn *websocket.Conn) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	ag := m.agents[key]
	m.mu.Unlock()

	if ag != nil {
		ag.UpdateConn(conn)
	}
}

// Remove stops and removes the agent for (userID, agentID).
func (m *AgentManager) Remove(userID, agentID string) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	defer m.mu.Unlock()

	if ag, exists := m.agents[key]; exists {
		ag.Stop()
		delete(m.agents, key)
	}
}

// agentConfig holds configuration for an agent.
type agentConfig struct {
	SystemPrompt string
}

// loadAgentConfig loads agent configuration (placeholder).
func (m *AgentManager) loadAgentConfig(userID, agentID string) agentConfig {
	return agentConfig{SystemPrompt: "You are a helpful assistant."}
}

// loadMemories loads user memories (placeholder).
func (m *AgentManager) loadMemories(userID string) []string {
	return []string{}
}
