package agent

// Queue pins: the manager-owned queue is the authority — FIFO
// order, one active item, pause on message-level failure only,
// reservation semantics, bounded admission, and mutation-ordered
// snapshots.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

func newTestState() (*agentState, *[]wstypes.QueueStateEvent) {
	s := newAgentState("agent-queue", context.Background(), &sync.WaitGroup{}, zerolog.Nop())
	recorded := &[]wstypes.QueueStateEvent{}
	s.publish = func(ev wstypes.QueueStateEvent) {
		*recorded = append(*recorded, ev)
	}
	return s, recorded
}

// testStartItem lands a committed start on the state under the same
// publication section the worker uses; nil batch = no events to publish.
func testStartItem(s *agentState, userMsgID string) {
	_ = s.startItem(func() (*backend.ItemStart, *backend.JournalBatch, error) {
		return &backend.ItemStart{UserMessageID: userMsgID}, nil, nil
	})
}

// testNextModel reports the reserved item's selection: routing stays in
// the queue, not on the snapshot.
func testNextModel(s *agentState) *apitypes.ModelEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next == nil {
		return nil
	}
	return s.next.model
}

// testPendingModel reports one waiting item's selection, oldest first.
func testPendingModel(s *agentState, i int) *apitypes.ModelEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[i].model
}

func TestQueueClaimsFIFOOneAtATime(t *testing.T) {
	s, published := newTestState()

	a, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	c, err := testAdmit(s, "C", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	require.NotSame(t, a, b)
	require.NotEqual(t, a.id, c.id)

	claim := s.claim()
	require.NotNil(t, claim)
	require.Equal(t, "A", claim.item.content)
	require.Equal(t, a.id, claim.item.id)
	// One active item only; the rest wait.
	require.Nil(t, s.claim())

	// The running projection lands after the durable start: the item
	// moves into Active — with the backend-assigned thread and user row —
	// and leaves Items in the same snapshot.
	testStartItem(s, "user-row-1")
	last := (*published)[len(*published)-1]
	require.NotNil(t, last.Active)
	require.Equal(t, a.id, last.Active.ID)
	require.Equal(t, "user-row-1", last.Active.UserMessageID)
	require.Len(t, last.Items, 2, "the claimed item left the pending list")
	require.Nil(t, last.Next)

	require.True(t, s.finish(outcomeSucceeded))
	claim = s.claim()
	require.NotNil(t, claim)
	require.Equal(t, "B", claim.item.content)
	require.True(t, s.finish(outcomeSucceeded))
	claim = s.claim()
	require.NotNil(t, claim)
	require.Equal(t, "C", claim.item.content)
	require.False(t, s.finish(outcomeSucceeded), "empty queue cannot run")
	require.Nil(t, s.claim())
}

// The claim itself publishes nothing: a client must never see a running
// item whose durable start has not committed. startItem publishes the
// committed lifecycle batch and the one running snapshot together, after
// that commit.
func TestMarkStartedPublishesRunningProjection(t *testing.T) {
	s, published := newTestState()
	item, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	before := len(*published)
	claim := s.claim()
	require.NotNil(t, claim)
	require.Len(t, *published, before, "the claim transition stays unpublished")

	testStartItem(s, "user-row-1")
	require.Len(t, *published, before+1)
	ev := (*published)[len(*published)-1]
	require.NotNil(t, ev.Active)
	require.Equal(t, item.id, ev.Active.ID)
	require.Equal(t, "user-row-1", ev.Active.UserMessageID)
	require.Empty(t, ev.Items)
	require.False(t, ev.Paused)
}

func TestQueueCapacityRejectsWithoutMutation(t *testing.T) {
	s, published := newTestState()

	for range queueCapacity {
		_, err := testAdmit(s, "msg", nil, wstypes.QueueItemKindPrompt)
		require.NoError(t, err)
	}
	before := len(*published)
	snapshot := s.stateSnapshot()

	_, err := testAdmit(s, "overflow", nil, wstypes.QueueItemKindPrompt)
	require.ErrorIs(t, err, errQueueFull)
	require.Len(t, *published, before, "a rejection publishes nothing")
	require.Equal(t, snapshot, s.stateSnapshot(), "state unchanged by rejection")
}

func TestQueuePauseOnMessageFailureOnly(t *testing.T) {
	s, _ := newTestState()

	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	claim := s.claim()
	require.NotNil(t, claim)

	// A message-level failure pauses; B stays pending, in order.
	require.False(t, s.finish(outcomeMessageFailed), "a message failure pauses automatic dispatch")
	snapshot := s.stateSnapshot()
	require.True(t, snapshot.Paused)
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, "B", snapshot.Items[0].Content)

	require.Nil(t, s.claim(), "paused queue does not claim FIFO")

	// Enqueue while paused appends without resuming.
	_, err = testAdmit(s, "C", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	require.Nil(t, s.claim())

	// A tool-level outcome and a stop do not pause: the explicit
	// selection resumes, and its non-message settlement continues
	// the FIFO.
	_, err = s.sendNow(b.id, nil)
	require.NoError(t, err)
	resumed := s.claim()
	require.NotNil(t, resumed)
	require.True(t, s.finish(outcomeSucceeded), "a non-message settlement keeps dispatch running")
	require.False(t, s.stateSnapshot().Paused)
	next := s.claim()
	require.NotNil(t, next, "unpaused FIFO continues")
	require.Equal(t, "C", next.item.content)
}

func TestQueuePauseArmsOnlyWithWaitingWork(t *testing.T) {
	s, _ := newTestState()

	// A failure with nothing waiting never arms the gate: dispatch
	// stays open and the next prompt runs on its own, fixed model
	// selection or not.
	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	claim := s.claim()
	require.NotNil(t, claim)
	require.False(t, s.finish(outcomeMessageFailed), "nothing waits — finish reports no runnable work, but not via the gate")
	require.False(t, s.stateSnapshot().Paused, "an empty queue never arms the pause")

	_, err = testAdmit(s, "B", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	next := s.claim()
	require.NotNil(t, next, "a prompt after an empty-queue failure runs normally")
	require.Equal(t, "B", next.item.content)
	require.False(t, s.finish(outcomeSucceeded))
}

func TestQueueSendNowRebindsPausedFailedModelEntries(t *testing.T) {
	s, _ := newTestState()
	m1 := &apitypes.ModelEntry{ID: "m1", ProviderID: "p"}
	m2 := &apitypes.ModelEntry{ID: "m2", ProviderID: "p"}
	m3 := &apitypes.ModelEntry{ID: "m3", ProviderID: "p"}

	a, err := testAdmit(s, "A", m1, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", m1, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	_, err = testAdmit(s, "C", m2, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	claim := s.claim()
	require.NotNil(t, claim)
	require.Equal(t, a.id, claim.item.id)
	require.False(t, s.finish(outcomeMessageFailed), "message failure pauses with work waiting")

	// The paused rebind heals every entry captured on the failed
	// selection — the reserved one included — and leaves different
	// selections alone.
	_, err = s.sendNow(b.id, m3)
	require.NoError(t, err)
	snapshot := s.stateSnapshot()
	require.NotNil(t, snapshot.Next)
	require.Equal(t, "m3", testNextModel(s).ID, "the reserved stale entry is rebound")
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, "m2", testPendingModel(s, 0).ID, "a different selection keeps its own model")
	// Already-healed entries no longer match the failed selection:
	// a repeat send_now with yet another selection leaves them on
	// the recovery model — only failed-model entries move.
	_, err = s.sendNow(b.id, &apitypes.ModelEntry{ID: "m4", ProviderID: "p"})
	require.NoError(t, err)
	require.Equal(t, "m3", testNextModel(s).ID)

	resumed := s.claim()
	require.NotNil(t, resumed)
	require.Equal(t, "m3", resumed.item.model.ID, "the claim runs the rebound selection")
}

func TestQueueSendNowOnHealthyQueueKeepsAdmissionModels(t *testing.T) {
	s, _ := newTestState()
	m1 := &apitypes.ModelEntry{ID: "m1", ProviderID: "p"}
	m2 := &apitypes.ModelEntry{ID: "m2", ProviderID: "p"}

	_, err := testAdmit(s, "A", m1, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", m2, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	claim := s.claim()
	require.NotNil(t, claim)

	// No pause: the frame's selection is ignored — the promotion
	// keeps the item's admission-time model.
	_, err = s.sendNow(b.id, &apitypes.ModelEntry{ID: "m3", ProviderID: "p"})
	require.NoError(t, err)
	snapshot := s.stateSnapshot()
	require.NotNil(t, snapshot.Next)
	require.Equal(t, "m2", testNextModel(s).ID)
}

func TestQueueStopHoldsWaitingWork(t *testing.T) {
	s, _ := newTestState()
	m1 := &apitypes.ModelEntry{ID: "m1", ProviderID: "p"}

	_, err := testAdmit(s, "A", m1, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	_, err = testAdmit(s, "B", m1, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	claim := s.claim()
	require.NotNil(t, claim)

	// Stop interrupts the active item and arms the gate: B waits,
	// FIFO claiming is closed, and the stopped selection rides the
	// gate for a paused send-now's rebind.
	s.stopNow()
	snapshot := s.stateSnapshot()
	require.True(t, snapshot.Paused)
	require.Len(t, snapshot.Items, 1)

	// The interrupted turn settles — a stop, not a failure — and the
	// held queue still claims nothing behind it.
	require.False(t, s.finish(outcomeSucceeded), "the settle reports nothing runnable — the gate holds")
	require.Nil(t, s.claim(), "a held queue claims nothing")

	_, err = s.sendNow(snapshot.Items[0].ID, &apitypes.ModelEntry{ID: "m2", ProviderID: "p"})
	require.NoError(t, err)
	resumed := s.claim()
	require.NotNil(t, resumed)
	require.Equal(t, "m2", resumed.item.model.ID, "the stop-pause rebind applies like a failure's")
	require.False(t, s.stateSnapshot().Paused)

	// Stop with nothing waiting only interrupts — no gate, ever.
	require.False(t, s.finish(outcomeSucceeded), "nothing waits — nothing runnable")
	s.stopNow()
	require.False(t, s.stateSnapshot().Paused)
}

func TestQueueSendNowReservesAndInterruptsActive(t *testing.T) {
	s, published := newTestState()

	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	_, err = testAdmit(s, "B", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	c, err := testAdmit(s, "C", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	active := s.claim()
	require.NotNil(t, active)
	require.Equal(t, "A", active.item.content)

	item, err := s.sendNow(c.id, nil)
	require.NoError(t, err)
	require.Equal(t, c.id, item.id)

	select {
	case <-active.ctx.Done():
	default:
		t.Fatal("send_now must interrupt the active item's context")
	}

	snapshot := s.stateSnapshot()
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, "B", snapshot.Items[0].Content, "remaining order preserved")
	require.NotNil(t, snapshot.Next)
	require.Equal(t, c.id, snapshot.Next.ID)

	// After the interrupted turn settles, the reserved item runs
	// next — before the FIFO remainder.
	require.True(t, s.finish(outcomeSucceeded))
	next := s.claim()
	require.NotNil(t, next)
	require.Equal(t, "C", next.item.content)
	require.True(t, s.finish(outcomeSucceeded))
	rest := s.claim()
	require.NotNil(t, rest)
	require.Equal(t, "B", rest.item.content)

	// Snapshots arrive in mutation order: three admissions, one
	// reservation, two finishes — claims publish nothing until their
	// durable start lands.
	require.Len(t, *published, 6)
}

func TestQueueSendNowRepeatAndConflicts(t *testing.T) {
	s, _ := newTestState()

	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	c, err := testAdmit(s, "C", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	first, err := s.sendNow(c.id, nil)
	require.NoError(t, err)

	again, err := s.sendNow(c.id, nil)
	require.NoError(t, err, "repeat for the reserved item is an ack")
	require.Equal(t, first.id, again.id)

	_, err = s.sendNow(b.id, nil)
	require.ErrorIs(t, err, errReservationHeld)

	_, err = s.sendNow("missing", nil)
	require.ErrorIs(t, err, errItemNotPending)

	// A conflicting or stale selection mutates nothing.
	snapshot := s.stateSnapshot()
	require.Len(t, snapshot.Items, 2)
	require.Equal(t, c.id, snapshot.Next.ID)
}

func TestQueueReservedClaimClearsPause(t *testing.T) {
	s, published := newTestState()

	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	claim := s.claim()
	require.NotNil(t, claim)
	require.False(t, s.finish(outcomeMessageFailed), "message failure pauses automatic dispatch")
	require.Nil(t, s.claim())

	_, err = s.sendNow(b.id, nil)
	require.NoError(t, err)
	resumed := s.claim()
	require.NotNil(t, resumed, "the explicit selection lifts the pause")
	require.Equal(t, "B", resumed.item.content)
	require.False(t, s.stateSnapshot().Paused)
	testStartItem(s, "")
	require.False(t, (*published)[len(*published)-1].Paused,
		"the running projection carries the lifted pause")
}

func TestQueueStopKeepsArmedGateModel(t *testing.T) {
	s, _ := newTestState()
	m1 := &apitypes.ModelEntry{ID: "m1", ProviderID: "p"}
	m2 := &apitypes.ModelEntry{ID: "m2", ProviderID: "p"}
	m3 := &apitypes.ModelEntry{ID: "m3", ProviderID: "p"}

	_, err := testAdmit(s, "A", m1, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", m1, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	_, err = testAdmit(s, "C", m2, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	require.NotNil(t, s.claim())
	require.False(t, s.finish(outcomeMessageFailed), "message failure pauses with work waiting")

	// A stop arriving after the turn settled (no active item) must
	// not rewrite the armed gate: the failed selection stays
	// attributed, so the recovery rebind still targets it.
	s.stopNow()
	require.True(t, s.stateSnapshot().Paused)

	_, err = s.sendNow(b.id, m3)
	require.NoError(t, err)
	snapshot := s.stateSnapshot()
	require.NotNil(t, snapshot.Next)
	require.Equal(t, "m3", testNextModel(s).ID, "the failed-model entry still rebinds")
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, "m2", testPendingModel(s, 0).ID, "a different selection keeps its own model")
}

// A finished turn's context is cancelled: it exists for exactly one
// turn, so nothing outside settlement can keep work alive on it.
func TestQueueFinishCancelsTurnContext(t *testing.T) {
	s, _ := newTestState()
	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	claim := s.claim()
	require.NotNil(t, claim)
	require.NoError(t, claim.ctx.Err())

	s.finish(outcomeSucceeded)
	require.ErrorIs(t, claim.ctx.Err(), context.Canceled)
}

func TestQueueDrainGatesAdmissionAndClaims(t *testing.T) {
	s, _ := newTestState()

	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	b, err := testAdmit(s, "B", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	s.beginDrain()
	_, err = testAdmit(s, "C", nil, wstypes.QueueItemKindPrompt)
	require.ErrorIs(t, err, errQueueDraining)
	_, err = s.sendNow(b.id, nil)
	require.ErrorIs(t, err, errQueueDraining)

	// The active turn keeps settling, but nothing new claims.
	claim := s.claim()
	require.Nil(t, claim)
}

func TestQueueTurnContextDescendsFromRoot(t *testing.T) {
	root, rootCancel := context.WithCancel(context.Background())
	s := newAgentState("agent-root", root, &sync.WaitGroup{}, zerolog.Nop())

	_, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	claim := s.claim()
	require.NotNil(t, claim)

	rootCancel()
	select {
	case <-claim.ctx.Done():
	default:
		t.Fatal("turn context must descend from the manager's root")
	}
}

func TestQueueErrorsAreDistinct(t *testing.T) {
	distinct := []error{errQueueDraining, errQueueFull, errReservationHeld, errItemNotPending}
	for i, a := range distinct {
		for j, b := range distinct {
			if i != j {
				require.False(t, errors.Is(a, b))
			}
		}
	}
}
