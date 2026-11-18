package agent_test

// Drain pins: shutdown must not kill a running turn mid-persist —
// Drain interrupts every lane, and each interrupted turn settles its
// partial answer into the thread before the process is free to exit.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
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
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			PersistTurn:    sink.persist,
		})

	c, sc := wsPair(t)

	itemID, err := mgr.Submit("agent-drain", "conn-1", sc, "hi", nil)
	require.NoError(t, err)

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
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "par ",
			TurnID: itemID}},
		calls[1])

	require.False(t, mgr.Status("agent-drain").Working)
}

// Drain ends the queue for good: a drained worker exits with the
// manager's root context, and no replacement spawns behind it — the
// gate would hold every claim anyway.
func TestDrainStopsWorkersWithoutRespawn(t *testing.T) {
	failing := func(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("no default model set")
	}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: failing})

	c, sc := wsPair(t)

	// The first turn fails instantly, pausing with the second item
	// waiting: a live worker with work it can never claim.
	submit(t, mgr, "agent-drainstop", "conn-1", sc, "hi")
	submit(t, mgr, "agent-drainstop", "conn-1", sc, "again")

	for settled := false; !settled; {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeQueueState {
			continue
		}
		settled = eventPayload[wstypes.QueueStateEvent](t, ev).Paused
	}
	// The paused queue has already released its worker — nothing
	// may run — and its state waits for Drain without a respawn
	// loop.
	require.Nil(t, mgr.Get("agent-drainstop"),
		"a paused queue with waiting work holds no worker")

	mgr.Drain(time.Second)

	require.Eventually(t, func() bool {
		return mgr.Get("agent-drainstop") == nil
	}, 2*time.Second, 20*time.Millisecond,
		"a drained worker exits with the manager root and no replacement spawns")
}
