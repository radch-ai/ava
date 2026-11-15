package agent_test

// Mid-turn join pins: a connection attached while a turn streams
// first receives the turn's accumulated text as one replay chunk —
// ordered before the chunks still to come — so a page reopened
// mid-turn isn't frozen out of the live tail.

import (
	"context"
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

func TestJoinMidTurnReplaysPrefix(t *testing.T) {
	url, release := gatedStream(t)

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(url)})

	c1, sc1 := wsPair(t)
	c2, sc2 := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-join", "conn-1", sc1,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

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

	// The joiner sees the replayed prefix, the live remainder, the
	// final message, and the turn-end status — in order.
	ev := readEvent(t, c2)
	require.Equal(t, wstypes.EventTypeMessageChunk, ev.Type)
	p := eventPayload[wstypes.MessageChunkEvent](t, ev)
	require.Equal(t, "msg-1", p.ID)
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

func TestJoinWithoutLaneIsNoOp(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver("http://unused")})

	_, sc := wsPair(t)
	// No lane exists for this agent; joining must not create one.
	mgr.Join("agent-nolane", "conn-1", sc)
	require.Nil(t, mgr.Get("agent-nolane"))
}
