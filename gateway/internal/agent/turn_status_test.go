package agent_test

// Working-state pins: the worker brackets every turn with
// lane_status events carrying working — true before anything the
// turn emits, false after the final event — so the composer's stop
// control can track it. Queue snapshots may interleave; they are not
// turn output.

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

func TestTurnBroadcastsWorkingTransitions(t *testing.T) {
	llm := fakeOpenAI(t, "working done")

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

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
