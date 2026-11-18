package agent_test

// Tool capture pins: a tool round reaches the wire in order —
// tool_call, tool_result, then the answer's chunks and final
// message — the invocation lands as a thread row between the
// user and assistant rows, and a sick sink costs the turn
// nothing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// readAllUntilMessage collects every frame up to and including the
// final message event, in arrival order.
func readAllUntilMessage(t *testing.T, c *websocket.Conn) []wstypes.Event {
	t.Helper()
	var frames []wstypes.Event
	for {
		ev := readEvent(t, c)
		frames = append(frames, ev)
		if ev.Type == wstypes.EventTypeMessage {
			return frames
		}
	}
}

// toolRoundManager wires a lane whose one resolved tool reports to
// both the wire and the given sink.
func toolRoundManager(t *testing.T, echo *fakeTool, llmResponses []string,
	persist func(context.Context, string, []backend.ThreadMessageDraft) error) *agent.UserAgentManager {
	t.Helper()
	llm := newScriptedLLM(t, llmResponses...)
	return agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools:          fixedTools(echo),
			PersistTurn:    persist,
		})
}

func TestToolRoundEmitsWireEventsInOrder(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	mgr := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("final answer")}, nil)

	c, sc := wsPair(t)
	itemID := submit(t, mgr, "agent-capwire", "conn-1", sc, "search for otters")

	frames := readAllUntilMessage(t, c)
	var call wstypes.ToolCallEvent
	var result wstypes.ToolResultEvent
	var order []string
	for _, f := range frames {
		switch f.Type {
		case wstypes.EventTypeToolCall:
			require.Empty(t, call.ID, "one call expected")
			call = eventPayload[wstypes.ToolCallEvent](t, f)
			order = append(order, "call")
		case wstypes.EventTypeToolResult:
			result = eventPayload[wstypes.ToolResultEvent](t, f)
			order = append(order, "result")
		case wstypes.EventTypeMessageChunk:
			order = append(order, "chunk")
		case wstypes.EventTypeMessage:
			order = append(order, "message")
		}
	}
	require.Equal(t, []string{"call", "result", "chunk", "message"}, order)
	require.Equal(t, "call-1", call.ID)
	require.Equal(t, itemID, call.TurnID, "tool events carry their turn's item id")
	require.Equal(t, "echo_tool", call.Name)
	require.Equal(t, `{"query":"plush otters"}`, call.Args)
	require.Equal(t, "call-1", result.ID)
	require.Equal(t, itemID, result.TurnID)
	require.Equal(t, "tool says hi", result.Result)
	require.Nil(t, result.Error)
}

func TestToolRowPersistsBetweenUserAndAssistant(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	sink := &recordingSink{}
	mgr := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("final answer")}, sink.persist)
	c, sc := wsPair(t)
	itemID := submit(t, mgr, "agent-cappersist", "conn-1", sc, "search for otters")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "final answer", p.Content)

	// User row, the native assistant group, its outcome, then the
	// settled answer — each a separate required write.
	calls := sink.recorded()
	require.Len(t, calls, 4)
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)

	require.Len(t, calls[1], 1)
	require.Equal(t, apitypes.ThreadRoleAssistant, calls[1][0].Role)
	require.Equal(t, itemID, calls[1][0].TurnID)
	var group []backend.AgentThreadCall
	require.NoError(t, json.Unmarshal(calls[1][0].ToolCalls, &group))
	require.Len(t, group, 1)
	require.Equal(t, "echo_tool", group[0].Name)
	require.Equal(t, `{"query":"plush otters"}`, group[0].Arguments,
		"arguments ride verbatim as the model-authored string")

	require.Len(t, calls[2], 1)
	require.Equal(t, apitypes.ThreadRoleTool, calls[2][0].Role)
	require.Equal(t, itemID, calls[2][0].TurnID)
	var env backend.ToolEnvelope
	require.NoError(t, json.Unmarshal([]byte(calls[2][0].Content), &env))
	require.Equal(t, "echo_tool", env.Name)
	require.Equal(t, group[0].ID, env.CallID, "the envelope pairs with its assistant call")
	require.JSONEq(t, `{"query":"plush otters"}`, string(env.Args))
	// The result rides as the exact model-facing string.
	require.Equal(t, "tool says hi", env.Result)
	require.Nil(t, env.Error)

	require.Len(t, calls[3], 1)
	require.Equal(t, apitypes.ThreadRoleAssistant, calls[3][0].Role)
	require.Equal(t, "final answer", calls[3][0].Content,
		"the terminal row carries only the terminal round's text")
}

// The wire's failure text and the persisted row's text come from one
// rendering — a client watching live and one reloading history read
// the same cause, and the tool's own wrapping survives it.
func TestToolFailureTextMatchesPersistedRow(t *testing.T) {
	echo := &fakeTool{name: "echo_tool",
		err: fmt.Errorf("search backend down: %w", errors.New("timeout"))}
	sink := &recordingSink{}
	mgr := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("never reached")}, sink.persist)

	c, sc := wsPair(t)
	submit(t, mgr, "agent-capfail", "conn-1", sc, "search for otters")

	var resultErr *string
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeToolResult:
			resultErr = eventPayload[wstypes.ToolResultEvent](t, ev).Error
		case wstypes.EventTypeError:
			require.NotNil(t, resultErr)
			require.Equal(t, "search backend down: timeout", *resultErr)

			calls := sink.recorded()
			require.Len(t, calls, 3)
			require.Equal(t, apitypes.ThreadRoleAssistant, calls[1][0].Role,
				"the failed round still carries its assistant call")
			require.Equal(t, apitypes.ThreadRoleTool, calls[2][0].Role)
			var env backend.ToolEnvelope
			require.NoError(t, json.Unmarshal([]byte(calls[2][0].Content), &env))
			require.NotNil(t, env.Error)
			require.Equal(t, *resultErr, *env.Error)
			return
		}
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

// One native assistant message with two calls is one recorded group:
// the model's declared order and arguments survive, and each outcome
// pairs with its own call.
func TestToolGroupPreservesDeclaredOrder(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	sink := &recordingSink{}
	mgr := toolRoundManager(t, echo,
		[]string{twoCallChunk("echo_tool") + toolCallDone(), sseChunk("done")}, sink.persist)

	c, sc := wsPair(t)
	submit(t, mgr, "agent-two", "conn-1", sc, "search twice")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "done", p.Content)

	calls := sink.recorded()
	require.Len(t, calls, 5, "user, group, two outcomes, terminal")
	var group []backend.AgentThreadCall
	require.NoError(t, json.Unmarshal(calls[1][0].ToolCalls, &group))
	require.Len(t, group, 2)
	require.Equal(t, "call-1", group[0].ID)
	require.Equal(t, "call-2", group[1].ID)
	require.Equal(t, `{"query":"one"}`, group[0].Arguments)
	require.Equal(t, `{"query":"two"}`, group[1].Arguments)

	pairs := map[string]string{}
	for _, batch := range calls[2:4] {
		var env backend.ToolEnvelope
		require.NoError(t, json.Unmarshal([]byte(batch[0].Content), &env))
		pairs[env.CallID] = string(env.Result)
	}
	require.Equal(t, map[string]string{
		"call-1": "tool says hi",
		"call-2": "tool says hi",
	}, pairs)
}
