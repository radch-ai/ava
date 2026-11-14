package agent

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/wsconn"
)

const (
	// defaultIdleTimeout is how long a lane survives its last message
	// before the goroutine exits and the lane is deleted.
	defaultIdleTimeout = 15 * time.Minute
)

// Message represents a message submitted to an agent. A nil Model
// follows the default routing.
type Message struct {
	ID      string
	Content string
	Model   *apitypes.ModelEntry
}

// ModelResolver maps a selection to a callable endpoint. An alias of
// the backend.ResolveModel signature, so the bound function needs no
// adapter.
type ModelResolver = func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error)

type UserAgentOptions struct {
	AgentID     string
	Model       *apitypes.ModelEntry
	MsgCh       chan Message // passed from UserAgentManager
	IdleTimeout time.Duration
	OnExit      func()
	// ResolveModel maps a selection to a callable endpoint for
	// every lane; bound by the gateway at boot.
	ResolveModel ModelResolver
	Logger       zerolog.Logger
}

type UserAgent struct {
	agentID string
	ctx     context.Context

	MsgCh chan Message // owned by UserAgentManager, passed via options
	mu    sync.Mutex

	conns *wsconn.Broadcaster // Broadcasts to all connected devices

	idleTimeout time.Duration
	onExit      func()
	logger      zerolog.Logger

	resolveModel ModelResolver

	model *apitypes.ModelEntry
}

// newUserAgent creates a lane agent and starts its message loop.
// Returns nil if MsgCh is nil (graceful degradation per lane, doesn't crash the app).
func newUserAgent(ctx context.Context, opts UserAgentOptions) *UserAgent {
	// MsgCh is required for agent operation.
	// This should never happen in practice since UserAgentManager.SubmitMsg always
	// creates the channel before calling newUserAgent. The check exists for defensive
	// programming and for tests that may call newUserAgent directly.
	// Gracefully return nil rather than panic (per-lane error).
	if opts.MsgCh == nil {
		opts.Logger.Error().
			Str("agent_id", opts.AgentID).
			Msg("MsgCh is nil, cannot create agent")
		return nil
	}

	a := &UserAgent{
		ctx:     ctx,
		agentID: opts.AgentID,

		conns:        wsconn.NewBroadcaster(opts.Logger),
		model:        opts.Model,
		MsgCh:        opts.MsgCh,
		idleTimeout:  opts.IdleTimeout,
		logger:       opts.Logger,
		resolveModel: opts.ResolveModel,

		onExit: opts.OnExit,
	}

	if a.idleTimeout == 0 {
		a.idleTimeout = defaultIdleTimeout
	}

	// Start the message processing loop
	go a.loop()

	return a
}

// AddConn adds a connection to the broadcaster.
// Called when a device sends a message to this agent.
func (a *UserAgent) addConn(connID string, conn *websocket.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conns.Add(connID, conn)
}

// updateModel sets the model for the next query; nil follows the
// default routing.
func (a *UserAgent) updateModel(entry *apitypes.ModelEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = entry
}

// RemoveConn removes a connection from the broadcaster.
// Called when a device disconnects.
func (a *UserAgent) RemoveConn(connID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.conns.Remove(connID)
}

// ConnCount returns the number of active connections.
func (a *UserAgent) ConnCount() int {
	return a.conns.Count()
}

func (a *UserAgent) loop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case msg := <-a.MsgCh:
			// Coalesce the batch behind the first message so the
			// queue is empty between turns.
			a.drainMessages(msg)
			idleTimer.Reset(a.idleTimeout)

		case <-idleTimer.C:
			a.mu.Lock()
			if len(a.MsgCh) == 0 {
				a.mu.Unlock()
				if a.onExit != nil {
					a.onExit()
				}
				return
			}
			a.mu.Unlock()
			idleTimer.Reset(a.idleTimeout)

		case <-a.ctx.Done():
			if a.onExit != nil {
				a.onExit()
			}
			return
		}
	}
}

func (a *UserAgent) drainMessages(first Message) []Message {
	messages := []Message{first}

	for {
		select {
		case m := <-a.MsgCh:
			messages = append(messages, m)
		default:
			return messages
		}
	}
}

func (a *UserAgent) GetStatus() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	modelID := ""
	if a.model != nil {
		modelID = a.model.ID
	}

	return map[string]interface{}{
		"agent_id":   a.agentID,
		"queue_len":  len(a.MsgCh),
		"active":     a.ctx.Err() == nil,
		"model":      modelID,
		"conn_count": a.conns.Count(),
	}
}
