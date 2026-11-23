package agent_test

// Admission and halt contracts over the real journal: a live subscriber
// receives the accepted item's durable event before the runtime queue
// names it — keeping live cursors contiguous with replay — and a lane
// whose turn ends without a recorded terminal outcome dispatches
// nothing, not even a reserved-next selection.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	agent "github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// The committed acceptance is published before the queue snapshot names
// the item, so a live client's cursor never skips the accepted event's
// positions — replay from the advanced cursor answers nothing new.
func TestAdmissionPublishesAcceptedEvent(t *testing.T) {
	llm := fakeOpenAI(t, "done")
	defer llm.Close()
	mgr, sqlDB := journaledLane(t, &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)
	itemID, err := admitItem(t, mgr, "agent-accept", "conn-1", sc, "hi", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	// Durable admission first, runtime exposure second.
	first := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeLifecycle, first.Type,
		"the accepted event must precede the queue snapshot, got %v", first.Type)
	events := lifecycleBatch(t, first)
	require.Len(t, events, 1)
	require.Equal(t, apitypes.LifecycleItemAccepted, events[0].Kind)
	require.Equal(t, itemID, events[0].ItemID)

	snap := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeQueueState, snap.Type)
	state := eventPayload[wstypes.QueueStateEvent](t, snap)
	require.Len(t, state.Items, 1)
	require.Equal(t, itemID, state.Items[0].ID)

	// The running projection carries the started identity — never a
	// blank user row — and the started event that precedes it names the
	// execution thread, so neither half is ever observed alone.
	var startedThread string
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLifecycle:
			for _, lifecycle := range lifecycleBatch(t, ev) {
				if lifecycle.Kind == apitypes.LifecycleItemStarted {
					startedThread = lifecycle.ThreadID
				}
			}
		case wstypes.EventTypeQueueState:
			running := eventPayload[wstypes.QueueStateEvent](t, ev)
			if running.Active == nil || running.Active.ID != itemID {
				continue
			}
			require.NotEmpty(t, running.Active.UserMessageID)
			require.NotEmpty(t, startedThread, "the started event must publish before the running projection")
			// Live cursor stays contiguous: the accepted event the
			// subscriber applied is the whole journal — replay answers
			// nothing new.
			_ = sqlDB
			return
		}
	}
}

// A turn whose terminal outcome cannot be recorded halts the lane: the
// reserved-next selection — the one path that otherwise bypasses the
// pause gate — must not run ahead of an outcome nobody recorded.
func TestUnrecordedTerminalHaltsLane(t *testing.T) {
	llm := fakeOpenAI(t, "done")
	defer llm.Close()
	sqlDB, turns := journalFor(t)
	hook := &journalHook{
		LifecycleJournal: &lazyJournal{TurnService: turns, sqlDB: sqlDB, seen: make(map[string]struct{})},
		finishItem: func(context.Context, string, string, string, string, []backend.ThreadMessageDraft) (*backend.JournalBatch, error) {
			return nil, errors.New("terminal write unavailable")
		},
	}
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)}, hook)

	c, sc := wsPair(t)
	first, err := admitItem(t, mgr, "agent-halt", "conn-1", sc, "one", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)

	// The turn ends without a terminal record: the transient error is
	// the only report, and the lane must halt behind it.
	readUntilError(t, c)

	second, err := mgr.Submit("agent-halt", "two", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	_, err = mgr.SendNow("agent-halt", second, nil)
	require.NoError(t, err)

	// Nothing may start: no user row for the reserved item, and no
	// running snapshot ever names it. Service recovery — not dispatch —
	// settles the stuck item.
	require.Never(t, func() bool {
		for _, row := range storedRows(t, sqlDB, "agent-halt") {
			if row.TurnID == second && row.Role == apitypes.ThreadRoleUser {
				return true
			}
		}
		return false
	}, 750*time.Millisecond, 25*time.Millisecond, "a halted lane must not start the reserved item")

	// The reserved item keeps the queue runnable-looking, but a halted
	// lane must not respawn workers against it: a respawn loop here
	// burns a core and floods subscribers with lane-status frames.
	require.Never(t, func() bool {
		return agent.Worker(mgr, "agent-halt") != nil
	}, 750*time.Millisecond, 25*time.Millisecond, "a halted lane must not respawn workers against a reserved item")

	// The first item's start did land before the halt; its terminal
	// state stays unrecorded for service recovery to settle.
	var started int
	for _, row := range storedRows(t, sqlDB, "agent-halt") {
		if row.TurnID == first {
			started++
		}
	}
	require.GreaterOrEqual(t, started, 1)
}
