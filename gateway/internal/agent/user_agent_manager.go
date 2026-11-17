package agent

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// UserAgentManagerOptions configures a new UserAgentManager.
type UserAgentManagerOptions struct {
	// ResolveModel maps a selection to a callable endpoint for
	// every lane; bound by the gateway at boot.
	ResolveModel ModelResolver
	// PersistTurn appends turn messages to the thread for every
	// lane; bound by the gateway at boot.
	PersistTurn TurnSink
	// GetAgentConfig reads an agent's config for each lane's
	// prompts; bound by the gateway at boot.
	GetAgentConfig AgentConfigSource
	// Tools fetches each lane's effective tools per turn —
	// permissions applied; nil leaves lanes tool-less.
	Tools AgentToolsFunc
}

// UserAgentManager manages agent lanes with thread-safe access.
// Uses sync.Map for lock-free reads.
type UserAgentManager struct {
	lanes          sync.Map // map[agentID]*agentLane
	logger         zerolog.Logger
	resolveModel   ModelResolver
	persistTurn    TurnSink
	getAgentConfig AgentConfigSource
	tools          AgentToolsFunc
}

// NewUserAgentManager creates a new UserAgentManager.
func NewUserAgentManager(logger zerolog.Logger, opts *UserAgentManagerOptions) *UserAgentManager {
	m := &UserAgentManager{
		logger: logger,
	}
	if opts != nil {
		m.resolveModel = opts.ResolveModel
		m.persistTurn = opts.PersistTurn
		m.getAgentConfig = opts.GetAgentConfig
		m.tools = opts.Tools
	}
	return m
}

const (
	defaultMsgQueueSize = 20
)

// agentLane holds the channel coordination and agent reference.
// sync.Once ensures the lane and agent are initialized exactly once;
// exitMu serializes the exit (closing done) against a submit's final
// check-and-send so the two can never interleave.
type agentLane struct {
	once    sync.Once
	exitMu  sync.Mutex
	msgCh   chan Message   // message channel
	done    chan struct{}  // closed when the agent exits
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
			// The lane outlives the submitting connection: its
			// lifetime is the idle timeout, not any one peer.
			lane.agent = newUserAgent(context.WithoutCancel(ctx), UserAgentOptions{
				AgentID:        agentID,
				Model:          msg.Model,
				MsgCh:          lane.msgCh,
				ResolveModel:   m.resolveModel,
				PersistTurn:    m.persistTurn,
				GetAgentConfig: m.getAgentConfig,
				Tools:          m.tools,
				Logger:         m.logger,
				OnExit: func() {
					lane.exitMu.Lock()
					defer lane.exitMu.Unlock()
					close(lane.done)
					m.lanes.Delete(key)
				},
			})
		})
	})

	// once.Do returns as soon as the goroutine is launched, not when it finishes.
	// All callers block here until lane.agent is initialized.
	lane.agentWg.Wait()

	// A lane whose agent already exited is rejected up front: the
	// broadcaster must not see conns for a dead lane.
	select {
	case <-lane.done:
		m.logger.Warn().Str("agent_id", agentID).Msg("submit dropped: lane exited")
		return false
	default:
	}

	// Add connection to agent's broadcaster and update model.
	lane.agent.addConn(connID, conn)
	lane.agent.updateModel(msg.Model)

	// The exit lock closes the drop window for good: without it a
	// select with both arms ready picks randomly, so a submit could
	// land a message in a lane whose loop already decided to leave —
	// success reported, prompt never run. Under the lock there is no
	// middle state: either the send beats the close (the loop's
	// emptiness check sees the message) or done is closed and the
	// submit reports the drop.
	lane.exitMu.Lock()
	select {
	case <-lane.done:
		lane.exitMu.Unlock()
		m.logger.Warn().Str("agent_id", agentID).Msg("submit dropped: lane exited")
		return false
	case lane.msgCh <- msg:
		lane.exitMu.Unlock()
		return true
	}
}

// Join attaches a connection to an existing lane so it receives the
// lane's events — including a replay of the live turn's streamed
// prefix. A lane that doesn't exist stays nonexistent: the status
// reply already reports an idle lane.
func (m *UserAgentManager) Join(agentID, connID string, conn *websocket.Conn) {
	val, ok := m.lanes.Load(agentID)
	if !ok {
		return
	}
	lane := val.(*agentLane)
	lane.agentWg.Wait()
	lane.agent.Join(connID, conn)
}

// Get returns the agent for agentID or nil if not found.
func (m *UserAgentManager) Get(agentID string) *UserAgent {
	key := agentID

	if val, ok := m.lanes.Load(key); ok {
		return val.(*agentLane).agent
	}
	return nil
}

// Drain ends the manager's live work: every running turn is
// interrupted — a partial answer settles into the thread per the
// interrupt rules — and the call waits, bounded by timeout, for the
// turns to finish persisting. Connections are gone by the time a
// server drains (its context ended them); persistence is the
// durable path, and a reconnecting UI refetches into it.
func (m *UserAgentManager) Drain(timeout time.Duration) {
	m.lanes.Range(func(_, val any) bool {
		if ag := val.(*agentLane).agent; ag != nil {
			ag.Interrupt()
		}
		return true
	})
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !m.anyWorking() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// anyWorking reports whether any lane still runs a turn.
func (m *UserAgentManager) anyWorking() bool {
	working := false
	m.lanes.Range(func(_, val any) bool {
		if ag := val.(*agentLane).agent; ag != nil && ag.LaneStatus().Working {
			working = true
			return false
		}
		return true
	})
	return working
}
