package websocket

import (
	"context"
	"sync"

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

// AgentLane holds the channel coordination and agent reference.
type AgentLane struct {
	Once   sync.Once
	LaneCh chan agent.Message // message channel
	Done   chan struct{}      // closed when agent exits
	Agent  *agent.UserAgent
}

const (
	defaultLaneQueueSize = 100
)

// AgentManager manages per-user UserAgents with thread-safe access.
//
// # One Connection Per Agent
// Each agent lane has its own WebSocket connection — no multiplexing.
// Agents stream independently; a slow or dead agent doesn't block others.
//
// # Persistence Across Reconnects
// Agents survive disconnections. On reconnect, Submit updates the connection
// on the existing agent's WSConnWrapper atomically.
//
// # Thread Safety
// sync.Map provides lock-free reads. sync.Once ensures each agent is
// initialized exactly once per lane.
type AgentManager struct {
	lanes          sync.Map // map[AgentKey]*AgentLane
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

// SubmitMsg sends a message to an agent, creating it if needed.
// Returns false if the agent has exited.
func (m *AgentManager) SubmitMsg(ctx context.Context, userID, agentID string, conn *wsconn.Wrapper, msg agent.Message) bool {
	key := AgentKey{UserID: userID, AgentID: agentID}

	// Get or create lane
	lane := &AgentLane{}
	if actual, ok := m.lanes.LoadOrStore(key, lane); ok {
		lane = actual.(*AgentLane)
	}

	// Update conn for existing agent (handles reconnect)
	if lane.Agent != nil {
		lane.Agent.UpdateConn(conn.Conn())
	}

	// Initialize lane and agent exactly once
	lane.Once.Do(func() {
		lane.LaneCh = make(chan agent.Message, defaultLaneQueueSize)
		lane.Done = make(chan struct{})
		lane.Agent = agent.NewUserAgent(ctx, agent.UserAgentOptions{
			AgentID:        agentID,
			UserID:         userID,
			Conn:           conn,
			QuotaChatModel: m.quotaChatModel,
			LaneCh:         lane.LaneCh,
			OnExit: func() {
				close(lane.Done)
				m.lanes.Delete(key)
			},
		})
	})

	// Send the message
	select {
	case <-lane.Done:
		return false
	case lane.LaneCh <- msg:
		return true
	}
}

// Get returns the agent for (userID, agentID) or nil if not found.
func (m *AgentManager) Get(userID, agentID string) *agent.UserAgent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	if val, ok := m.lanes.Load(key); ok {
		return val.(*AgentLane).Agent
	}
	return nil
}

// Remove stops and removes the agent for (userID, agentID).
func (m *AgentManager) Remove(userID, agentID string) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	if val, ok := m.lanes.LoadAndDelete(key); ok {
		lane := val.(*AgentLane)
		if lane.Agent != nil {
			lane.Agent.Stop()
		}
	}
}
