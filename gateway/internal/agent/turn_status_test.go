package agent_test

// Working-state pins: the lane brackets every turn with lane_status
// events carrying working — true before anything the turn emits,
// false after the final event — so the composer's stop control can
// track it.

import (
	"context"
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

	ok := mgr.SubmitMsg(context.Background(), "agent-working", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	// The turn announces itself before any turn output.
	first := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeLaneStatus, first.Type)
	started := eventPayload[wstypes.LaneStatus](t, first)
	require.True(t, started.Working)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "working done", p.Content)

	// And reports the turn ended after the final event.
	last := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeLaneStatus, last.Type)
	ended := eventPayload[wstypes.LaneStatus](t, last)
	require.False(t, ended.Working)
}
