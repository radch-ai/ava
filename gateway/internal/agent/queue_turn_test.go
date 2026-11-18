package agent_test

// Send-immediately pins at the turn level: promoting a pending item
// interrupts the active turn, the interrupted turn settles first,
// and the selected item executes next — once — before the remaining
// FIFO, which then continues on its own.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// parkedStream flushes one delta, then parks until the request
// context dies — the send-now interrupt is the only release.
func parkedStream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("slow ")))
		flusher.Flush()
		<-r.Context().Done()
	}))
}

// quickStream answers one short turn immediately.
func quickStream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("quick answer")))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

// TestClaimSnapshotPrecedesFirstChunk pins the ordering the UI
// depends on: the queue publishes the waiting→executing transition —
// the item's persisted user-row identity included — before any
// streamed output, so a subscriber shows the user's message
// immediately instead of waiting for the turn to settle.
func TestClaimSnapshotPrecedesFirstChunk(t *testing.T) {
	quick := quickStream(t)
	defer quick.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread, ResolveModel: fixedResolver(quick.URL)})

	c, sc := wsPair(t)
	itemID := submit(t, mgr, "agent-claim-order", "conn-1", sc, "visible early")

	for {
		ev := readEvent(t, c)
		if ev.Type == wstypes.EventTypeMessageChunk {
			t.Fatal("the first chunk arrived before the claim snapshot")
		}
		if ev.Type != wstypes.EventTypeQueueState {
			continue
		}
		snapshot := eventPayload[wstypes.QueueStateEvent](t, ev)
		if snapshot.Active == nil {
			continue
		}
		require.Equal(t, itemID, snapshot.Active.ID)
		require.NotEmpty(t, snapshot.Active.UserMessageID,
			"the claim snapshot names the persisted user row")
		return
	}
}

func TestSendNowInterruptsActiveAndRunsSelectedNext(t *testing.T) {
	parked := parkedStream(t)
	defer parked.Close()
	quick := quickStream(t)
	defer quick.Close()

	var quickHits atomic.Int32
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		if entry != nil && entry.ID == "quick" {
			quickHits.Add(1)
			return backend.ResolvedModel{BaseURL: quick.URL, APIKey: "sk-test", Model: "test-model"}, nil
		}
		return backend.ResolvedModel{BaseURL: parked.URL, APIKey: "sk-test", Model: "test-model"}, nil
	}

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread, ResolveModel: resolver})

	c, sc := wsPair(t)

	// A parks mid-stream; B and C wait behind it, both quick.
	submit(t, mgr, "agent-sendnow", "conn-1", sc, "slow question")
	_, err := mgr.Submit("agent-sendnow", "conn-1", sc,
		"b waits", &apitypes.ModelEntry{ID: "quick"})
	require.NoError(t, err)
	cID, err := mgr.Submit("agent-sendnow", "conn-1", sc,
		"c chosen", &apitypes.ModelEntry{ID: "quick"})
	require.NoError(t, err)

	// The parked turn's first chunk is on the wire.
	for {
		ev := readEvent(t, c)
		if ev.Type == wstypes.EventTypeMessageChunk {
			break
		}
	}

	// Promote C: the reservation is acknowledged.
	promoted, err := mgr.SendNow("agent-sendnow", cID, nil)
	require.NoError(t, err)
	require.Equal(t, cID, promoted)

	// The parked turn settles its partial as the final message —
	// send-now is a stop, not a failure.
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "slow ", settled.Content)

	// C runs next — its quick answer arrives before B's — and the
	// queue then continues on its own: B follows with no further
	// control frame.
	_, chosen := readUntilMessage(t, c)
	require.Equal(t, "quick answer", chosen.Content)
	_, trailing := readUntilMessage(t, c)
	require.Equal(t, "quick answer", trailing.Content)
	require.Equal(t, int32(2), quickHits.Load(), "the selected item ran exactly once")
}
