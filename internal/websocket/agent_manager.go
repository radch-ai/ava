package websocket

import (
	"sync"

	"github.com/nevaria-ai/gateway/internal/agent"
	llmproxy "github.com/nevaria-ai/llm-proxy-sdk"
	"github.com/rs/zerolog"
)

// AgentKey is a composite key for agent lookup: (userID, agentID)
type AgentKey struct {
	UserID  string
	AgentID string
}

// AgentManagerOptions configures a new AgentManager.
type AgentManagerOptions struct {
	LLMProxyHandler *llmproxy.Handler
}

// AgentManager manages per-user agents with thread-safe access.
// Agents are keyed by composite (userID, agentID) to support multiple agents per user.
type AgentManager struct {
	mu       sync.RWMutex
	agents   map[AgentKey]*agent.Agent
	logger   zerolog.Logger
	llmProxy *llmproxy.Handler
}

// NewAgentManager creates a new AgentManager.
func NewAgentManager(logger zerolog.Logger, opts *AgentManagerOptions) *AgentManager {
	var llmProxy *llmproxy.Handler
	if opts != nil {
		llmProxy = opts.LLMProxyHandler
	}

	return &AgentManager{
		agents:   make(map[AgentKey]*agent.Agent),
		logger:   logger,
		llmProxy: llmProxy,
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

// createAgent creates a new agent with appropriate LLM client.
func (m *AgentManager) createAgent(userID, agentID string) *agent.Agent {
	opts := agent.AgentOptions{
		ID:        agentID,
		SessionID: userID + ":" + agentID,
	}

	// If LLM proxy handler is configured, create SDK client for this user
	if m.llmProxy != nil {
		sdkClient := NewSDKClient(m.llmProxy, userID)
		opts.LLMClient = sdkClient
	} else {
		// Use mock client for backward compatibility/testing
		opts.LLMClient = &agent.MockLLMClient{}
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
