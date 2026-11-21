package agent_test

// Synchronization hand-off pins: a connection registered mid-turn receives
// the runtime snapshot and the live turn's prefix before the chunks still to
// come, the prefix's sequence is what the next chunk continues, and a
// hand-off never claims readiness for a position the committed head has
// already passed. This is the lane side of the protocol; the gateway's own
// tests drive the frames.
import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// readSyncReady performs one synchronization hand-off at the agent's
// committed head: the server end registers (the connection the
// gateway hands the lane) and the client end reads — the same split
// every other test uses, since wsPair's keep-alive loop already holds
// the server end's reader.
func readSyncReady(t *testing.T, mgr *agent.UserAgentManager, agentID, connID string, register, read *websocket.Conn) wstypes.SyncReadyEvent {
	t.Helper()
	journal := mgr.Journal()
	require.NotNil(t, journal)
	_, head, _, err := journal.JournalStatus(context.Background(), agentID)
	require.NoError(t, err)
	registered, _, err := mgr.HandOff(context.Background(), agentID, connID, register, head)
	require.NoError(t, err)
	require.True(t, registered)
	for {
		ev := readEvent(t, read)
		if ev.Type == wstypes.EventTypeSyncReady {
			return eventPayload[wstypes.SyncReadyEvent](t, ev)
		}
	}
}

func TestHandOffMidTurnDeliversQueueThenPrefix(t *testing.T) {
	url, release := gatedStream(t)
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread, ResolveModel: fixedResolver(url)})

	c1, sc1 := wsPair(t)
	c2, sc2 := wsPair(t)

	itemID := submit(t, mgr, "agent-join", "conn-1", sc1, "hi")

	// conn-1 confirms the first chunk is on the wire; the prefix is now
	// replayable.
	var firstSeq int64
	for {
		ev := readEvent(t, c1)
		if ev.Type == wstypes.EventTypeMessageChunk {
			firstSeq = eventPayload[wstypes.MessageChunkEvent](t, ev).Seq
			break
		}
	}
	require.EqualValues(t, 1, firstSeq, "the stream's first chunk is sequence 1")

	ready := readSyncReady(t, mgr, "agent-join", "conn-2", sc2, c2)
	close(release)

	// The ready frame carries the running item, its current text, and the
	// sequence that text ends at.
	require.NotNil(t, ready.Queue.Active)
	require.Equal(t, itemID, ready.Queue.Active.ID)
	require.NotEmpty(t, ready.Queue.Active.UserMessageID,
		"the joiner can render the running turn's user row")
	require.Empty(t, ready.Queue.Items)
	require.False(t, ready.Queue.Paused)
	require.NotNil(t, ready.Prefix)
	require.Equal(t, itemID, ready.Prefix.ItemID)
	require.Equal(t, "par ", ready.Prefix.Text)
	require.Equal(t, int64(1), ready.Prefix.LastSeq)

	// The live remainder follows the prefix: one more chunk, then the
	// terminal event.
	ev := readEvent(t, c2)
	require.Equal(t, wstypes.EventTypeMessageChunk, ev.Type)
	p := eventPayload[wstypes.MessageChunkEvent](t, ev)
	require.Equal(t, itemID, p.ID)
	require.Equal(t, "tial", p.Delta)
	require.Equal(t, int64(2), p.Seq, "the live chunk continues the prefix's sequence")

	_, msg := readUntilMessage(t, c2)
	require.Equal(t, "par tial", msg.Content)
	_ = c1
}

// A hand-off for a position the head already passed must say so, so the
// caller closes the connection instead of claiming readiness.
func TestHandOffRefusesStalePosition(t *testing.T) {
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: fixedResolver("http://unused")})
	_, sc := wsPair(t)
	// An admission advances the committed head.
	_, err := mgr.Submit("agent-stale", "hello", nil, "")
	require.NoError(t, err)

	journal := mgr.Journal()
	_, head, _, err := journal.JournalStatus(context.Background(), "agent-stale")
	require.NoError(t, err)
	require.Greater(t, head, int64(0))

	registered, _, err := mgr.HandOff(context.Background(), "agent-stale", "conn-1", sc, 0)
	require.NoError(t, err)
	require.False(t, registered, "a stale position must not register")
	require.Zero(t, agent.ConnCount(mgr, "agent-stale"))

	registered, _, err = mgr.HandOff(context.Background(), "agent-stale", "conn-1", sc, head)
	require.NoError(t, err)
	require.True(t, registered)
	require.Equal(t, 1, agent.ConnCount(mgr, "agent-stale"))
}

// Handing off to an agent with no worker and no work still subscribes: the
// queue outlives the worker.
func TestHandOffWithoutWorkerSubscribes(t *testing.T) {
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: fixedResolver("http://unused")})
	c, sc := wsPair(t)
	ready := readSyncReady(t, mgr, "agent-nolane", "conn-1", sc, c)

	require.Nil(t, agent.Worker(mgr, "agent-nolane"), "the hand-off must not spawn a worker")
	require.Empty(t, ready.Queue.Items)
	require.False(t, ready.Queue.Paused)
	require.Nil(t, ready.Prefix, "no turn runs, so there is no prefix")
	require.Equal(t, 1, agent.ConnCount(mgr, "agent-nolane"))
}

// TestHandOffChunkContiguity pins the cut-and-registration atomicity: a
// stream flushing chunks continuously while connections join must hand
// each joiner a prefix whose last sequence the very next chunk on that
// connection continues — no gap (a chunk lost between cut and
// registration), no duplicate, no close.
func TestHandOffChunkContiguity(t *testing.T) {
	stop := make(chan struct{})
	var chunks atomic.Int64
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
				return
			case <-ticker.C:
				_, _ = w.Write([]byte(sseChunk("x")))
				flusher.Flush()
				chunks.Add(1)
			}
		}
	}))
	t.Cleanup(llm.Close)
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

	c1, sc1 := wsPair(t)
	submit(t, mgr, "agent-join", "conn-1", sc1, "hi")
	for {
		if ev := readEvent(t, c1); ev.Type == wstypes.EventTypeMessageChunk {
			break // the stream is live and flushing
		}
	}
	journal := mgr.Journal()
	_, head, _, err := journal.JournalStatus(context.Background(), "agent-join")
	require.NoError(t, err)

	for i := range 50 {
		c2, sc2 := wsPair(t)
		registered, _, err := mgr.HandOff(context.Background(), "agent-join",
			fmt.Sprintf("join-%d", i), sc2, head)
		require.NoError(t, err)
		require.True(t, registered)
		var prefix *wstypes.TurnPrefix
		for prefix == nil {
			ev := readEvent(t, c2)
			if ev.Type == wstypes.EventTypeSyncReady {
				ready := eventPayload[wstypes.SyncReadyEvent](t, ev)
				prefix = ready.Prefix
			}
		}
		require.NotNil(t, prefix, "join %d: mid-turn hand-off must carry a prefix", i)
		// The first chunk after ready must continue the prefix exactly.
		last := prefix.LastSeq
		for range 3 {
			var chunk wstypes.MessageChunkEvent
			for {
				ev := readEvent(t, c2)
				if ev.Type != wstypes.EventTypeMessageChunk {
					continue
				}
				chunk = eventPayload[wstypes.MessageChunkEvent](t, ev)
				break
			}
			require.Equal(t, last+1, chunk.Seq,
				"join %d: chunk after prefix last_seq %d must be contiguous", i, last)
			last = chunk.Seq
		}
		// The pair's cleanup closes the client end; further writes to
		// the registered server end fail silently, as production does.
		require.NoError(t, c2.Close(websocket.StatusNormalClosure, "done"))
	}

	close(stop)
	_, msg := readUntilMessage(t, c1)
	require.Equal(t, strings.Repeat("x", int(chunks.Load())), msg.Content)
}
