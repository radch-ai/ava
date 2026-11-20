package agent_test

// Working-state pins: the worker brackets every turn with
// lane_status events carrying working — true before anything the
// turn emits, false after the final event — so the composer's stop
// control can track it. Queue snapshots may interleave; they are not
// turn output.

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

func TestTurnBroadcastsWorkingTransitions(t *testing.T) {
	llm := fakeOpenAI(t, "working done")

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-working", "conn-1", sc, "hi")

	// The turn announces itself before any turn output; the
	// admission snapshot may precede the announcement.
	first := readEvent(t, c)
	require.Contains(t,
		[]wstypes.EventType{wstypes.EventTypeLaneStatus, wstypes.EventTypeQueueState},
		first.Type, "unexpected leading event %v", first.Type)
	started := nextLaneStatus(t, c)
	require.True(t, started.Working)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "working done", p.Content)

	// And reports the turn ended after the final event.
	last := nextLaneStatus(t, c)
	require.False(t, last.Working)
}

// The worker's exit lowers the lane after the final turn bracket:
// the bracket's active stays true while the worker lives, so the
// strip would show a live lane forever without an exit broadcast.
func TestWorkerExitLowersLane(t *testing.T) {
	llm := fakeOpenAI(t, "done")
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-exit", "conn-1", sc, "hi")

	_, p := readUntilMessage(t, c)
	require.Equal(t, "done", p.Content)

	// The bracket first: working lowers while the worker still lives.
	bracket := nextLaneStatus(t, c)
	require.False(t, bracket.Working)
	require.True(t, bracket.Active)

	// Then the worker leaves: the lane reports inactive without a
	// status request. Queue traffic may interleave.
	deadline := time.Now().Add(2 * time.Second)
	for {
		ev, arrived := readEventMaybe(t, c, time.Until(deadline))
		if !arrived {
			t.Fatal("worker exit did not lower the lane")
		}
		if ev.Type != wstypes.EventTypeLaneStatus {
			continue
		}
		status := eventPayload[wstypes.LaneStatus](t, ev)
		require.False(t, status.Working, "inactive lane cannot be working")
		if !status.Active {
			return
		}
	}
}
