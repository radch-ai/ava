package agent_test

// The pre-prompt automation end to end, through the real lane: the
// trigger matrix (compact unconditionally at the whole-context
// threshold, tool-mass shake below it, silent zero-elided exit), the
// single-commit prompt, restart replay, the degrades, the stop
// skip-once, the lane-status lifecycle, command exemption, and the
// send-now overtake.

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// countingThreadSource wraps the backend's thread read, counting every
// call — the one-fetch pin and the checker-counting tests' meter.
func countingThreadSource(svc *backend.TurnService, reads *atomic.Int32) func(context.Context, string) ([]backend.AgentThreadMessage, error) {
	return func(ctx context.Context, agentID string) ([]backend.AgentThreadMessage, error) {
		reads.Add(1)
		return svc.GetAgentThread(ctx, agentID)
	}
}

// laneStatusUntil reads events until one lane_status satisfies pred and
// returns it; every status seen along the way is also appended to seen.
func laneStatusUntil(t *testing.T, c *websocket.Conn, pred func(wstypes.LaneStatus) bool, seen *[]wstypes.LaneStatus) wstypes.LaneStatus {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeLaneStatus {
			continue
		}
		status := eventPayload[wstypes.LaneStatus](t, ev)
		if seen != nil {
			*seen = append(*seen, status)
		}
		if pred(status) {
			return status
		}
	}
}

// parkedSummaryLLM serves streamed prompt turns immediately but parks
// every non-streaming (summarization) request until released — the
// checker's model call is genuinely in flight while the test acts.
func parkedSummaryLLM(t *testing.T, answer string) (url string, parked chan struct{}, release func()) {
	t.Helper()
	parked = make(chan struct{})
	releaseCh := make(chan struct{})
	var releaseOnce sync.Once
	release = func() { releaseOnce.Do(func() { close(releaseCh) }) }
	var once atomic.Bool
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if !strings.Contains(string(body), `"stream":true`) {
			if once.CompareAndSwap(false, true) {
				close(parked)
			}
			<-releaseCh
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"parked summary"},"finish_reason":"stop"}]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk(answer)))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(func() {
		release()
		llm.Close()
	})
	return llm.URL, parked, release
}

// ≥ 70% of the window compacts unconditionally: no shake runs first
// (every elided flag stays 0 — the in-memory reduction is the compact
// engine's own), the rotation commits with its divider and seeded
// summary, the prompt's user row lands once in the successor, and the
// model receives summary-then-prompt in transcript order.
func TestPrePromptCompactAtThresholdRotatesUnconditionally(t *testing.T) {
	bulk := strings.Repeat("measurement observation record ", 140)
	sqlDB, svc := contextBackend(t, "agent-ppcompact")
	seedConversation(t, sqlDB, "agent-ppcompact", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: bulk},
		{turnID: "t2", callID: "call-2", rowID: "row-new", name: "fetch", result: bulk},
	})
	llm := newCompactLLM(t, []string{sseChunk("the answer")}, []string{"auto summary"})
	mgr := laneCompactManager(t, llm.url, svc, 1500, nil)
	before := activeThreadID(t, sqlDB, "agent-ppcompact")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-ppcompact", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "the answer", settled.Content)

	// The rotation committed: compact-kind successor, divider, seeded
	// summary, pointer moved.
	successor := activeThreadID(t, sqlDB, "agent-ppcompact")
	require.NotEqual(t, before, successor)
	var kind, previous string
	require.NoError(t, sqlDB.QueryRow(
		`SELECT kind, previous_thread_id FROM threads WHERE agent_id = 'agent-ppcompact' AND id = ?`,
		successor).Scan(&kind, &previous))
	require.Equal(t, "compact", kind)
	require.Equal(t, before, previous)
	divider := threadChangedDivider(t, svc, "agent-ppcompact", successor)
	require.NotNil(t, divider)
	require.Equal(t, "Compacted", divider.Content)

	// No shake ran before the compaction — every stored row keeps
	// its payload.
	require.Equal(t, map[string]int{"row-old": 0, "row-new": 0},
		toolStates(t, sqlDB, "agent-ppcompact"))

	// The prompt committed exactly once, into the successor, and the
	// answer followed it there.
	rows := compactRows(t, sqlDB, "agent-ppcompact", successor)
	require.Len(t, rows, 3)
	require.Equal(t, [2]string{"assistant", "auto summary"}, rows[0])
	require.Equal(t, [2]string{"user", "continue"}, rows[1])
	require.Equal(t, [2]string{"assistant", "the answer"}, rows[2])
	require.Equal(t, apitypes.ItemCompleted, itemStates(t, sqlDB, "agent-ppcompact")[prompt])

	// The provider saw the summarization first, then the turn whose
	// transcript leads with the summary ahead of the prompt.
	bodies := llm.received()
	require.Len(t, bodies, 2)
	require.Contains(t, bodies[0], "compressing an agent conversation")
	require.Contains(t, bodies[1], "auto summary")
	require.Contains(t, bodies[1], "continue")
	require.Less(t, strings.Index(bodies[1], "auto summary"), strings.Index(bodies[1], "continue"),
		"the turn's transcript carries summary before prompt")
}

// Below the compact threshold with tool mass over the shake line, the
// durable shake runs: old oversized rows elide, the protected newest
// turn keeps its payload, no rotation happens, and the turn's request
// carries the restore placeholder instead of the elided payload.
func TestPrePromptShakeBelowCompactThreshold(t *testing.T) {
	oldBulk := strings.Repeat("measurement observation record ", 200)
	newBulk := strings.Repeat("fresh protected payload ", 200)
	sqlDB, svc := contextBackend(t, "agent-ppshake")
	seedConversation(t, sqlDB, "agent-ppshake", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: oldBulk},
		{turnID: "t2", callID: "call-2", rowID: "row-new", name: "fetch", result: newBulk},
	})
	llm := newCompactLLM(t, []string{sseChunk("answer")}, nil)
	mgr := laneCompactManager(t, llm.url, svc, 3000, nil)
	before := activeThreadID(t, sqlDB, "agent-ppshake")

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-ppshake", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "answer", settled.Content)

	require.Equal(t, map[string]int{"row-old": 1, "row-new": 0},
		toolStates(t, sqlDB, "agent-ppshake"),
		"the old row elides; the newest turn is protected")
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-ppshake"))
	require.Nil(t, threadChangedDivider(t, svc, "agent-ppshake", before),
		"no rotation below the compact threshold")

	bodies := llm.received()
	require.Len(t, bodies, 1, "no summarization below the compact threshold")
	require.Contains(t, bodies[0], "[elided tool output:")
	require.Contains(t, bodies[0], "read_tool_output")
	require.Contains(t, bodies[0], "row-old")
	require.NotContains(t, bodies[0], "measurement observation record",
		"the elided payload must not reach the model")
	require.Contains(t, bodies[0], "fresh protected payload",
		"the protected newest turn keeps its payload in context")
}

// Tool mass over the line with nothing elidable — the mass sits in the
// protected newest turn — is the silent exit: the shake runs, elides
// zero, and the prompt proceeds without events, rotation, or flags.
func TestPrePromptShakeNoElidableIsSilent(t *testing.T) {
	bulk := strings.Repeat("measurement observation record ", 400)
	sqlDB, svc := contextBackend(t, "agent-ppsilent")
	seedConversation(t, sqlDB, "agent-ppsilent", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-only", name: "fetch", result: bulk},
	})
	var shakes atomic.Int32
	hook := &journalHook{
		shakeAgent: func(ctx context.Context, agentID string) (int, error) {
			shakes.Add(1)
			return svc.ShakeAgentContext(ctx, agentID)
		},
	}
	llm := newCompactLLM(t, []string{sseChunk("answer")}, nil)
	mgr := laneCompactManager(t, llm.url, svc, 3000, hook)
	before := activeThreadID(t, sqlDB, "agent-ppsilent")

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-ppsilent", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "answer", settled.Content)

	require.Equal(t, int32(1), shakes.Load(), "the shake ran once and elided zero")
	require.Equal(t, map[string]int{"row-only": 0}, toolStates(t, sqlDB, "agent-ppsilent"))
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-ppsilent"))
	var changed int
	require.NoError(t, sqlDB.QueryRow(
		`SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = 'agent-ppsilent' AND kind = 'thread_changed'`).
		Scan(&changed))
	require.Zero(t, changed, "the silent exit journals nothing observable")
	bodies := llm.received()
	require.Len(t, bodies, 1)
	require.Contains(t, bodies[0], "measurement observation record",
		"the protected payload stays in context")
}

// A committed pre-prompt compaction is the stored truth a fresh
// process replays: the successor reads summary, prompt, answer, in
// that order.
func TestPrePromptRestartReplayCarriesSummaryPromptAnswer(t *testing.T) {
	bulk := strings.Repeat("measurement observation record ", 140)
	sqlDB, svc := contextBackend(t, "agent-ppcold")
	seedConversation(t, sqlDB, "agent-ppcold", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: bulk},
		{turnID: "t2", callID: "call-2", rowID: "row-new", name: "fetch", result: bulk},
	})
	llm := newCompactLLM(t, []string{sseChunk("the answer")}, []string{"carried summary"})
	mgr := laneCompactManager(t, llm.url, svc, 1500, nil)

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-ppcold", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "the answer", settled.Content)
	successor := activeThreadID(t, sqlDB, "agent-ppcold")

	// A fresh process over the same store: replay reads the successor.
	_, _, cold, err := backend.New(sqlDB, zerolog.Nop(), coldKey(t), backend.DefaultToolLimits(), nil)
	require.NoError(t, err)
	msgs, err := cold.GetAgentThread(context.Background(), "agent-ppcold")
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	require.Equal(t, "assistant", string(msgs[0].Role))
	require.Equal(t, "carried summary", msgs[0].Content)
	require.Equal(t, "user", string(msgs[1].Role))
	require.Equal(t, "continue", msgs[1].Content)
	require.Equal(t, "assistant", string(msgs[2].Role))
	require.Equal(t, "the answer", msgs[2].Content)

	// And the next turn on the fresh lane reasons over the summary.
	fresh := newCompactLLM(t, []string{sseChunk("cold answer")}, nil)
	coldMgr := laneCompactManager(t, fresh.url, cold, 128<<10, nil)
	c2, sc2 := wsPair(t)
	next, err := admitItem(t, coldMgr, "agent-ppcold", "conn-2", sc2, "next", nil, "")
	require.NoError(t, err)
	_, nextSettled := readUntilMessage(t, c2)
	require.Equal(t, "cold answer", nextSettled.Content)
	require.Equal(t, successor, activeThreadID(t, sqlDB, "agent-ppcold"))
	bodies := fresh.received()
	require.Len(t, bodies, 1)
	require.Contains(t, bodies[0], "carried summary")
	require.NotContains(t, bodies[0], "measurement observation record")
	require.Equal(t, apitypes.ItemCompleted, itemStates(t, sqlDB, "agent-ppcold")[next])
}

// A model nobody sized skips the automation entirely: no rotation, no
// shake, the prompt runs — and the whole claim performs exactly one
// projected thread read, the same records instance feeding estimator
// and model.
func TestPrePromptWindowUnresolvableSkipsAutomation(t *testing.T) {
	sqlDB, svc := contextBackend(t, "agent-ppzero")
	seedConversation(t, sqlDB, "agent-ppzero", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-1", name: "fetch", result: "payload"},
	})
	var reads atomic.Int32
	llm := newCompactLLM(t, []string{sseChunk("answer")}, nil)
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   windowedResolver(llm.url, 0),
		Tools:          svc.AgentTools,
		GetAgentThread: countingThreadSource(svc, &reads),
	}, &journalHook{LifecycleJournal: svc})
	before := activeThreadID(t, sqlDB, "agent-ppzero")

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-ppzero", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "answer", settled.Content)

	require.Equal(t, int32(1), reads.Load(),
		"the no-action path performs exactly one projected thread read")
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-ppzero"))
	require.Equal(t, map[string]int{"row-1": 0}, toolStates(t, sqlDB, "agent-ppzero"))
	require.Len(t, llm.received(), 1)
}

// A summarization failure degrades: the prompt runs uncompacted in the
// original thread, the provider sees the turn, and the item completes.
func TestPrePromptCompactFailureDegrades(t *testing.T) {
	bulk := strings.Repeat("measurement observation record ", 140)
	sqlDB, svc := contextBackend(t, "agent-ppdegrade")
	seedConversation(t, sqlDB, "agent-ppdegrade", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: bulk},
		{turnID: "t2", callID: "call-2", rowID: "row-new", name: "fetch", result: bulk},
	})
	llm := newCompactLLM(t, []string{sseChunk("uncompacted answer")}, nil)
	// A 400 whose phrasing matches no overflow shape: a plain failure.
	llm.overflows = []string{"the summarizer exploded"}
	mgr := laneCompactManager(t, llm.url, svc, 1500, nil)
	before := activeThreadID(t, sqlDB, "agent-ppdegrade")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-ppdegrade", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "uncompacted answer", settled.Content)

	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-ppdegrade"),
		"the degrade keeps the original thread")
	require.Equal(t, apitypes.ItemCompleted, itemStates(t, sqlDB, "agent-ppdegrade")[prompt],
		"an unrequested maintenance failure must not fail the prompt")
	bodies := llm.received()
	require.Len(t, bodies, 2, "the failed summarization and the turn")
	require.Contains(t, bodies[0], "compressing an agent conversation",
		"the failed request was the summarization")
	require.Contains(t, bodies[1], "continue")
	require.Contains(t, bodies[1], "measurement observation record",
		"the uncompacted records still feed the turn")
}

// A giant prompt on an empty history skips the compact branch — there
// is nothing to summarize — and runs as the one provider request.
func TestPrePromptEmptyHistorySkipsCompact(t *testing.T) {
	sqlDB, svc := contextBackend(t, "agent-ppempty")
	llm := newCompactLLM(t, []string{sseChunk("answer")}, nil)
	mgr := laneCompactManager(t, llm.url, svc, 200, nil)
	before := activeThreadID(t, sqlDB, "agent-ppempty")

	c, sc := wsPair(t)
	giant := strings.Repeat("enormous prompt text ", 40)
	prompt, err := admitItem(t, mgr, "agent-ppempty", "conn-1", sc, giant, nil, "")
	require.NoError(t, err)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "answer", settled.Content)

	bodies := llm.received()
	require.Len(t, bodies, 1, "exactly one provider request: the turn")
	require.Contains(t, bodies[0], giant)
	require.Contains(t, bodies[0], `"stream":true`, "the one request is the turn")
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-ppempty"))
	var threads int
	require.NoError(t, sqlDB.QueryRow(
		`SELECT COUNT(*) FROM threads WHERE agent_id = 'agent-ppempty'`).Scan(&threads))
	require.Equal(t, 1, threads, "no summarization attempt, no rotation")
	require.Equal(t, apitypes.ItemCompleted, itemStates(t, sqlDB, "agent-ppempty")[prompt])
}

// A stop during the checker skips the automation once: the queue never
// pauses, the prompt claims and runs uncompacted, nothing rotates.
func TestPrePromptStopSkipsAutomationOnce(t *testing.T) {
	bulk := strings.Repeat("measurement observation record ", 140)
	sqlDB, svc := contextBackend(t, "agent-ppstop")
	seedConversation(t, sqlDB, "agent-ppstop", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: bulk},
		{turnID: "t2", callID: "call-2", rowID: "row-new", name: "fetch", result: bulk},
	})
	url, parked, release := parkedSummaryLLM(t, "the answer")
	mgr := laneCompactManager(t, url, svc, 1500, nil)
	before := activeThreadID(t, sqlDB, "agent-ppstop")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-ppstop", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	<-parked
	mgr.Interrupt("agent-ppstop")
	release()

	// The answer's terminal event precedes the drained snapshot on
	// the wire; read the message first so it is not swallowed.
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "the answer", settled.Content)
	final := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
		return p.Active == nil && len(p.Items) == 0
	})
	require.False(t, final.Paused, "a stop during the checker never pauses the queue")

	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-ppstop"), "no rotation after the skip")
	require.Nil(t, threadChangedDivider(t, svc, "agent-ppstop", before))
	require.Equal(t, map[string]int{"row-old": 0, "row-new": 0}, toolStates(t, sqlDB, "agent-ppstop"))
	require.Equal(t, apitypes.ItemCompleted, itemStates(t, sqlDB, "agent-ppstop")[prompt],
		"the stopped-checker prompt still ran")
}

// The Compacting flag rides lane_status through the checker: true
// while the summarization runs, false after — and Working stays false
// throughout, the checker is not a turn.
func TestPrePromptCompactingLaneStatusLifecycle(t *testing.T) {
	bulk := strings.Repeat("measurement observation record ", 140)
	sqlDB, svc := contextBackend(t, "agent-ppstatus")
	seedConversation(t, sqlDB, "agent-ppstatus", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: bulk},
		{turnID: "t2", callID: "call-2", rowID: "row-new", name: "fetch", result: bulk},
	})
	url, parked, release := parkedSummaryLLM(t, "the answer")
	mgr := laneCompactManager(t, url, svc, 1500, nil)

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-ppstatus", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	<-parked

	// Collect every status while the summarization is parked: the
	// checker's whole lifetime.
	var during []wstypes.LaneStatus
	compacting := laneStatusUntil(t, c, func(s wstypes.LaneStatus) bool { return s.Compacting }, &during)
	require.False(t, compacting.Working, "the checker is not a turn")
	for _, s := range during {
		require.False(t, s.Working, "Working stays false throughout the checker")
	}
	release()

	var after []wstypes.LaneStatus
	cleared := laneStatusUntil(t, c, func(s wstypes.LaneStatus) bool { return !s.Compacting }, &after)
	require.True(t, cleared.Active)
	_, settled := readUntilMessage(t, c)
	require.Equal(t, "the answer", settled.Content)
	require.Equal(t, "compact", threadKind(t, sqlDB, "agent-ppstatus", activeThreadID(t, sqlDB, "agent-ppstatus")))
}

// Command items never trigger the checker: a queued /shake and /compact
// run with no checker passes — the thread-read count equals the
// lanes' own single read.
func TestPrePromptCommandItemsExempt(t *testing.T) {
	sqlDB, svc := contextBackend(t, "agent-ppcmd")
	seedConversation(t, sqlDB, "agent-ppcmd", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "payload"},
	})
	var reads atomic.Int32
	llm := newCompactLLM(t, nil, []string{"cmd summary"})
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   windowedResolver(llm.url, 128<<10),
		Tools:          svc.AgentTools,
		GetAgentThread: countingThreadSource(svc, &reads),
	}, &journalHook{LifecycleJournal: svc})

	c, sc := wsPair(t)
	shakeID, err := admitItem(t, mgr, "agent-ppcmd", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)
	terminal, _ := drainToTerminal(t, c, shakeID)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	compactID, err := admitItem(t, mgr, "agent-ppcmd", "conn-1", sc, "/compact", nil, wstypes.QueueItemKindCompact)
	require.NoError(t, err)
	terminal, _ = drainToTerminal(t, c, compactID)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)

	require.Equal(t, int32(1), reads.Load(),
		"only the compact lane's own read; the checker never ran")
	require.Len(t, llm.received(), 1, "one summarization, no checker passes")
}

// A send-now reservation overtaking the checked item claims first and
// runs unchecked; both prompts settle completed in claim order.
func TestPrePromptSendNowOvertake(t *testing.T) {
	sqlDB, svc := contextBackend(t, "agent-ppovertake")
	seedConversation(t, sqlDB, "agent-ppovertake", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-1", name: "fetch", result: "payload"},
	})
	release := make(chan struct{})
	var reads atomic.Int32
	gated := func(ctx context.Context, agentID string) ([]backend.AgentThreadMessage, error) {
		if reads.Add(1) == 1 {
			<-release // park p1's checker only
		}
		return svc.GetAgentThread(ctx, agentID)
	}
	llm := newCompactLLM(t, []string{sseChunk("from-p2"), sseChunk("from-p1")}, nil)
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   windowedResolver(llm.url, 128<<10),
		Tools:          svc.AgentTools,
		GetAgentThread: gated,
	}, &journalHook{LifecycleJournal: svc})

	c, sc := wsPair(t)
	p1, err := admitItem(t, mgr, "agent-ppovertake", "conn-1", sc, "first prompt", nil, "")
	require.NoError(t, err)

	// The checker holds p1; the send-now reserves p2 past it.
	require.Eventually(t, func() bool { return reads.Load() >= 1 },
		2*time.Second, 5*time.Millisecond, "p1's checker never started")
	p2, err := admitItem(t, mgr, "agent-ppovertake", "conn-2", nil, "second prompt", nil, "")
	require.NoError(t, err)
	_, err = mgr.SendNow("agent-ppovertake", p2, nil)
	require.NoError(t, err)
	close(release)

	_, first := readUntilMessage(t, c)
	require.Equal(t, "from-p2", first.Content, "the overtaker claims first")
	_, second := readUntilMessage(t, c)
	require.Equal(t, "from-p1", second.Content)

	states := itemStates(t, sqlDB, "agent-ppovertake")
	require.Equal(t, apitypes.ItemCompleted, states[p1])
	require.Equal(t, apitypes.ItemCompleted, states[p2])
}

// threadKind reads one thread's kind column.
func threadKind(t *testing.T, sqlDB *sql.DB, agentID, threadID string) string {
	t.Helper()
	var kind string
	require.NoError(t, sqlDB.QueryRow(
		`SELECT kind FROM threads WHERE agent_id = ? AND id = ?`,
		agentID, threadID).Scan(&kind))
	return kind
}
