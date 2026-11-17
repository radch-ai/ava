package agent_test

// Lifecycle pins: the pause gate arms only while work waits — a
// paused queue never drops or auto-runs pending work and keeps its
// worker — while an empty-queue failure never pauses; the
// manager-owned state (pause flag, subscribers) survives the
// worker's idle eviction, with an explicit selection restarting
// execution.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// failThenQuick resolver: fails until flipped, then serves the given
// SSE endpoint.
type failThenQuick struct {
	quick atomic.Bool
	url   string
}

func (f *failThenQuick) resolve(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
	if f.quick.Load() {
		return backend.ResolvedModel{BaseURL: f.url, APIKey: "sk-test", Model: "test-model"}, nil
	}
	return backend.ResolvedModel{}, context.DeadlineExceeded
}

func TestPausedPendingWorkStaysIntact(t *testing.T) {
	quick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("resumed")))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer quick.Close()
	r := &failThenQuick{url: quick.URL}

	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{
		ResolveModel: r.resolve,
		IdleTimeout:  50 * time.Millisecond,
	})

	c, sc := wsPair(t)

	// A claims and fails at message level while B already waits
	// behind it — the gate arms because work was queued before the
	// failure.
	submit(t, mgr, "agent-paused", "conn-1", sc, "fails")
	bID, err := mgr.Submit("agent-paused", "conn-1", sc, "survivor", nil)
	require.NoError(t, err)
	readUntilError(t, c)
	snapshot := readQueueState(t, c)
	require.True(t, snapshot.Paused)
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, bID, snapshot.Items[0].ID, "B stays pending behind the gate")

	// The waiting work keeps the worker, but nothing runs on its own.
	time.Sleep(150 * time.Millisecond)
	require.NotNil(t, mgr.Get("agent-paused"), "waiting work keeps the worker")

	// The explicit selection resumes: B runs.
	r.quick.Store(true)
	_, err = mgr.SendNow("agent-paused", bID, nil)
	require.NoError(t, err)
	_, p := readUntilMessage(t, c)
	require.Equal(t, "resumed", p.Content)
}

func TestEmptyQueueFailureNeverPausesAndStateSurvivesEviction(t *testing.T) {
	r := &failThenQuick{url: "http://unused"}
	resolverCalls := atomic.Int32{}
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		resolverCalls.Add(1)
		return r.resolve(ctx, entry)
	}

	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{
		ResolveModel: resolver,
		IdleTimeout:  50 * time.Millisecond,
	})

	c, sc := wsPair(t)

	// A message-level failure with nothing waiting never arms the
	// pause gate: the closing snapshot is unpaused, and the attached
	// connection keeps the manager-owned state alive past the
	// worker's idle exit.
	submit(t, mgr, "agent-evict", "conn-1", sc, "fails")
	readUntilError(t, c)
	snapshot := readQueueState(t, c)
	require.False(t, snapshot.Paused, "an empty queue never pauses")
	time.Sleep(150 * time.Millisecond)
	require.Nil(t, mgr.Get("agent-evict"), "the quiet queue lets the worker idle out")
	require.Equal(t, 1, mgr.Status("agent-evict").ConnCount)

	// A later prompt runs normally after the failure — no explicit
	// selection needed — respawning a worker: it fails too, and with
	// nothing behind it the queue still does not pause.
	_, err := mgr.Submit("agent-evict", "conn-1", sc, "after eviction", nil)
	require.NoError(t, err)
	readUntilError(t, c)
	require.Equal(t, int32(2), resolverCalls.Load(), "the prompt ran without an explicit selection")
	snapshot = readQueueState(t, c)
	require.False(t, snapshot.Paused)
	require.Empty(t, snapshot.Items)
}

func TestStopInterruptsAndHoldsQueue(t *testing.T) {
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
		&agent.UserAgentManagerOptions{ResolveModel: resolver})

	c, sc := wsPair(t)

	// A parks mid-stream with B waiting behind it.
	submit(t, mgr, "agent-stop", "conn-1", sc, "slow question")
	bID, err := mgr.Submit("agent-stop", "conn-1", sc,
		"b waits", &apitypes.ModelEntry{ID: "quick"})
	require.NoError(t, err)
	for {
		ev := readEvent(t, c)
		if ev.Type == wstypes.EventTypeMessageChunk {
			break
		}
	}

	// Stop: the gate arms first — the paused snapshot is on the wire
	// before the interrupted turn settles.
	mgr.Interrupt("agent-stop")
	snapshot := readQueueState(t, c)
	require.True(t, snapshot.Paused)
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, bID, snapshot.Items[0].ID)

	// The parked turn settles its partial — a stop, not a failure —
	// and B does not run behind it.
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "slow ", settled.Content)
	require.Zero(t, quickHits.Load(), "the held queue ran nothing behind the stop")

	// Send immediately is the resume: B runs on its own selection.
	_, err = mgr.SendNow("agent-stop", bID, nil)
	require.NoError(t, err)
	_, resumed := readUntilMessage(t, c)
	require.Equal(t, "quick answer", resumed.Content)
	require.Equal(t, int32(1), quickHits.Load())
}

// slowSSE delays its first byte, making one turn outlast the idle
// timeout.
func slowSSE(t *testing.T, delay time.Duration, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk(reply)))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

// A turn outlasting the idle timeout must not leave a stale timer
// fire behind: the worker exits only after a full fresh idle interval
// with no work, never immediately at the turn's end.
func TestLongTurnDoesNotCauseImmediateIdleExit(t *testing.T) {
	llm := slowSSE(t, 200*time.Millisecond, "slow reply")
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{
		ResolveModel: fixedResolver(llm.URL),
		IdleTimeout:  60 * time.Millisecond,
	})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-long", "conn-1", sc, "slow")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "slow reply", p.Content)

	// The turn ran 200 ms against a 60 ms idle timer, so a fire was
	// pending the whole time. The worker must still be here.
	require.NotNil(t, mgr.Get("agent-long"),
		"a stale idle fire must not evict the worker at turn end")

	// And it exits only after a fresh idle interval with no work.
	require.Eventually(t, func() bool {
		return mgr.Get("agent-long") == nil
	}, 2*time.Second, 10*time.Millisecond, "worker must exit after a fresh idle interval")
}
