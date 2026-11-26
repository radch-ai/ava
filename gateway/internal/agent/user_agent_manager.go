package agent

// UserAgentManager owns one agentState per agent — the authoritative
// queue and its subscribers — and the spawning of disposable runtime
// workers on demand. A worker's exit releases the worker only;
// pending work, pause state, and connections stay behind.

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// UserAgentManagerOptions configures a new UserAgentManager.
type UserAgentManagerOptions struct {
	// ResolveModel maps a selection to a callable endpoint for
	// every lane; bound by the gateway at boot.
	ResolveModel ModelResolver
	// Journal is the durable lifecycle feed every transition commits
	// through; bound by the gateway at boot. An unbound journal fails
	// every admission and turn closed.
	Journal LifecycleJournal
	// GetAgentConfig reads an agent's editable config — the prompt
	// texts a turn needs; bound by the gateway at boot.
	GetAgentConfig AgentConfigSource
	// Tools fetches each lane's effective tools per turn —
	// permissions applied; nil leaves lanes tool-less.
	Tools AgentToolsFunc
	// GetAgentThread reads the agent's active thread as the turn's
	// model input; bound by the gateway at boot. Required: an unbound
	// source fails each turn at message level.
	GetAgentThread AgentThreadSource
	// NoteDiscoveredWindow records a window a provider stated in its
	// own rejection; bound by the gateway at boot. Optional: an unbound
	// note drops the lesson.
	NoteDiscoveredWindow WindowNoteFunc
}

// UserAgentManager manages per-agent queue states with thread-safe
// access. States are never removed — a submitted item outlives its
// worker — so a plain map under a read lock is the cheapest fit.
type UserAgentManager struct {
	statesMu sync.RWMutex
	states   map[string]*agentState

	logger               zerolog.Logger
	resolveModel         ModelResolver
	getAgentConfig       AgentConfigSource
	tools                AgentToolsFunc
	getAgentThread       AgentThreadSource
	journal              LifecycleJournal
	noteDiscoveredWindow WindowNoteFunc

	// turns counts claimed turns across every lane; Drain waits on it.
	turns sync.WaitGroup

	rootCtx    context.Context
	rootCancel context.CancelFunc
}

// NewUserAgentManager creates a new UserAgentManager.
func NewUserAgentManager(logger zerolog.Logger, opts *UserAgentManagerOptions) *UserAgentManager {
	m := &UserAgentManager{
		logger: logger,
		states: make(map[string]*agentState),
	}
	if opts != nil {
		m.resolveModel = opts.ResolveModel
		m.getAgentConfig = opts.GetAgentConfig
		m.tools = opts.Tools
		m.getAgentThread = opts.GetAgentThread
		m.journal = opts.Journal
		m.noteDiscoveredWindow = opts.NoteDiscoveredWindow
	}
	m.rootCtx, m.rootCancel = context.WithCancel(context.Background())
	return m
}

// state returns the agent's queue state, creating it if needed. The
// state exists before any prompt: subscriptions attach to it even
// while no worker runs.
func (m *UserAgentManager) state(agentID string) *agentState {
	m.statesMu.RLock()
	s, ok := m.states[agentID]
	m.statesMu.RUnlock()
	if ok {
		return s
	}

	m.statesMu.Lock()
	defer m.statesMu.Unlock()
	if s, ok := m.states[agentID]; ok {
		return s
	}
	s = newAgentState(agentID, m.rootCtx, &m.turns, m.logger)
	s.ensureWorker = func() { m.ensureWorker(s) }
	m.states[agentID] = s
	return s
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
	return newUserAgent(UserAgentOptions{
		State:                s,
		ResolveModel:         m.resolveModel,
		GetAgentConfig:       m.getAgentConfig,
		Tools:                m.tools,
		GetAgentThread:       m.getAgentThread,
		Journal:              m.journal,
		NoteDiscoveredWindow: m.noteDiscoveredWindow,
		Logger:               m.logger,
		OnExit:               func(w *UserAgent) { m.workerExited(s, w) },
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
	s.mu.Lock()
	runnable := s.runnableLocked()
	s.mu.Unlock()
	if runnable {
		s.worker = m.newWorker(s)
	}
}

// Journal returns the durable lifecycle journal the manager's workers
// commit through, nil when the binary bound none.
func (m *UserAgentManager) Journal() LifecycleJournal { return m.journal }

// Submit admits one prompt or command. The durable acceptance commits
// before the item enters the runtime queue, so a rejected submission
// writes nothing and an accepted one can never be lost from the journal;
// the caller answers the requesting connection with queue_result, and
// subscribers learn of it through the lifecycle feed.
func (m *UserAgentManager) Submit(agentID, content string, model *apitypes.ModelEntry, kind string) (string, error) {
	if kind == "" {
		kind = wstypes.QueueItemKindPrompt
	}
	if m.journal == nil {
		return "", errors.New("no lifecycle journal configured")
	}
	s := m.state(agentID)
	itemID := uuid.NewString()
	item, err := s.admit(itemID, content, model, kind, func(ctx context.Context) (*backend.JournalBatch, error) {
		return m.journal.AcceptItem(ctx, agentID, itemID, content, kind, model)
	})
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

// HandOff finishes one connection's synchronization: under the agent's
// publication boundary it compares the client's position with the
// committed head and, when they still match, registers the connection
// with sync_ready as its ordered backlog. The prefix cut and the
// registration share the streaming cut's snapMu, so a chunk flushed
// between the two cannot miss the new connection — the backlog is the
// continuation point the prefix names. Live events enqueued after the
// boundary releases follow that backlog, so nothing committed between
// the last replay batch and the registration is lost. false means the
// head moved off the position the empty replay page ended on, so the
// page cannot advance the client — the caller closes without
// sync_ready and the device recovers through a fresh sync. The
// returned head is the one the comparison read.
func (m *UserAgentManager) HandOff(ctx context.Context, agentID, connID string, conn *websocket.Conn, pos int64) (bool, int64, error) {
	if m.journal == nil {
		return false, 0, errors.New("no lifecycle journal configured")
	}
	s := m.state(agentID)
	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	serverHistory, head, _, err := m.journal.JournalStatus(ctx, agentID)
	if err != nil {
		return false, 0, err
	}
	if head != pos {
		return false, head, nil
	}
	s.workerMu.Lock()
	w := s.worker
	s.workerMu.Unlock()
	// The cut and the registration are one step under snapMu; the lock
	// order matches the streaming flush (snapMu → worker.mu, snapMu →
	// conns), so nothing inverts.
	s.snapMu.Lock()
	var prefix *wstypes.TurnPrefix
	if w != nil {
		prefix = w.prefixSnapshotLocked()
	}
	ready := wstypes.Event{
		Type:    wstypes.EventTypeSyncReady,
		AgentID: agentID,
		Data: wstypes.SyncReadyEvent{
			Cursor: backend.FormatCursor(serverHistory, head),
			Queue:  s.stateSnapshot(),
			Lane:   w.LaneStatus(),
			Prefix: prefix,
		},
	}
	s.conns.AddWithBacklog(connID, conn, func() []any { return []any{ready} })
	s.snapMu.Unlock()
	return true, head, nil
}

// peek returns the agent's queue state without creating one; false
// means no state exists — nothing was ever admitted for the agent.
func (m *UserAgentManager) peek(agentID string) (*agentState, bool) {
	m.statesMu.RLock()
	defer m.statesMu.RUnlock()
	s, ok := m.states[agentID]
	return s, ok
}

// Interrupt is the Stop button: cancel the active item, if any, and
// hold the queue behind it — waiting work pauses instead of running
// on. Send-now's own interrupt never arms the gate.
func (m *UserAgentManager) Interrupt(agentID string) {
	if s, ok := m.peek(agentID); ok {
		s.stopNow()
	}
}

// RemoveConn drops a connection from the agent's subscribers, worker
// or not. States are per-agent and live for the process: collecting
// them here could only race a concurrent Submit that already loaded
// the state, orphaning its admitted work.
func (m *UserAgentManager) RemoveConn(agentID, connID string) {
	if s, ok := m.peek(agentID); ok {
		s.conns.Remove(connID)
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
	m.statesMu.RLock()
	states := make([]*agentState, 0, len(m.states))
	for _, s := range m.states {
		states = append(states, s)
	}
	m.statesMu.RUnlock()

	for _, s := range states {
		s.beginDrain()
		s.interruptActive()
	}
	// Every claim settles exactly one finish, so the wait ends as soon
	// as the last interrupted turn has persisted — no polling.
	done := make(chan struct{})
	go func() {
		m.turns.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
	m.rootCancel()
}
