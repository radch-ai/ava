package agent

// The authoritative per-agent queue: pending prompts, the reserved
// next item, the active turn, and the pause gate. Manager-owned, so
// the queue and its subscriptions outlive the disposable runtime
// worker.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
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

// turnClaim is an item moving to execution, with the context the turn
// runs on. The persisted user row's identity is assigned by the backend
// when the turn starts, so the claim carries none.
type turnClaim struct {
	item *queuedItem
	ctx  context.Context
}

// agentState is one agent's queue plus its subscribers. Lock order:
// pubMu before snapMu before mu, and none held across a network write
// except pubMu's own publication calls — mu guards data only; pubMu
// serializes durable-mutate-then-publish so subscribers see queue
// snapshots and lifecycle frames in commit order; snapMu cuts the
// streaming answer (prefix extension, sequence, chunk frame) so the cut
// never queues behind a journal write.
type agentState struct {
	agentID string

	pubMu  sync.Mutex
	snapMu sync.Mutex
	mu     sync.Mutex

	pending []*queuedItem
	next    *queuedItem // reserved by send_now; runs before the FIFO

	active *queuedItem
	// userMsgID names the started item's persisted user row, assigned by
	// the backend's durable start. Empty until it commits, and for
	// commands that never own a row.
	userMsgID string
	paused    bool
	// pauseModel is the selection whose failure armed the pause —
	// nil both for default routing and while unpaused. A paused
	// send_now rebinds the pending entries captured on it.
	pauseModel *apitypes.ModelEntry
	cancel     context.CancelFunc // cancels the active item's turn ctx
	// halted stops all dispatch after a turn whose terminal outcome
	// could not be recorded; only service recovery — which settles the
	// stuck item before the next process serves the lane — clears it.
	halted bool

	// rootCtx parents every turn's context; the manager's root, set
	// once when the state is created.
	rootCtx context.Context

	conns *wsconn.Broadcaster

	// publish emits one queue snapshot; injectable for tests.
	publish func(wstypes.QueueStateEvent)

	// ensureWorker starts the runtime worker when work can run; set
	// by the manager, nil in unit tests.
	ensureWorker func()

	// draining gates admission and claims for good once shutdown starts.
	draining atomic.Bool

	// turnWG counts claimed turns still settling; the manager's Drain
	// waits on it instead of polling the lane flags.
	turnWG *sync.WaitGroup

	// workerMu guards the disposable runtime worker handle; the
	// queue itself never depends on it.
	workerMu sync.Mutex
	worker   *UserAgent
}

func newAgentState(agentID string, rootCtx context.Context, turnWG *sync.WaitGroup, logger zerolog.Logger) *agentState {
	s := &agentState{
		agentID: agentID,
		rootCtx: rootCtx,
		conns:   wsconn.NewBroadcaster(logger),
		turnWG:  turnWG,
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
func (s *agentState) drainingNow() bool { return s.draining.Load() }

// beginDrain gates the queue against new admissions and further
// claims; the active turn, if any, keeps settling.
func (s *agentState) beginDrain() { s.draining.Store(true) }

// admit records one submission and queues it. The capacity and drain
// checks run first, under the publication boundary, and the durable
// acceptance commits before the item exists in the runtime queue: a
// rejected admission writes nothing, and an accepted one is never
// acknowledged before it is durable. A paused queue stays paused —
// admission is not a resume. accept is the durable half, called with the
// boundary held but no queue mutex.
func (s *agentState) admit(itemID, content string, model *apitypes.ModelEntry, kind string, accept func(context.Context) (*backend.JournalBatch, error)) (*queuedItem, error) {
	// The drain gate is checked before the publication boundary is
	// taken: a drain must reject at once, even while a journal write
	// holds the boundary. The flag only ever flips on, so an admission
	// let through here is at worst settled by recovery next boot.
	if s.drainingNow() {
		return nil, errQueueDraining
	}

	s.pubMu.Lock()

	s.mu.Lock()
	if len(s.pending)+s.nextSlotCount() >= queueCapacity {
		s.mu.Unlock()
		s.pubMu.Unlock()
		return nil, errQueueFull
	}
	s.mu.Unlock()

	if batch, err := accept(context.Background()); err != nil {
		s.pubMu.Unlock()
		return nil, err
	} else if batch != nil {
		// The committed acceptance is published before the runtime
		// queue names the item: a live client's cursor stays
		// contiguous with replay, never skipping the positions the
		// accepted event committed.
		s.publishLifecycleLocked(batch)
	}

	s.mu.Lock()
	item := &queuedItem{id: itemID, content: content, kind: kind, model: model}
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

// publishLifecycleLocked enqueues one committed journal batch as a
// lifecycle frame. Caller holds pubMu: the frame's cursor order and the
// queue snapshots around it share one serialization, so a subscriber
// applies durable facts in commit order. Publication is best-effort —
// a device that misses frames resynchronizes from the journal, which
// is why nothing here can fail a turn.
func (s *agentState) publishLifecycleLocked(batch *backend.JournalBatch) {
	if batch == nil || len(batch.Events) == 0 {
		return
	}
	events := make([]json.RawMessage, 0, len(batch.Events))
	for _, ev := range batch.Events {
		raw, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		events = append(events, raw)
	}
	if len(events) == 0 {
		return
	}
	_ = s.conns.WriteJSON(context.Background(), wstypes.Event{
		Type:    wstypes.EventTypeLifecycle,
		AgentID: s.agentID,
		Data:    wstypes.LifecycleBatchEvent{Cursor: batch.Cursor, Events: events},
	})
}

// commitAndPublish runs one durable mutation inside the publication
// boundary so enqueue order can never diverge from commit order: the
// journal write and the lifecycle frame carrying it share one pubMu
// section. The mutation itself is the caller's — its guard queries and
// error classification stay where they run.
func (s *agentState) commitAndPublish(mutate func() (*backend.JournalBatch, error)) error {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	batch, err := mutate()
	if err != nil {
		return err
	}
	s.publishLifecycleLocked(batch)
	return nil
}

// startItem is commitAndPublish for an item's start: the committed
// batch, the running identity, and the queue snapshot land in one
// publication section, so a synchronizing device can never observe an
// active item whose identity was not published with its durable start.
func (s *agentState) startItem(mutate func() (*backend.ItemStart, *backend.JournalBatch, error)) error {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	start, batch, err := mutate()
	if err != nil {
		return err
	}
	s.publishLifecycleLocked(batch)
	s.mu.Lock()
	s.userMsgID = start.UserMessageID
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	s.publish(snapshot)
	return nil
}

// nextSlotCount reports the reserved slot as 0/1; caller holds mu.
func (s *agentState) nextSlotCount() int {
	if s.next != nil {
		return 1
	}
	return 0
}

// claim takes the next item to execute: the reserved item first — an
// explicit selection, which also clears the pause gate — then the FIFO
// head when not paused. The claim publishes nothing: the
// waiting→executing projection lands after the backend commits the
// item's start (markStarted), so no subscriber ever sees a running item
// whose user row does not exist. The turn context is installed with the
// claim so an interrupt can land before streaming starts. Returns nil
// when nothing may run now.
func (s *agentState) claim() *turnClaim {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	s.mu.Lock()
	if s.drainingNow() || s.halted {
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
	// Every claim is settled by exactly one finish: Drain waits on this
	// count instead of sampling the lane flags.
	s.turnWG.Add(1)
	s.mu.Unlock()
	return claim
}

// activateLocked installs the item as active with a fresh turn
// context; caller holds mu.
func (s *agentState) activateLocked(item *queuedItem) *turnClaim {
	ctx, cancel := context.WithCancel(s.rootCtx)
	s.active = item
	s.cancel = cancel
	// The backend assigns the user row when the item starts; commands
	// never own one.
	s.userMsgID = ""
	return &turnClaim{item: item, ctx: ctx}
}

// finish records the active turn's end and reports whether another
// item may run now. outcomeTerminalUnrecorded stops the lane for good:
// a terminal outcome nobody recorded must not be overtaken by any later
// item, so only service recovery clears it. outcomeMessageFailed arms
// the pause gate, and only while waiting work remains: the gate exists
// to hold items queued before the failure, so an empty queue never
// pauses — the next prompt runs normally. The failed turn's selection
// rides the gate so a paused send_now can rebind the entries sharing
// it. Every other outcome leaves dispatch alone.
//
// The turn's context is cancelled here: it exists for exactly one turn,
// and nothing outside it may keep work alive after settlement.
func (s *agentState) finish(outcome turnOutcome) bool {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	s.mu.Lock()
	pauseModel := s.active.model
	cancel := s.cancel
	s.active = nil
	s.cancel = nil
	s.userMsgID = ""
	switch {
	case outcome == outcomeTerminalUnrecorded:
		s.halted = true
	case outcome == outcomeMessageFailed && len(s.pending) > 0:
		s.paused = true
		s.pauseModel = pauseModel
	}
	canRun := !s.halted && (s.next != nil || (len(s.pending) > 0 && !s.paused))
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	s.publish(snapshot)
	// Cancel is idempotent and takes no other lock, so finishing the
	// turn under pubMu is safe.
	if cancel != nil {
		defer cancel()
	}
	s.turnWG.Done()
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
	// Like admission, the drain gate answers before the boundary: a
	// journal write in flight must not delay the rejection.
	if s.drainingNow() {
		return nil, errQueueDraining
	}

	s.pubMu.Lock()

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
	return *a == *b
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
	// The interrupt lands before the publication boundary is taken: a
	// durable mutation in flight runs under pubMu and may itself be
	// waiting on this turn's context, so the cancel must never queue
	// behind the boundary it is meant to release. The selection the
	// gate attributes is read after, off the settled (or settling)
	// turn — a stop is a stop whether or not the turn got there first.
	s.interruptActive()

	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	if s.drainingNow() {
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
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	s.publish(snapshot)
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

// canRunNow reports whether claim() would return an item right now:
// a reserved send-now, or FIFO head while not paused. The worker
// exit path uses it to respawn only for work that can actually run,
// so a paused queue keeps its state without a respawn loop.
func (s *agentState) canRunNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next != nil || (!s.paused && len(s.pending) > 0)
}

// queueItemWire renders one waiting item for a snapshot.
func queueItemWire(it *queuedItem) wstypes.QueueItem {
	return wstypes.QueueItem{ID: it.id, Content: it.content, Kind: it.kind}
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
