package agent

// The authoritative per-agent queue: pending prompts, the reserved
// next item, the active turn, and the pause gate. Manager-owned, so
// the queue and its subscriptions outlive the disposable runtime
// worker.

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/wsconn"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// queueCapacity bounds an agent's waiting work: pending FIFO plus
// the reserved-next item; the active item does not count.
const queueCapacity = 20

// Queue admission and control rejections; user-facing text rides
// queue_result, so no queue state changes on rejection.
var (
	errQueueDraining   = errors.New("shutting down; try again once the service restarts")
	errQueueFull       = errors.New("queue is full; wait for pending work to finish")
	errReservationHeld = errors.New("another item is already selected to run next")
	errItemNotPending  = errors.New("item is not pending in this queue")
)

// queuedItem is one admitted prompt. Content is immutable after
// admission; the model selection rides the item from submission and
// moves only through a paused send_now's recovery rebind. kind is the
// admission classification: a command executes without a model.
type queuedItem struct {
	id      string
	content string
	kind    string
	model   *apitypes.ModelEntry
}

// turnClaim is an item moving to execution, with the context the
// turn runs on. userMsgID names the started turn's persisted user
// row — distinct from the item ID, which stays the stream ID.
type turnClaim struct {
	item      *queuedItem
	userMsgID string
	ctx       context.Context
}

// agentState is one agent's queue plus its subscribers. Lock order:
// pubMu before mu, and neither across a network write — mu guards
// data only; pubMu serializes mutate-then-publish so subscribers see
// queue snapshots in mutation order.
type agentState struct {
	agentID string

	pubMu sync.Mutex
	mu    sync.Mutex

	pending []*queuedItem
	next    *queuedItem // reserved by send_now; runs before the FIFO

	active    *queuedItem
	userMsgID string
	paused    bool
	// pauseModel is the selection whose failure armed the pause —
	// nil both for default routing and while unpaused. A paused
	// send_now rebinds the pending entries captured on it.
	pauseModel *apitypes.ModelEntry
	cancel     context.CancelFunc // cancels the active item's turn ctx

	// rootCtx parents every turn's context; the manager's root, set
	// once when the state is created.
	rootCtx context.Context

	conns *wsconn.Broadcaster

	// publish emits one queue snapshot; injectable for tests.
	publish func(wstypes.QueueStateEvent)

	// ensureWorker starts the runtime worker when work can run; set
	// by the manager, nil in unit tests.
	ensureWorker func()

	drainMu  sync.Mutex
	draining bool

	// workerMu guards the disposable runtime worker handle; the
	// queue itself never depends on it.
	workerMu sync.Mutex
	worker   *UserAgent
}

func newAgentState(agentID string, rootCtx context.Context, logger zerolog.Logger) *agentState {
	s := &agentState{
		agentID: agentID,
		rootCtx: rootCtx,
		conns:   wsconn.NewBroadcaster(logger),
	}
	s.publish = func(ev wstypes.QueueStateEvent) {
		_ = s.conns.WriteJSON(context.Background(), wstypes.Event{
			Type:    wstypes.EventTypeQueueState,
			AgentID: s.agentID,
			Data:    ev,
		})
	}
	return s
}

// drainingNow reports whether shutdown has gated the queue.
func (s *agentState) drainingNow() bool {
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	return s.draining
}

// beginDrain gates the queue against new admissions and further
// claims; the active turn, if any, keeps settling.
func (s *agentState) beginDrain() {
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	s.draining = true
}

// enqueue admits one prompt, publishing the new snapshot. Rejected
// when draining or at capacity; a paused queue stays paused —
// enqueueing is not a resume.
func (s *agentState) enqueue(content string, model *apitypes.ModelEntry, kind string) (*queuedItem, error) {
	s.pubMu.Lock()

	if s.drainingNow() {
		s.pubMu.Unlock()
		return nil, errQueueDraining
	}

	s.mu.Lock()
	if len(s.pending)+s.nextSlotCount() >= queueCapacity {
		s.mu.Unlock()
		s.pubMu.Unlock()
		return nil, errQueueFull
	}
	item := &queuedItem{id: uuid.NewString(), content: content, kind: kind, model: model}
	s.pending = append(s.pending, item)
	paused := s.paused
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	s.publish(snapshot)
	s.pubMu.Unlock()

	// Spawning the worker sits outside pubMu: the lock serializes
	// mutate-then-publish, nothing else. Concurrent ensureWorker
	// calls dedup under workerMu.
	if !paused && s.ensureWorker != nil {
		s.ensureWorker()
	}
	return item, nil
}

// nextSlotCount reports the reserved slot as 0/1; caller holds mu.
func (s *agentState) nextSlotCount() int {
	if s.next != nil {
		return 1
	}
	return 0
}

// claim takes the next item to execute: the reserved item first —
// an explicit selection, which also clears the pause gate — then the
// FIFO head when not paused. The claim publishes the
// waiting→executing transition before returning, so subscribers see
// the item leave the queue and its user row appear together. The
// turn context is installed with the claim so an interrupt can land
// before streaming starts. Returns nil when nothing may run now.
func (s *agentState) claim() *turnClaim {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	s.mu.Lock()
	if s.drainingNow() {
		s.mu.Unlock()
		return nil
	}
	if s.active != nil {
		s.mu.Unlock()
		return nil
	}
	var item *queuedItem
	if s.next != nil {
		item = s.next
		s.next = nil
		s.paused = false
		s.pauseModel = nil
	} else if !s.paused && len(s.pending) > 0 {
		item = s.pending[0]
		s.pending = s.pending[1:]
	}
	if item == nil {
		s.mu.Unlock()
		return nil
	}
	claim := s.activateLocked(item)
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	s.publish(snapshot)
	return claim
}

// activateLocked installs the item as active with a fresh turn
// context; caller holds mu.
func (s *agentState) activateLocked(item *queuedItem) *turnClaim {
	ctx, cancel := context.WithCancel(s.rootCtx)
	s.active = item
	s.cancel = cancel
	// Commands persist no user row: their work is the thread switch,
	// not a prompt in the transcript.
	s.userMsgID = ""
	if item.kind == wstypes.QueueItemKindPrompt {
		s.userMsgID = uuid.NewString()
	}
	return &turnClaim{item: item, userMsgID: s.userMsgID, ctx: ctx}
}

// finish records the active turn's end and reports whether another
// item may run now. Only a message-level failure arms the pause
// gate, and only while waiting work remains: the gate exists to hold
// items queued before the failure, so an empty queue never pauses —
// the next prompt runs normally. The failed turn's selection rides
// the gate so a paused send_now can rebind the entries sharing it.
// Tool failures and stops leave dispatch alone.
func (s *agentState) finish(messageFailed bool) bool {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	s.mu.Lock()
	failedModel := s.active.model
	s.active = nil
	s.cancel = nil
	s.userMsgID = ""
	if messageFailed && len(s.pending) > 0 {
		s.paused = true
		s.pauseModel = failedModel
	}
	canRun := s.next != nil || (len(s.pending) > 0 && !s.paused)
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	s.publish(snapshot)
	return canRun
}

// sendNow promotes one pending item to run next. The first
// reservation wins; a repeat for the already-reserved item is an
// idempotent ack. The active turn, if any, is interrupted through
// the cancel captured with the reservation — never a later turn's.
//
// Model is the caller's current selection and applies only while
// paused — the recovery path: every waiting entry captured on the
// selection that armed the pause is rebound to it, so the items
// behind the reservation do not re-fail on the same model. A
// healthy queue ignores Model entirely; each item keeps the
// selection it was admitted with.
func (s *agentState) sendNow(itemID string, model *apitypes.ModelEntry) (*queuedItem, error) {
	s.pubMu.Lock()

	if s.drainingNow() {
		s.pubMu.Unlock()
		return nil, errQueueDraining
	}

	s.mu.Lock()
	if s.next != nil && s.next.id == itemID {
		s.rebindPausedLocked(model)
		item := s.next
		snapshot := s.snapshotLocked()
		s.mu.Unlock()
		s.publish(snapshot)
		s.pubMu.Unlock()
		return item, nil
	}
	idx := -1
	for i, it := range s.pending {
		if it.id == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		s.pubMu.Unlock()
		return nil, errItemNotPending
	}
	if s.next != nil {
		s.mu.Unlock()
		s.pubMu.Unlock()
		return nil, errReservationHeld
	}
	item := s.pending[idx]
	s.pending = append(s.pending[:idx], s.pending[idx+1:]...)
	s.next = item
	s.rebindPausedLocked(model)
	cancel := s.cancel
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	s.publish(snapshot)
	s.pubMu.Unlock()

	// The interrupt and the spawn sit outside pubMu: the reservation
	// is already published, and the cancel was captured with it — it
	// can only ever name the interrupted turn, never a later one.
	if cancel != nil {
		cancel()
	}
	if s.ensureWorker != nil {
		s.ensureWorker()
	}
	return item, nil
}

// rebindPausedLocked rebinds every waiting entry captured on the
// pause-arming selection to the caller's current one. Non-matching
// entries — deliberately queued on another selection — keep theirs;
// an unpaused queue is a no-op. Caller holds mu.
func (s *agentState) rebindPausedLocked(model *apitypes.ModelEntry) {
	if !s.paused {
		return
	}
	rebind := func(it *queuedItem) {
		if sameModelEntry(it.model, s.pauseModel) {
			it.model = model
		}
	}
	for _, it := range s.pending {
		rebind(it)
	}
	if s.next != nil {
		rebind(s.next)
	}
}

// sameModelEntry compares two selections; nil is default routing and
// matches only nil.
func sameModelEntry(a, b *apitypes.ModelEntry) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.ID == b.ID && a.ProviderID == b.ProviderID
}

// interruptActive cancels the active turn's context, if any. Manual
// stops and send-now share this; neither pauses the queue.
func (s *agentState) interruptActive() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// stopNow is the Stop button: interrupt the active turn and, while
// waiting work remains, arm the pause gate so the queue does not
// continue on its own — the stopper decides what runs next, and
// Send immediately is the resume. The stopped turn's selection rides
// the gate like a failure's. An already-armed gate is never
// rewritten: its recorded selection names what failed, and a stop
// with no turn to interrupt has nothing to attribute. An existing
// reservation wins: it was an explicit send-now and claims
// momentarily; an empty queue only interrupts, per the arming
// invariant.
func (s *agentState) stopNow() {
	s.pubMu.Lock()

	if s.drainingNow() {
		s.pubMu.Unlock()
		return
	}

	s.mu.Lock()
	var stoppedModel *apitypes.ModelEntry
	if s.active != nil {
		stoppedModel = s.active.model
	}
	if !s.paused && len(s.pending) > 0 && s.next == nil {
		s.paused = true
		s.pauseModel = stoppedModel
	}
	cancel := s.cancel
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	s.publish(snapshot)
	s.pubMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// snapshotLocked renders the wire snapshot; caller holds mu.
func (s *agentState) snapshotLocked() wstypes.QueueStateEvent {
	ev := wstypes.QueueStateEvent{
		Items:  make([]wstypes.QueueItem, 0, len(s.pending)),
		Paused: s.paused,
	}
	for _, it := range s.pending {
		ev.Items = append(ev.Items, queueItemWire(it))
	}
	if s.next != nil {
		n := queueItemWire(s.next)
		ev.Next = &n
	}
	if s.active != nil {
		ev.Active = &wstypes.QueueActiveItem{
			ID:            s.active.id,
			Content:       s.active.content,
			Kind:          s.active.kind,
			UserMessageID: s.userMsgID,
		}
		if s.active.model != nil {
			m := modelRefWire(s.active.model)
			ev.Active.Model = &m
		}
	}
	return ev
}

// stateSnapshot captures the current wire snapshot for a join or
// status reply.
func (s *agentState) stateSnapshot() wstypes.QueueStateEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// queueLen reports waiting items (FIFO plus reserved) for
// lane_status.
func (s *agentState) queueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) + s.nextSlotCount()
}

// canRunNow reports whether claim() would return an item right now:
// a reserved send-now, or FIFO head while not paused. The worker
// exit path uses it to respawn only for work that can actually run,
// so a paused queue keeps its state without a respawn loop.
func (s *agentState) canRunNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next != nil || (!s.paused && len(s.pending) > 0)
}

// modelRefWire projects an entry onto the wire's public reference —
// never a resolved endpoint or credential.
func modelRefWire(e *apitypes.ModelEntry) wstypes.ModelRef {
	return wstypes.ModelRef{ID: e.ID, ProviderID: e.ProviderID}
}

// queueItemWire renders one waiting item for a snapshot.
func queueItemWire(it *queuedItem) wstypes.QueueItem {
	q := wstypes.QueueItem{ID: it.id, Content: it.content, Kind: it.kind}
	if it.model != nil {
		m := modelRefWire(it.model)
		q.Model = &m
	}
	return q
}

// activeModelID reports the active item's model id for lane_status;
// empty when nothing runs or the item follows default routing.
func (s *agentState) activeModelID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.model == nil {
		return ""
	}
	return s.active.model.ID
}

// hasActive reports whether an item is executing.
func (s *agentState) hasActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active != nil
}
