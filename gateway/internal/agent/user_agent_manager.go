package agent

import (
	"context"
	"sync"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// UserAgentManagerOptions configures a new UserAgentManager.
type UserAgentManagerOptions struct {
}

// UserAgentManager manages agent lanes with thread-safe access.
// Uses sync.Map for lock-free reads.
type UserAgentManager struct {
	lanes  sync.Map // map[agentID]*agentLane
	logger zerolog.Logger
}

// NewUserAgentManager creates a new UserAgentManager.
func NewUserAgentManager(logger zerolog.Logger, opts *UserAgentManagerOptions) *UserAgentManager {
	return &UserAgentManager{
		logger: logger,
	}
}

const (
	defaultMsgQueueSize = 20
)

// agentLane holds the channel coordination and agent reference.
// sync.Once ensures the lane and agent are initialized exactly once.
type agentLane struct {
	once    sync.Once
	msgCh   chan Message   // message channel
	done    chan struct{}  // closed when agent exits
	agentWg sync.WaitGroup // waits for agent to be initialized
	agent   *UserAgent
}

// SubmitMsg sends a message to an agent, creating it if needed.
// Also adds the connection to the agent's broadcaster for streaming.
// Returns false if the agent has exited.
func (m *UserAgentManager) SubmitMsg(ctx context.Context, agentID, connID string, conn *websocket.Conn, msg Message) bool {
	key := agentID

	// Get or create lane
	lane := &agentLane{}
	if actual, ok := m.lanes.LoadOrStore(key, lane); ok {
		lane = actual.(*agentLane)
	}

	// Initialize lane and agent exactly once
	lane.once.Do(func() {
		lane.msgCh = make(chan Message, defaultMsgQueueSize)
		lane.done = make(chan struct{})

		lane.agentWg.Go(func() {
			lane.agent = newUserAgent(ctx, UserAgentOptions{
				AgentID: agentID,
				Model:   msg.Model,
				MsgCh:   lane.msgCh,
				Logger:  m.logger,
				OnExit: func() {
					close(lane.done)
					m.lanes.Delete(key)
				},
			})
		})
	})

	// once.Do returns as soon as the goroutine is launched, not when it finishes.
	// All callers block here until lane.agent is initialized.
	lane.agentWg.Wait()

	// Add connection to agent's broadcaster and update model
	lane.agent.addConn(connID, conn)
	lane.agent.updateModel(msg.Model)

	// Send the message (also checks if agent exited)
	select {
	case <-lane.done:
		return false
	case lane.msgCh <- msg:
		return true
	}
}

// Get returns the agent for agentID or nil if not found.
func (m *UserAgentManager) Get(agentID string) *UserAgent {
	key := agentID

	if val, ok := m.lanes.Load(key); ok {
		return val.(*agentLane).agent
	}
	return nil
}
