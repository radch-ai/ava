package agent_test

// /shake over the real lane: the command executes without a model
// call, leaves the active thread alone, elides only old large tool
// results, keeps the queue moving, pauses on failure like any command,
// and makes the placeholder resolvable by the read_tool_output tool the
// next turn is offered.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// seedTurn is one conversation turn the lane's shake reads: a prompt, an
// assistant call group, and the outcome row.
type seedTurn struct {
	turnID string
	callID string
	rowID  string
	name   string
	result string
	err    *string
}

// seedConversation writes the turns directly, oldest first — storage
// shape only; the write path itself is pinned in the backend's tests.
func seedConversation(t *testing.T, sqlDB *sql.DB, agentID string, turns []seedTurn) {
	t.Helper()
	threadID := agentID + "-t1"
	// Seeded in the past: the live turn mints its own rising stamps, and
	// fixtures stamped at call time could sort inside them.
	base := time.Now().Add(-time.Hour).UnixMilli()
	at := base
	for i, turn := range turns {
		at++
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
			 VALUES (?, ?, 'user', ?, ?, ?, ?)`,
			fmt.Sprintf("%s-user-%d", agentID, i), agentID, "prompt "+turn.turnID, at, turn.turnID, threadID); err != nil {
			t.Fatalf("seed user row %d: %v", i, err)
		}
		calls, err := json.Marshal([]backend.AgentThreadCall{{
			ID: turn.callID, Name: turn.name, Arguments: `{"query":"x"}`,
		}})
		require.NoError(t, err)
		at++
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, tool_calls, thread_id)
			 VALUES (?, ?, 'assistant', '', ?, ?, ?, ?)`,
			fmt.Sprintf("%s-group-%d", agentID, i), agentID, at, turn.turnID, string(calls), threadID); err != nil {
			t.Fatalf("seed group %d: %v", i, err)
		}
		env, err := json.Marshal(backend.ToolEnvelope{
			Name: turn.name, CallID: turn.callID, Args: json.RawMessage(`{"query":"x"}`),
			Result: turn.result, Error: turn.err,
		})
		require.NoError(t, err)
		at++
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
			 VALUES (?, ?, 'tool', ?, ?, ?, ?)`,
			turn.rowID, agentID, string(env), at, turn.turnID, threadID); err != nil {
			t.Fatalf("seed tool row %d: %v", i, err)
		}
	}
}

// laneShakeManager wires the lane to a real backend whose shakes and
// retrieval reads are the backend's own.
func laneShakeManager(t *testing.T, llm *contextLLM, svc *backend.TurnService, hook *journalHook) *agent.UserAgentManager {
	t.Helper()
	if hook == nil {
		hook = &journalHook{}
	}
	hook.LifecycleJournal = svc
	return hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(llm.URL),
		Tools:          svc.AgentTools,
		GetAgentThread: svc.GetAgentThread,
	}, hook)
}

// drainToTerminal collects every lifecycle kind it passes and returns
// the item's terminal event.
func drainToTerminal(t *testing.T, c *websocket.Conn, itemID string) (apitypes.LifecycleEvent, []string) {
	t.Helper()
	var kinds []string
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		for _, lc := range lifecycleBatch(t, ev) {
			kinds = append(kinds, lc.Kind)
			if lc.Kind == apitypes.LifecycleItemFinished && lc.ItemID == itemID {
				return lc, kinds
			}
		}
	}
}

// toolStates reads the elision flag of every tool row, keyed by id.
func toolStates(t *testing.T, sqlDB *sql.DB, agentID string) map[string]int {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT id, elided FROM thread_messages WHERE agent_id = ? AND role = 'tool' ORDER BY created_at ASC, id ASC`, agentID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	out := make(map[string]int)
	for rows.Next() {
		var id string
		var elided int
		require.NoError(t, rows.Scan(&id, &elided))
		out[id] = elided
	}
	require.NoError(t, rows.Err())
	return out
}

// placeholder is the exact reference text the projection must emit.
func placeholder(rowID string, size int) string {
	return fmt.Sprintf("[elided tool output: %d bytes. Restore it with the read_tool_output tool: {\"reference\":%q}]", size, rowID)
}

// toolCallArgsChunk renders one SSE frame carrying a complete tool call
// with explicit arguments.
func toolCallArgsChunk(name, callID, args string) string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"index": 0, "id": callID, "type": "function",
					"function": map[string]any{"name": name, "arguments": args},
				}},
			},
		}},
	}
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// The whole lane: /shake admits as a command, executes with no model
// request, leaves the thread where it was, and flips only the old large
// results.
func TestShakeLaneElidesWithoutModel(t *testing.T) {
	llm := newContextLLM(t) // any model request would land here
	sqlDB, svc := contextBackend(t, "agent-shake")
	big := strings.Repeat("x", 3000)
	seedConversation(t, sqlDB, "agent-shake", []seedTurn{
		{turnID: "t1", callID: "call-big", rowID: "row-big", name: "fetch", result: big},
		{turnID: "t1b", callID: "call-small", rowID: "row-small", name: "fetch", result: "tiny"},
		{turnID: "t2", callID: "call-tail", rowID: "row-tail", name: "fetch", result: big},
	})
	mgr := laneShakeManager(t, llm, svc, nil)

	c, sc := wsPair(t)
	itemID, err := admitItem(t, mgr, "agent-shake", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)

	terminal, kinds := drainToTerminal(t, c, itemID)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.Equal(t, "shake", terminal.ItemKind)
	require.Equal(t, "agent-shake-t1", terminal.ThreadID)
	require.NotContains(t, kinds, apitypes.LifecycleThreadChanged,
		"a shake never rotates the thread")
	require.Empty(t, terminal.Rows, "a shake commits no row")

	require.Empty(t, llm.requests(), "a shake never calls the model")
	require.Equal(t, "agent-shake-t1", activeThreadRow(t, sqlDB, "agent-shake"))
	require.Equal(t, map[string]int{
		"row-big":   1, // old and large: elided
		"row-small": 0, // immediacy wins below the threshold
		"row-tail":  0, // the active tail is protected
	}, toolStates(t, sqlDB, "agent-shake"))

	msgs, err := svc.GetAgentThread(context.Background(), "agent-shake")
	require.NoError(t, err)
	var seen bool
	for _, m := range msgs {
		if m.Role != apitypes.ThreadRoleTool {
			continue
		}
		if m.ToolCallID == "call-big" {
			seen = true
			require.True(t, m.Elided)
			require.Equal(t, placeholder("row-big", len(big)), m.Content)
		} else {
			require.False(t, m.Elided, "call %s must keep its payload", m.ToolCallID)
		}
	}
	require.True(t, seen, "the elided outcome is still projected")
}

// The queue continues behind a shake: the next prompt runs normally.
func TestShakeLaneQueueContinues(t *testing.T) {
	llm := newContextLLM(t, sseChunk("after shake"))
	sqlDB, svc := contextBackend(t, "agent-shakeq")
	seedConversation(t, sqlDB, "agent-shakeq", []seedTurn{
		{turnID: "t1", callID: "call-big", rowID: "row-big", name: "fetch", result: strings.Repeat("x", 3000)},
		{turnID: "t2", callID: "call-tail", rowID: "row-tail", name: "fetch", result: "tail"},
	})
	mgr := laneShakeManager(t, llm, svc, nil)

	c, sc := wsPair(t)
	shakeID, err := admitItem(t, mgr, "agent-shakeq", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)
	_, err = admitItem(t, mgr, "agent-shakeq", "conn-1", sc, "now answer", nil, "")
	require.NoError(t, err)

	terminal, _ := drainToTerminal(t, c, shakeID)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	_, p := readUntilMessage(t, c)
	require.Equal(t, "after shake", p.Content)
	require.Len(t, llm.requests(), 1, "only the prompt calls the model")
}

// A failed shake is a message-level failure: the queue pauses behind
// it exactly like a failed /new.
func TestShakeLaneFailurePauses(t *testing.T) {
	llm := newContextLLM(t, sseChunk("never reached"))
	_, svc := contextBackend(t, "agent-shakef")
	failing := func(context.Context, string, string) (*backend.ShakeResult, *backend.JournalBatch, error) {
		return nil, nil, errSink
	}
	mgr := laneShakeManager(t, llm, svc, &journalHook{shakeThread: failing})

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-shakef", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)
	promptID, err := admitItem(t, mgr, "agent-shakef", "conn-1", sc, "queued behind", nil, "")
	require.NoError(t, err)

	failure := readTurnError(t, c)
	require.Equal(t, errSink.Error(), failure.Message)
	paused := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool { return p.Paused })
	require.Len(t, paused.Items, 1)
	require.Equal(t, promptID, paused.Items[0].ID)
}

// A stop that lands before the elision commits leaves nothing elided and
// settles the item as a quiet stop, never as a failure.
func TestShakeLaneCancelBeforeCommitIsQuietStop(t *testing.T) {
	llm := newContextLLM(t)
	sqlDB, svc := contextBackend(t, "agent-shakec")
	big := strings.Repeat("x", 3000)
	seedConversation(t, sqlDB, "agent-shakec", []seedTurn{
		{turnID: "t1", callID: "call-big", rowID: "row-big", name: "fetch", result: big},
		{turnID: "t2", callID: "call-tail", rowID: "row-tail", name: "fetch", result: "tail"},
	})
	entered := make(chan struct{})
	blocked := func(ctx context.Context, _, _ string) (*backend.ShakeResult, *backend.JournalBatch, error) {
		close(entered)
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	mgr := laneShakeManager(t, llm, svc, &journalHook{shakeThread: blocked})

	c, sc := wsPair(t)
	itemID, err := admitItem(t, mgr, "agent-shakec", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)
	<-entered
	mgr.Interrupt("agent-shakec")

	final := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
		return p.Active == nil && len(p.Items) == 0 && !p.Paused
	})
	require.False(t, final.Paused, "a stop is not a failure")
	require.Equal(t, map[string]int{"row-big": 0, "row-tail": 0}, toolStates(t, sqlDB, "agent-shakec"))
	require.Equal(t, apitypes.ItemStopped, itemStates(t, sqlDB, "agent-shakec")[itemID])
}

// The placeholder is resolvable: the model calls read_tool_output with
// the reference and receives the exact stored original, captured as an
// ordinary tool row.
func TestShakeLaneRetrievalRound(t *testing.T) {
	big := strings.Repeat("x", 3000)
	sqlDB, svc := contextBackend(t, "agent-shaker")
	seedConversation(t, sqlDB, "agent-shaker", []seedTurn{
		{turnID: "t1", callID: "call-big", rowID: "row-big", name: "fetch", result: big},
		{turnID: "t2", callID: "call-tail", rowID: "row-tail", name: "fetch", result: "tail"},
	})
	// The model's first round restores the reference, the second answers.
	llm := newContextLLM(t,
		toolCallArgsChunk("read_tool_output", "call-restore", `{"reference":"row-big"}`)+toolCallDone(),
		sseChunk("restored"))
	mgr := laneShakeManager(t, llm, svc, nil)

	c, sc := wsPair(t)
	shakeID, err := admitItem(t, mgr, "agent-shaker", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)
	drainToTerminal(t, c, shakeID)

	submit(t, mgr, "agent-shaker", "conn-1", sc, "what did it say")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "restored", p.Content)

	// The captured outcome carries the exact original text.
	rows, err := sqlDB.Query(
		`SELECT content FROM thread_messages WHERE agent_id = 'agent-shaker' AND role = 'tool' ORDER BY created_at ASC, id ASC`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var results []string
	for rows.Next() {
		var content string
		require.NoError(t, rows.Scan(&content))
		var env backend.ToolEnvelope
		require.NoError(t, json.Unmarshal([]byte(content), &env))
		results = append(results, env.Result)
	}
	require.NoError(t, rows.Err())
	require.Len(t, results, 3, "two seeded outcomes plus the captured restore")
	require.Contains(t, results, big, "the restore returned the exact stored original")
}

// Across a cold restart the same database projects the placeholder and
// offers read_tool_output with it.
func TestShakeLaneColdRestartKeepsReference(t *testing.T) {
	sqlDB, svc := contextBackend(t, "agent-shakecold")
	big := strings.Repeat("x", 3000)
	seedConversation(t, sqlDB, "agent-shakecold", []seedTurn{
		{turnID: "t1", callID: "call-big", rowID: "row-big", name: "fetch", result: big},
		{turnID: "t2", callID: "call-tail", rowID: "row-tail", name: "fetch", result: "tail"},
	})
	silent := newContextLLM(t)
	c, sc := wsPair(t)
	first := laneShakeManager(t, silent, svc, nil)
	shakeID, err := admitItem(t, first, "agent-shakecold", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)
	drainToTerminal(t, c, shakeID)

	// A fresh backend and manager over the same store: the read is cold.
	_, _, cold, err := backend.New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x43}, 32), backend.DefaultToolLimits())
	require.NoError(t, err)
	llm := newScriptedLLM(t, sseChunk("cold answer"))
	coldMgr := hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(llm.URL),
		Tools:          cold.AgentTools,
		GetAgentThread: cold.GetAgentThread,
	}, &journalHook{LifecycleJournal: cold})

	// A fresh connection: the restarted manager owns the queue state.
	c2, sc2 := wsPair(t)
	submit(t, coldMgr, "agent-shakecold", "conn-1", sc2, "resume")
	_, p := readUntilMessage(t, c2)
	require.Equal(t, "cold answer", p.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 1)
	var names []string
	for _, tool := range reqs[0].Tools {
		names = append(names, tool.Function.Name)
	}
	require.Contains(t, names, "read_tool_output",
		"the restored context offers read_tool_output")
	var carried bool
	for _, m := range reqs[0].Messages {
		if strings.Contains(m.Content, placeholder("row-big", len(big))) {
			carried = true
		}
	}
	require.True(t, carried, "the cold request carries the elision placeholder")
}

// toolRowResults lists every tool row's envelope result in commit order.
func toolRowResults(t *testing.T, sqlDB *sql.DB, agentID string) []string {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT content FROM thread_messages WHERE agent_id = ? AND role = 'tool' ORDER BY created_at ASC, id ASC`, agentID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var content string
		require.NoError(t, rows.Scan(&content))
		var env backend.ToolEnvelope
		require.NoError(t, json.Unmarshal([]byte(content), &env))
		out = append(out, env.Result)
	}
	require.NoError(t, rows.Err())
	return out
}

// The restore cannot resurrect what the capture scrubbed: a leak-shaped
// original is redacted before it is stored, and read_tool_output
// re-serves the stored copy — redaction marker in place, canary-free.
func TestShakeLaneRestoreKeepsRedaction(t *testing.T) {
	const raw = "sk-abcdefghijklmnop1234" // the pattern battery's api_key shape
	padded := strings.Repeat("p", 3000) + " " + raw
	sqlDB, svc := contextBackend(t, "agent-shakeredact")

	// The scripted provider's queue grows as the test learns the minted
	// row id: the restore round must name it.
	var mu sync.Mutex
	var responses []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		resp := ""
		if len(responses) > 0 {
			resp, responses = responses[0], responses[1:]
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		if resp != "" {
			_, _ = w.Write([]byte(resp))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(llm.Close)
	mu.Lock()
	responses = []string{
		toolCallChunk("echo_tool") + toolCallDone(),
		sseChunk("done"),        // turn A settles
		sseChunk("tail answer"), // turn B: the protected active tail
	}
	mu.Unlock()

	// The leaky echo rides beside the internal tool through the real
	// resolution, so capture, scrub, elision, and restore all take the
	// production path.
	leaky := &fakeTool{name: "echo_tool", result: padded}
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(llm.URL),
		GetAgentThread: svc.GetAgentThread,
		Tools: func(ctx context.Context, agentID string) (backend.TurnTools, error) {
			resolved, err := svc.AgentTools(ctx, agentID)
			if err != nil {
				return backend.TurnTools{}, err
			}
			info, err := leaky.Info(ctx)
			if err != nil {
				return backend.TurnTools{}, err
			}
			resolved.Impls = append(resolved.Impls, leaky)
			resolved.Infos = append(resolved.Infos, info)
			return resolved, nil
		},
	}, &journalHook{LifecycleJournal: svc})

	c, sc := wsPair(t)
	first, err := admitItem(t, mgr, "agent-shakeredact", "conn-1", sc, "run the echo", nil, "")
	require.NoError(t, err)
	drainToTerminal(t, c, first)
	second, err := admitItem(t, mgr, "agent-shakeredact", "conn-1", sc, "and settle", nil, "")
	require.NoError(t, err)
	drainToTerminal(t, c, second)

	// Capture-time scrub: the stored outcome never held the canary.
	stored := toolRowResults(t, sqlDB, "agent-shakeredact")
	require.Len(t, stored, 1, "the echo outcome row")
	require.NotContains(t, stored[0], raw)
	require.Contains(t, stored[0], "[REDACTED:api_key]")

	shakeID, err := admitItem(t, mgr, "agent-shakeredact", "conn-1", sc, "/shake", nil, wstypes.QueueItemKindShake)
	require.NoError(t, err)
	drainToTerminal(t, c, shakeID)
	var rowID string
	require.NoError(t, sqlDB.QueryRow(
		`SELECT id FROM thread_messages WHERE agent_id = 'agent-shakeredact' AND role = 'tool' AND elided = 1`).
		Scan(&rowID), "the shake elided the echo outcome")

	mu.Lock()
	responses = append(responses,
		toolCallArgsChunk("read_tool_output", "call-restore", fmt.Sprintf(`{"reference":%q}`, rowID))+toolCallDone(),
		sseChunk("restored"))
	mu.Unlock()
	third, err := admitItem(t, mgr, "agent-shakeredact", "conn-1", sc, "restore it", nil, "")
	require.NoError(t, err)
	terminal, _ := drainToTerminal(t, c, third)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.Equal(t, "restored", terminalText(terminal.Rows))

	stored = toolRowResults(t, sqlDB, "agent-shakeredact")
	require.Len(t, stored, 2, "the restore outcome row")
	require.NotContains(t, stored[1], raw, "the restore re-serves the scrubbed copy")
	require.Contains(t, stored[1], "[REDACTED:api_key]")
	require.Contains(t, stored[1], strings.Repeat("p", 3000), "the surviving original rides")
}
