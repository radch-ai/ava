package agent_test

// Reasoning pins: reasoning is a lane fact. The streamed answer
// flips the lane's reasoning flag on at the first reasoning token
// and off when the answer or the stream ends, and the lane_status
// broadcasts carry the transitions — a join mid-thought reads the
// flag from its status reply. Reasoning text itself never rides
// the wire or lands in the thread.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// sseReasoningChunk renders one chat.completion.chunk SSE frame whose
// delta carries reasoning_content instead of content.
func sseReasoningChunk(delta string) string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"reasoning_content": delta},
		}},
	}
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// reasoningServer streams one reasoning phase then one answer delta.
func reasoningServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, frame := range []string{
			sseReasoningChunk("thinking hard "),
			sseReasoningChunk("about it"),
			sseChunk("the answer"),
		} {
			_, _ = w.Write([]byte(frame))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

// nextLaneStatus reads events until the next lane_status and returns
// its payload; turn output in between fails the pin.
func nextLaneStatus(t *testing.T, c *websocket.Conn) wstypes.LaneStatus {
	t.Helper()
	for {
		ev := readEvent(t, c)
		require.Equal(t, wstypes.EventTypeLaneStatus, ev.Type, "unexpected event")
		return eventPayload[wstypes.LaneStatus](t, ev)
	}
}

func TestTurnSignalsReasoningPhases(t *testing.T) {
	llm := reasoningServer(t)
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-reason", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	// The turn brackets itself with working; reasoning flips on at
	// the first reasoning token — before any turn output — and off
	// when the answer starts.
	started := nextLaneStatus(t, c)
	require.True(t, started.Working)
	require.False(t, started.Reasoning)

	thinking := nextLaneStatus(t, c)
	require.True(t, thinking.Reasoning)

	answering := nextLaneStatus(t, c)
	require.False(t, answering.Reasoning)

	// Only the answer streams; the settled record is content alone.
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "the answer", settled.Content)

	ended := nextLaneStatus(t, c)
	require.False(t, ended.Working)
	require.False(t, ended.Reasoning)
}

func TestReasoningSignalResetsWhenTheStreamEndsUnanswered(t *testing.T) {
	// A stream that ends after reasoning, without ever answering,
	// still lowers the flag — the turn cannot end mid-thought.
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseReasoningChunk("only thinking")))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-reason-end", "conn-1", sc,
		agent.Message{ID: "msg-2", Content: "hi"})
	require.True(t, ok)

	started := nextLaneStatus(t, c)
	require.True(t, started.Working)

	thinking := nextLaneStatus(t, c)
	require.True(t, thinking.Reasoning)

	// The turn settles with an empty answer; the reasoning flag is
	// already down by then.
	ended := nextLaneStatus(t, c)
	require.False(t, ended.Reasoning)
	_, settled := readUntilMessage(t, c)
	require.Empty(t, settled.Content)

	final := nextLaneStatus(t, c)
	require.False(t, final.Working)
	require.False(t, final.Reasoning)
}
