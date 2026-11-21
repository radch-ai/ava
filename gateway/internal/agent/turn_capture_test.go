package agent_test

// Tool capture pins: a tool round reaches the wire in order — the
// invocation's start, its outcome, then the answer's chunks and the
// terminal event — each invocation lands as its own thread row between the
// user row and the answer, and the outcome's text is one rendering shared
// by the wire and the stored row.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// toolRoundManager wires a lane whose one resolved tool reports through the
// real journal, and returns the store its rows land in.
func toolRoundManager(t *testing.T, echo *fakeTool, llmResponses []string) (*agent.UserAgentManager, *sql.DB) {
	t.Helper()
	llm := newScriptedLLM(t, llmResponses...)
	sqlDB, turns := journalFor(t)
	journal := &lazyJournal{TurnService: turns, sqlDB: sqlDB, seen: make(map[string]struct{})}
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
		Tools:          fixedTools(echo),
	}, &journalHook{LifecycleJournal: journal})
	return mgr, sqlDB
}

// toolOrder reads one frame's contribution to the round's ordering.
func toolOrder(t *testing.T, ev wstypes.Event) (stage string, lifecycle apitypes.LifecycleEvent) {
	t.Helper()
	switch ev.Type {
	case wstypes.EventTypeMessageChunk:
		return "chunk", apitypes.LifecycleEvent{}
	case wstypes.EventTypeLifecycle:
		for _, event := range lifecycleBatch(t, ev) {
			switch event.Kind {
			case apitypes.LifecycleToolStarted:
				return "call", event
			case apitypes.LifecycleToolFinished:
				return "result", event
			case apitypes.LifecycleItemFinished:
				return "message", event
			}
		}
		return "", apitypes.LifecycleEvent{}
	}
	return "", apitypes.LifecycleEvent{}
}

func TestToolRoundEmitsWireEventsInOrder(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	mgr, _ := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("final answer")})

	c, sc := wsPair(t)
	itemID := submit(t, mgr, "agent-capwire", "conn-1", sc, "search for otters")

	var call, result apitypes.LifecycleEvent
	var order []string
	for {
		ev := readEvent(t, c)
		stage, lifecycle := toolOrder(t, ev)
		if stage == "" {
			continue
		}
		if stage != "chunk" {
			order = append(order, stage)
		}
		switch stage {
		case "call":
			call = lifecycle
		case "result":
			result = lifecycle
		case "message":
			// Two chunks coalesce at most into one frame; the ordering
			// claim is call → result → chunk → terminal, and the
			// terminal event must be the item's.
			require.Equal(t, itemID, lifecycle.ItemID)
			require.Equal(t, apitypes.ItemCompleted, lifecycle.Outcome)
			require.Equal(t, []string{"call", "result", "message"}, order)
			require.NotEmpty(t, call.InvocationID, "the invocation carries a server identity")
			require.Equal(t, itemID, call.ItemID, "tool events carry their turn's item id")
			require.Equal(t, "echo_tool", call.ToolName)
			require.Equal(t, `{"query":"plush otters"}`, call.ToolArgs)
			require.Equal(t, call.InvocationID, result.InvocationID)
			require.Equal(t, "call-1", result.CallID)
			require.Equal(t, "tool says hi", result.ToolResult)
			require.Nil(t, result.ToolError)
			return
		}
	}
}

func TestToolRowPersistsBetweenUserAndAssistant(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	mgr, sqlDB := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("final answer")})
	c, sc := wsPair(t)
	itemID := submit(t, mgr, "agent-cappersist", "conn-1", sc, "search for otters")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "final answer", p.Content)

	// User row, the native assistant group, its outcome, then the
	// settled answer.
	rows := storedRows(t, sqlDB, "agent-cappersist")
	require.Len(t, rows, 4)
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)

	require.Equal(t, apitypes.ThreadRoleAssistant, rows[1].Role)
	require.Equal(t, itemID, rows[1].TurnID)
	var group []backend.AgentThreadCall
	require.NoError(t, json.Unmarshal([]byte(rows[1].ToolCalls), &group))
	require.Len(t, group, 1)
	require.Equal(t, "echo_tool", group[0].Name)
	require.Equal(t, `{"query":"plush otters"}`, group[0].Arguments,
		"arguments ride verbatim as the model-authored string")

	require.Equal(t, apitypes.ThreadRoleTool, rows[2].Role)
	require.Equal(t, itemID, rows[2].TurnID)
	var envelope backend.ToolEnvelope
	require.NoError(t, json.Unmarshal([]byte(rows[2].Content), &envelope))
	require.Equal(t, "echo_tool", envelope.Name)
	require.Equal(t, group[0].ID, envelope.CallID, "the envelope pairs with its assistant call")
	require.JSONEq(t, `{"query":"plush otters"}`, string(envelope.Args))
	// The result rides as the exact model-facing string.
	require.Equal(t, "tool says hi", envelope.Result)
	require.Nil(t, envelope.Error)

	require.Equal(t, apitypes.ThreadRoleAssistant, rows[3].Role)
	require.Equal(t, "final answer", rows[3].Content,
		"the terminal row carries only the terminal round's text")
}

// The wire's failure text and the persisted row's text come from one
// rendering — a client watching live and one reloading history read the
// same cause, and the tool's own wrapping survives it. A tool failure's
// whole report is durable: no transient error frame repeats it.
func TestToolFailureTextMatchesPersistedRow(t *testing.T) {
	echo := &fakeTool{name: "echo_tool",
		err: fmt.Errorf("search backend down: %w", errors.New("timeout"))}
	mgr, sqlDB := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("never reached")})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-capfail", "conn-1", sc, "search for otters")

	var resultErr *string
	for {
		ev := readEvent(t, c)
		require.NotEqual(t, wstypes.EventTypeError, ev.Type,
			"a tool-attributed failure is durable, never a transient error frame")
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		failed := false
		for _, lifecycle := range lifecycleBatch(t, ev) {
			switch lifecycle.Kind {
			case apitypes.LifecycleToolFinished:
				resultErr = lifecycle.ToolError
			case apitypes.LifecycleItemFinished:
				failed = lifecycle.Outcome == apitypes.ItemFailed
			}
		}
		if !failed {
			continue
		}
		require.NotNil(t, resultErr)
		require.Equal(t, "search backend down: timeout", *resultErr)

		rows := storedRows(t, sqlDB, "agent-capfail")
		require.Len(t, rows, 3)
		require.Equal(t, apitypes.ThreadRoleAssistant, rows[1].Role,
			"the failed round still carries its assistant call")
		require.Equal(t, apitypes.ThreadRoleTool, rows[2].Role)
		var envelope backend.ToolEnvelope
		require.NoError(t, json.Unmarshal([]byte(rows[2].Content), &envelope))
		require.NotNil(t, envelope.Error)
		require.Equal(t, *resultErr, *envelope.Error)
		return
	}
}

// twoCallChunk renders one SSE frame carrying two complete tool calls
// in a single assistant message.
func twoCallChunk(name string) string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"index": 0, "id": "call-1", "type": "function",
						"function": map[string]any{"name": name, "arguments": `{"query":"one"}`},
					},
					map[string]any{
						"index": 1, "id": "call-2", "type": "function",
						"function": map[string]any{"name": name, "arguments": `{"query":"two"}`},
					},
				},
			},
		}},
	}
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// One native assistant message with two calls is one recorded group: the
// model's declared order and arguments survive, and each outcome pairs with
// its own call — keyed by the server's invocation identity, so repeated
// provider call ids cannot collide.
func TestToolGroupPreservesDeclaredOrder(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	mgr, sqlDB := toolRoundManager(t, echo,
		[]string{twoCallChunk("echo_tool") + toolCallDone(), sseChunk("done")})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-two", "conn-1", sc, "search twice")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "done", p.Content)

	rows := storedRows(t, sqlDB, "agent-two")
	require.Len(t, rows, 5, "user, group, two outcomes, terminal")
	var group []backend.AgentThreadCall
	require.NoError(t, json.Unmarshal([]byte(rows[1].ToolCalls), &group))
	require.Len(t, group, 2)
	require.Equal(t, "call-1", group[0].ID)
	require.Equal(t, "call-2", group[1].ID)
	require.Equal(t, `{"query":"one"}`, group[0].Arguments)
	require.Equal(t, `{"query":"two"}`, group[1].Arguments)

	pairs := map[string]string{}
	for _, row := range rows[2:4] {
		var envelope backend.ToolEnvelope
		require.NoError(t, json.Unmarshal([]byte(row.Content), &envelope))
		pairs[envelope.CallID] = envelope.Result
	}
	require.Equal(t, map[string]string{
		"call-1": "tool says hi",
		"call-2": "tool says hi",
	}, pairs)
}
