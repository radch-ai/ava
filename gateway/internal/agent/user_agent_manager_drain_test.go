package agent_test

// Drain pins: shutdown must not kill a running turn mid-persist —
// Drain interrupts every lane, and each interrupted turn settles its
// partial answer into the thread before the process is free to exit.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

func TestDrainInterruptsAndSettlesTurns(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("par ")))
		flusher.Flush()
		// Park until the drain's interrupt cancels the request.
		<-r.Context().Done()
	}))
	defer llm.Close()

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-drain", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	// The turn is streaming its partial; wait for it to reach the
	// wire so there is something worth settling.
	for {
		ev := readEvent(t, c)
		if ev.Type == wstypes.EventTypeMessageChunk {
			break
		}
	}

	mgr.Drain(3 * time.Second)

	// The interrupted turn settled: the partial became the final
	// message, and it persisted before Drain returned.
	_, p := readUntilMessage(t, c)
	require.Equal(t, "par ", p.Content)

	calls := sink.recorded()
	require.Len(t, calls, 2)
	require.Equal(t,
		[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "par "}},
		calls[1])

	require.False(t, mgr.Get("agent-drain").LaneStatus().Working)
}
