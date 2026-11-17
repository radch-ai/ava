package agent_test

// Mid-turn join pins: a connection attached while a turn streams
// first receives the queue snapshot, then the turn's accumulated
// text as one replay chunk — ordered before the chunks still to
// come — so a page reopened mid-turn isn't frozen out of the live
// tail. Joining an agent without a worker still subscribes: the
// queue outlives the worker.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// gatedStream serves a model that flushes "par ", parks until
// released, then streams "tial" and finishes.
func gatedStream(t *testing.T) (url string, release chan struct{}) {
	t.Helper()
	release = make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("par ")))
		flusher.Flush()
		<-release
		_, _ = w.Write([]byte(sseChunk("tial")))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(llm.Close)
	return llm.URL, release
}

func TestJoinMidTurnReplaysQueueThenPrefix(t *testing.T) {
	url, release := gatedStream(t)

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(url)})

	c1, sc1 := wsPair(t)
	c2, sc2 := wsPair(t)

	itemID := submit(t, mgr, "agent-join", "conn-1", sc1, "hi")

	// conn-1 confirms the first chunk is on the wire; the prefix is
	// now replayable.
	for {
		ev := readEvent(t, c1)
		if ev.Type == wstypes.EventTypeMessageChunk {
			break
		}
	}

	mgr.Join("agent-join", "conn-2", sc2)
	close(release)

	// The joiner sees the queue snapshot first — the running item is
	// active, not lost — then the replayed prefix, the live
	// remainder, the final message, and the turn-end status, in
	// order.
	snapshot := readQueueState(t, c2)
	require.NotNil(t, snapshot.Active)
	require.Equal(t, itemID, snapshot.Active.ID)
	require.NotEmpty(t, snapshot.Active.UserMessageID,
		"the joiner can render the running turn's user row")
	require.Empty(t, snapshot.Items)
	require.False(t, snapshot.Paused)

	ev := readEvent(t, c2)
	require.Equal(t, wstypes.EventTypeMessageChunk, ev.Type)
	p := eventPayload[wstypes.MessageChunkEvent](t, ev)
	require.Equal(t, itemID, p.ID)
	require.Equal(t, "par ", p.Delta)

	ev = readEvent(t, c2)
	require.Equal(t, wstypes.EventTypeMessageChunk, ev.Type)
	p = eventPayload[wstypes.MessageChunkEvent](t, ev)
	require.Equal(t, "tial", p.Delta)

	ev, msg := readUntilMessage(t, c2)
	require.Equal(t, "agent-join", ev.AgentID)
	require.Equal(t, "par tial", msg.Content)

	last := readEvent(t, c2)
	require.Equal(t, wstypes.EventTypeLaneStatus, last.Type)
	ended := eventPayload[wstypes.LaneStatus](t, last)
	require.False(t, ended.Working)
}

// TestJoinWithoutWorkerSubscribesToQueue pins the subscribe-first
// lifecycle: a joiner arriving before any prompt — or after the
// worker's idle exit — attaches to the queue state and receives its
// snapshot without a worker ever running.
func TestJoinWithoutWorkerSubscribesToQueue(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver("http://unused")})

	c, sc := wsPair(t)
	mgr.Join("agent-nolane", "conn-1", sc)

	require.Nil(t, mgr.Get("agent-nolane"), "join must not spawn a worker")

	// The empty snapshot is on the wire for the joiner.
	snapshot := readQueueState(t, c)
	require.Empty(t, snapshot.Items)
	require.False(t, snapshot.Paused)

	require.Equal(t, 1, mgr.Status("agent-nolane").ConnCount)
}
