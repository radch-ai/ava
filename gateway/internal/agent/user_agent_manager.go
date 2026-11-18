package agent

// UserAgentManager owns one agentState per agent — the authoritative
// queue and its subscribers — and the spawning of disposable runtime
// workers on demand. A worker's exit releases the worker only;
// pending work, pause state, and connections stay behind.

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// UserAgentManagerOptions configures a new UserAgentManager.
type UserAgentManagerOptions struct {
	// ResolveModel maps a selection to a callable endpoint for
	// every lane; bound by the gateway at boot.
	ResolveModel ModelResolver
	// PersistTurn appends turn messages to the thread for every
	// lane; bound by the gateway at boot.
	PersistTurn TurnSink
	// GetAgentConfig reads an agent's editable config — the prompt
	// texts and tool list a turn needs; bound by the gateway at boot.
	GetAgentConfig AgentConfigSource
	// Tools fetches each lane's effective tools per turn —
	// permissions applied; nil leaves lanes tool-less.
	Tools AgentToolsFunc
	// GetAgentThread reads an agent's thread as the turn's model
	// input; bound by the gateway at boot. Required: an unbound
	// source fails each turn at message level.
	GetAgentThread AgentThreadSource
}

// UserAgentManager manages per-agent queue states with thread-safe
// access. Uses sync.Map for lock-free reads.
type UserAgentManager struct {
	states         sync.Map // map[agentID]*agentState
	logger         zerolog.Logger
	resolveModel   ModelResolver
	persistTurn    TurnSink
	getAgentConfig AgentConfigSource
	tools          AgentToolsFunc
	getAgentThread AgentThreadSource

	rootCtx    context.Context
	rootCancel context.CancelFunc
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
		m.getAgentThread = opts.GetAgentThread
	}
	m.rootCtx, m.rootCancel = context.WithCancel(context.Background())
	return m
}

// state returns the agent's queue state, creating it if needed. The
// state exists before any prompt: subscriptions attach to it even
// while no worker runs.
func (m *UserAgentManager) state(agentID string) *agentState {
	if v, ok := m.states.Load(agentID); ok {
		return v.(*agentState)
	}
	s := newAgentState(agentID, m.rootCtx, m.logger)
	s.ensureWorker = func() { m.ensureWorker(s) }
	actual, _ := m.states.LoadOrStore(agentID, s)
	return actual.(*agentState)
}

// ensureWorker installs exactly one live worker on the state; the
// worker claims from the queue and exits as soon as nothing may run,
// without touching the state's lifetime.
func (m *UserAgentManager) ensureWorker(s *agentState) {
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.worker != nil {
		return
	}
	s.worker = m.newWorker(s)
}

// newWorker builds and starts a worker; caller holds workerMu.
func (m *UserAgentManager) newWorker(s *agentState) *UserAgent {
	return newUserAgent(m.rootCtx, UserAgentOptions{
		AgentID:        s.agentID,
		State:          s,
		ResolveModel:   m.resolveModel,
		PersistTurn:    m.persistTurn,
		GetAgentConfig: m.getAgentConfig,
		Tools:          m.tools,
		GetAgentThread: m.getAgentThread,
		Logger:         m.logger,
		OnExit:         func(w *UserAgent) { m.workerExited(s, w) },
	})
}

// workerExited clears a worker that finished. Only the matching
// generation clears; a replacement may already be installed. A
// replacement spawns only when work can run right now — claim's own
// predicate — so a paused queue never respawn-loops against a worker
// that has nothing to do; its manager-owned state (items, pause,
// subscribers) stays intact without a live worker and for the
// process's lifetime. Work that arrived while the worker was leaving
// still spawns one: an accepted item is never stranded in a dead
// worker's wake. A draining queue ends for good: no replacement runs
// a gated claim.
func (m *UserAgentManager) workerExited(s *agentState, w *UserAgent) {
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.worker != w {
		return
	}
	s.worker = nil
	if s.canRunNow() && !s.drainingNow() {
		s.worker = m.newWorker(s)
	}
}

// Submit attaches the connection to the agent's subscribers,
// enqueues one prompt, and ensures a worker can pick it up. A
// rejected admission (full queue, draining) still leaves the
// connection subscribed; the gateway reports the rejection.
func (m *UserAgentManager) Submit(agentID, connID string, conn *websocket.Conn, content string, model *apitypes.ModelEntry) (string, error) {
	s := m.state(agentID)
	s.conns.Add(connID, conn)
	item, err := s.enqueue(content, model)
	if err != nil {
		return "", err
	}
	return item.id, nil
}

// SendNow promotes one pending item to run next, carrying the
// caller's current model selection: a paused queue rebinds its
// failed-model entries to it, a healthy queue ignores it. The
// gateway maps the error to a rejected queue_result; the queue
// mutates only on success.
func (m *UserAgentManager) SendNow(agentID, itemID string, model *apitypes.ModelEntry) (string, error) {
	s := m.state(agentID)
	item, err := s.sendNow(itemID, model)
	if err != nil {
		return "", err
	}
	return item.id, nil
}

// Join attaches a connection to the agent's subscribers so it
// receives the queue snapshot and, while a turn streams, its
// streamed prefix — ordered before the chunks still to come.
func (m *UserAgentManager) Join(agentID, connID string, conn *websocket.Conn) {
	s := m.state(agentID)
	s.workerMu.Lock()
	w := s.worker
	s.workerMu.Unlock()
	if w != nil {
		w.Join(connID, conn)
		return
	}
	s.conns.AddWithBacklog(connID, conn, func() []any {
		return []any{wstypes.Event{
			Type:    wstypes.EventTypeQueueState,
			AgentID: agentID,
			Data:    s.stateSnapshot(),
		}}
	})
}

// Get returns the agent's live worker or nil; workers are optional
// surfaces over the durable queue state.
func (m *UserAgentManager) Get(agentID string) *UserAgent {
	if v, ok := m.states.Load(agentID); ok {
		s := v.(*agentState)
		s.workerMu.Lock()
		defer s.workerMu.Unlock()
		return s.worker
	}
	return nil
}

// Status snapshots the agent onto the wire even without a worker —
// a queue can hold pending work while idle.
func (m *UserAgentManager) Status(agentID string) wstypes.LaneStatus {
	if v, ok := m.states.Load(agentID); ok {
		s := v.(*agentState)
		s.workerMu.Lock()
		w := s.worker
		s.workerMu.Unlock()
		if w != nil {
			return w.LaneStatus()
		}
		return wstypes.LaneStatus{
			QueueLen:  s.queueLen(),
			ConnCount: s.conns.Count(),
		}
	}
	return wstypes.LaneStatus{}
}

// Interrupt is the Stop button: cancel the active item, if any, and
// hold the queue behind it — waiting work pauses instead of running
// on. Send-now's own interrupt never arms the gate.
func (m *UserAgentManager) Interrupt(agentID string) {
	if v, ok := m.states.Load(agentID); ok {
		v.(*agentState).stopNow()
	}
}

// RemoveConn drops a connection from the agent's subscribers, worker
// or not. States are per-agent and live for the process: collecting
// them here could only race a concurrent Submit that already loaded
// the state, orphaning its admitted work.
func (m *UserAgentManager) RemoveConn(agentID, connID string) {
	if v, ok := m.states.Load(agentID); ok {
		v.(*agentState).conns.Remove(connID)
	}
}

// Drain ends the manager's live work: every queue stops admitting
// and claiming, each running item is interrupted — its partial
// answer settles into the thread per the interrupt rules — and the
// call waits, bounded by timeout, for the items to finish
// persisting. Workers then wind down with the manager's context.
// Pending queue items are in-memory state and do not outlive the
// process; transcript retention belongs to the backend and durable
// storage rebuilds it.
func (m *UserAgentManager) Drain(timeout time.Duration) {
	m.states.Range(func(_, val any) bool {
		s := val.(*agentState)
		s.beginDrain()
		s.interruptActive()
		return true
	})
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !m.anyActive() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.rootCancel()
}

// anyActive reports whether any queue still executes an item.
func (m *UserAgentManager) anyActive() bool {
	active := false
	m.states.Range(func(_, val any) bool {
		if val.(*agentState).hasActive() {
			active = true
			return false
		}
		return true
	})
	return active
}
