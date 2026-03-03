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

// agentEntry wraps an agent with coordination for submit vs exit.
type agentEntry struct {
	once   sync.Once
	mu     sync.RWMutex // coordinates submit vs exit
	active bool         // true while agent loop is running
	agent  *agent.UserAgent
}

// AgentManager manages per-user UserAgents with thread-safe access.
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
// sync.Map provides lock-free reads and fine-grained locking per key.
// sync.Once ensures each agent is initialized exactly once.
type AgentManager struct {
	agents         sync.Map // map[AgentKey]*agentEntry
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
		logger:         logger,
		quotaChatModel: quotaChatModel,
	}
}

// GetOrAdd returns an existing agent or creates a new one for (userID, agentID).
// conn is the WebSocket connection for this specific agent lane.
// On reconnect, use UpdateConn instead — this conn is only used at creation.
//
// Deprecated: Use Submit instead to ensure proper coordination with agent exit.
func (m *AgentManager) GetOrAdd(ctx context.Context, userID, agentID string, conn *wsconn.Wrapper) *agent.UserAgent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	// Get or create entry (cheap - just a small struct)
	entry := &agentEntry{}
	if actual, ok := m.agents.LoadOrStore(key, entry); ok {
		entry = actual.(*agentEntry)
	}

	// Initialize exactly once - all callers wait for the same initialization
	entry.once.Do(func() {
		config := m.loadAgentConfig(userID, agentID)
		entry.agent = agent.NewUserAgent(ctx, agent.Message{}, agent.UserAgentOptions{
			AgentID:        agentID,
			UserID:         userID,
			Conn:           conn,
			SystemPrompt:   config.SystemPrompt,
			QuotaChatModel: m.quotaChatModel,
			Memories:       m.loadMemories(userID),
			OnExit: func() {
				entry.mu.Lock()
				entry.active = false
				entry.mu.Unlock()
			},
		})
		entry.active = true
	})

	return entry.agent
}

// Submit sends a message to an agent, creating it if needed.
// It handles coordination between message submission and agent exit,
// ensuring messages are not sent to stopped agents.
func (m *AgentManager) Submit(ctx context.Context, userID, agentID string, conn *wsconn.Wrapper, msg agent.Message) bool {
	key := AgentKey{UserID: userID, AgentID: agentID}

	// Get or create entry
	entry := &agentEntry{}
	if actual, ok := m.agents.LoadOrStore(key, entry); ok {
		entry = actual.(*agentEntry)
	}

	// Initialize exactly once if needed
	entry.once.Do(func() {
		config := m.loadAgentConfig(userID, agentID)
		entry.agent = agent.NewUserAgent(ctx, agent.Message{}, agent.UserAgentOptions{
			AgentID:        agentID,
			UserID:         userID,
			Conn:           conn,
			SystemPrompt:   config.SystemPrompt,
			QuotaChatModel: m.quotaChatModel,
			Memories:       m.loadMemories(userID),
			OnExit: func() {
				entry.mu.Lock()
				entry.active = false
				entry.mu.Unlock()
			},
		})
		entry.active = true
	})

	// Acquire read lock to coordinate with exit
	entry.mu.RLock()
	defer entry.mu.RUnlock()

	if !entry.active {
		return false
	}

	entry.agent.Submit(msg)
	return true
}

// UpdateConn swaps the WebSocket connection for a specific agent lane on reconnect.
// The agent continues running; new writes transparently use the new connection.
func (m *AgentManager) UpdateConn(userID, agentID string, conn *websocket.Conn) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	if val, ok := m.agents.Load(key); ok {
		entry := val.(*agentEntry)
		if entry.agent != nil {
			entry.agent.UpdateConn(conn)
		}
	}
}

// Remove stops and removes the agent for (userID, agentID).
func (m *AgentManager) Remove(userID, agentID string) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	if val, ok := m.agents.LoadAndDelete(key); ok {
		entry := val.(*agentEntry)
		if entry.agent != nil {
			entry.agent.Stop()
		}
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
