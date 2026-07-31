package agent

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radch-ai/ava/services/gateway/internal/wsconn"
	"github.com/rs/zerolog"
)

// UserAgentManagerOptions configures a new UserAgentManager.
type UserAgentManagerOptions struct {
	BifrostBaseURL string        // Bifrost gateway URL (from BIFROST_BASEURL env)
	DBPool         *pgxpool.Pool // Database pool for loading user config
}

// laneKey is a composite key for agent lookup: (userID, agentID).
// Prevents agent ID collisions across users.
type laneKey struct {
	UserID  string
	AgentID string
}

// UserAgentManager manages per-user UserAgents with thread-safe access.
// Uses sync.Map for lock-free reads of agent lanes.
type UserAgentManager struct {
	lanes          sync.Map // map[laneKey]*agentLane
	logger         zerolog.Logger
	bifrostBaseURL string
	dbPool         *pgxpool.Pool
}

// NewUserAgentManager creates a new UserAgentManager.
func NewUserAgentManager(logger zerolog.Logger, opts *UserAgentManagerOptions) *UserAgentManager {
	return &UserAgentManager{
		logger:         logger,
		bifrostBaseURL: opts.BifrostBaseURL,
		dbPool:         opts.DBPool,
	}
}

const (
	defaultMsgQueueSize = 20
)

// agentLane holds the channel coordination and agent reference.
// sync.Once ensures the lane and agent are initialized exactly once.
type agentLane struct {
	once  sync.Once
	msgCh chan Message  // message channel
	done  chan struct{} // closed when agent exits
	agent *UserAgent
}

// SubmitMsg sends a message to an agent, creating it if needed.
// Returns false if the agent has exited.
//
// # One Connection Per Agent
// Each agent lane has its own WebSocket connection — no multiplexing.
// Agents stream independently; a slow or dead agent doesn't block others.
//
// # Persistence Across Reconnects
// Agents survive disconnections. On reconnect, the connection is updated
// on the existing agent's WSConnWrapper atomically via updateStats.
func (m *UserAgentManager) SubmitMsg(ctx context.Context, userID, agentID string, conn *wsconn.Wrapper, msg Message) bool {
	key := laneKey{UserID: userID, AgentID: agentID}

	// Get or create lane
	lane := &agentLane{}
	if actual, ok := m.lanes.LoadOrStore(key, lane); ok {
		lane = actual.(*agentLane)

		// Update stats for existing agent (handles reconnect + model change)
		if lane.agent != nil {
			if err := lane.agent.updateStats(ctx, conn.Conn(), msg.Model); err != nil {
				m.logger.Warn().Err(err).Msg("failed to update stats")
			}
		}
	}

	// Initialize lane and agent exactly once
	lane.once.Do(func() {
		lane.msgCh = make(chan Message, defaultMsgQueueSize)
		lane.done = make(chan struct{})

		lane.agent = newUserAgent(ctx, UserAgentOptions{
			AgentID:        agentID,
			UserID:         userID,
			Conn:           conn,
			Model:          msg.Model,
			MsgCh:          lane.msgCh,
			BifrostBaseURL: m.bifrostBaseURL,
			DBPool:         m.dbPool,
			Logger:         m.logger,
			OnExit: func() {
				close(lane.done)
				m.lanes.Delete(key)
			},
		})
	})

	// Send the message
	select {
	case <-lane.done:
		return false
	case lane.msgCh <- msg:
		return true
	}
}

// Get returns the agent for (userID, agentID) or nil if not found.
func (m *UserAgentManager) Get(userID, agentID string) *UserAgent {
	key := laneKey{UserID: userID, AgentID: agentID}

	if val, ok := m.lanes.Load(key); ok {
		return val.(*agentLane).agent
	}
	return nil
}
