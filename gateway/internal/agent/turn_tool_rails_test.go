package agent_test

// The gateway's own rails: a turn-scoped attempt budget and
// a defense-in-depth pattern scrub of everything the wire and the
// transcript carry. The backend's admission chain enforces authorization
// and known-value scrubbing; these are the final fan-out checks.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// manyToolCalls renders one SSE frame requesting n distinct tool calls.
func manyToolCalls(name string, n int) string {
	calls := make([]any, 0, n)
	for i := range n {
		calls = append(calls, map[string]any{
			"index": i, "id": fmt.Sprintf("call-%d", i), "type": "function",
			"function": map[string]any{
				"name":      name,
				"arguments": `{"query":"x"}`,
			},
		})
	}
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"role": "assistant", "tool_calls": calls},
		}},
	}
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// railsTurn is one driven turn's lifecycle events, in commit order, up to
// and including its terminal event.
type railsTurn struct {
	events []apitypes.LifecycleEvent
}

// collectTurn drains frames until the turn's terminal lifecycle event and
// reports every event the journal committed along the way.
func collectTurn(t *testing.T, c *websocket.Conn) railsTurn {
	t.Helper()
	var turn railsTurn
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		for _, lifecycle := range lifecycleBatch(t, ev) {
			turn.events = append(turn.events, lifecycle)
			if lifecycle.Kind == apitypes.LifecycleItemFinished {
				return turn
			}
		}
	}
}

// countKind counts the committed events of one kind.
func (turn railsTurn) countKind(kind string) int {
	n := 0
	for _, ev := range turn.events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// finishedOf returns the first event of one kind.
func (turn railsTurn) finishedOf(kind string) (apitypes.LifecycleEvent, bool) {
	for _, ev := range turn.events {
		if ev.Kind == kind {
			return ev, true
		}
	}
	return apitypes.LifecycleEvent{}, false
}

// The 33rd attempt in one turn is a structured budget outcome, the tool
// never runs for it, and the turn still completes.
func TestTurnAttemptBudgetStopsExecution(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	llm := newScriptedLLM(t,
		manyToolCalls("echo_tool", 33)+toolCallDone(),
		sseChunk("final answer"))

	mgr := newJournaledManager(t, &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
		Tools:          fixedTools(echo),
	})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-budget", "conn-1", sc, "go wild")

	turn := collectTurn(t, c)

	require.Equal(t, 33, turn.countKind(apitypes.LifecycleToolStarted),
		"every attempt's intent is recorded, denied ones included")

	denials := 0
	for _, ev := range turn.events {
		if ev.Kind != apitypes.LifecycleToolFinished {
			continue
		}
		if strings.Contains(ev.ToolResult, `"code":"budget_exceeded"`) {
			denials++
		}
	}
	require.Equal(t, 1, denials, "the 33rd attempt is a budget outcome")
	require.Equal(t, 33, turn.countKind(apitypes.LifecycleToolFinished),
		"every attempt commits an outcome")
	require.Len(t, echo.ranWith(), 32, "the denied attempt never reached the tool")

	// The turn survived: the model's answer still lands.
	terminal, ok := turn.finishedOf(apitypes.LifecycleItemFinished)
	require.True(t, ok, "turn reached a terminal event")
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.Equal(t, "final answer", terminalText(terminal.Rows))
}

// A credential shape in a tool's result is redacted before the wire and
// the transcript see it.
func TestFanOutScrubRedactsResult(t *testing.T) {
	const raw = "sk-abcdefghijklmnop1234"
	leaky := &fakeTool{name: "echo_tool", result: `{"echo":"` + raw + `"}`}
	llm := newScriptedLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(),
		sseChunk("done"))

	mgr := newJournaledManager(t, &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
		Tools:          fixedTools(leaky),
	})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-scrub", "conn-1", sc, "search")

	turn := collectTurn(t, c)
	finished, ok := turn.finishedOf(apitypes.LifecycleToolFinished)
	require.True(t, ok, "one tool outcome expected")
	require.NotContains(t, finished.ToolResult, raw)
	require.Contains(t, finished.ToolResult, "[REDACTED:api_key]")
}

// A credential shape in a tool failure's message is redacted on its way
// to the committed error.
func TestFanOutScrubRedactsToolError(t *testing.T) {
	const raw = "ghp_0123456789abcdefghijKLMN"
	failing := &fakeTool{name: "echo_tool", err: fmt.Errorf("upstream said %s", raw)}
	llm := newScriptedLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(),
		sseChunk("done"))

	mgr := newJournaledManager(t, &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
		Tools:          fixedTools(failing),
	})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-scrub-error", "conn-1", sc, "search")

	turn := collectTurn(t, c)
	finished, ok := turn.finishedOf(apitypes.LifecycleToolFinished)
	require.True(t, ok, "the failed invocation commits its outcome")
	require.NotNil(t, finished.ToolError)
	require.NotContains(t, *finished.ToolError, raw)
	require.Contains(t, *finished.ToolError, "[REDACTED:github_token]")
}
