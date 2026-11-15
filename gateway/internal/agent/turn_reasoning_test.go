package agent_test

// Reasoning pins: models that think before answering stream
// reasoning tokens alongside content; the wire carries them on the
// same chunk event (reasoning field), correlated by turn id, while
// the settled answer stays content-only — reasoning is live-only.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestTurnStreamsReasoningDeltas(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-reason", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	var reasoning strings.Builder
	var content strings.Builder
	var settled *wstypes.MessageEvent
	for settled == nil {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus:
			continue
		case wstypes.EventTypeMessageChunk:
			p := eventPayload[wstypes.MessageChunkEvent](t, ev)
			require.Equal(t, "msg-1", p.ID)
			reasoning.WriteString(p.Reasoning)
			content.WriteString(p.Delta)
		case wstypes.EventTypeMessage:
			settled = new(wstypes.MessageEvent)
			raw, _ := json.Marshal(ev.Data)
			require.NoError(t, json.Unmarshal(raw, settled))
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}

	require.Equal(t, "thinking hard about it", reasoning.String())
	require.Equal(t, "the answer", content.String())

	// The settled answer carries content only — reasoning is
	// live-only and never lands in the thread.
	require.Equal(t, "the answer", settled.Content)
}
