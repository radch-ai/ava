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
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

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
	persist func(context.Context, string, []apitypes.ThreadMessageDraft) error) *agent.UserAgentManager {
	t.Helper()
	llm := newScriptedLLM(t, llmResponses...)
	return agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			Tools:        fixedTools(echo),
			PersistTurn:  persist,
		})
}

func TestToolRoundEmitsWireEventsInOrder(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	mgr := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("final answer")}, nil)

	c, sc := wsPair(t)
	ok := mgr.SubmitMsg(context.Background(), "agent-capwire", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "search for otters"})
	require.True(t, ok)

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
	require.Equal(t, "echo_tool", call.Name)
	require.Equal(t, `{"query":"plush otters"}`, call.Args)
	require.Equal(t, "call-1", result.ID)
	require.Equal(t, "tool says hi", result.Result)
	require.Nil(t, result.Error)
}

func TestToolRowPersistsBetweenUserAndAssistant(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	sink := &recordingSink{}
	mgr := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("final answer")}, sink.persist)

	c, sc := wsPair(t)
	ok := mgr.SubmitMsg(context.Background(), "agent-cappersist", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "search for otters"})
	require.True(t, ok)
	_, p := readUntilMessage(t, c)
	require.Equal(t, "final answer", p.Content)

	// Drain, tool row, settle — in that order, the envelope between
	// the speaking rows.
	calls := sink.recorded()
	require.Len(t, calls, 3)
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)
	require.Equal(t, apitypes.ThreadRoleTool, calls[1][0].Role)
	require.Equal(t, apitypes.ThreadRoleAssistant, calls[2][0].Role)

	var env apitypes.ToolEnvelope
	require.NoError(t, json.Unmarshal([]byte(calls[1][0].Content), &env))
	require.Equal(t, "echo_tool", env.Name)
	require.JSONEq(t, `{"query":"plush otters"}`, string(env.Args))
	// A plain-text result rides as a quoted JSON string.
	require.Equal(t, `"tool says hi"`, string(env.Result))
	require.Nil(t, env.Error)
}

// toolFailingSink fails exactly the writes whose first draft carries
// the given role; everything else lands.
type toolFailingSink struct {
	mu      sync.Mutex
	calls   [][]apitypes.ThreadMessageDraft
	failFor apitypes.ThreadRole
}

func (s *toolFailingSink) persist(_ context.Context, _ string, msgs []apitypes.ThreadMessageDraft) error {
	s.mu.Lock()
	s.calls = append(s.calls, msgs)
	fail := len(msgs) > 0 && msgs[0].Role == s.failFor
	s.mu.Unlock()
	if fail {
		return errors.New("sink unavailable")
	}
	return nil
}

func TestMidTurnSinkFailureStillCompletesTurn(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	sink := &toolFailingSink{failFor: apitypes.ThreadRoleTool}
	mgr := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("final answer")}, sink.persist)

	c, sc := wsPair(t)
	ok := mgr.SubmitMsg(context.Background(), "agent-capsink", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "search for otters"})
	require.True(t, ok)

	// The tool row's failed write costs nothing: no error event, the
	// answer arrives, and every write was still attempted.
	_, p := readUntilMessage(t, c)
	require.Equal(t, "final answer", p.Content)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	require.Len(t, sink.calls, 3)
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
	ok := mgr.SubmitMsg(context.Background(), "agent-capfail", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "search for otters"})
	require.True(t, ok)

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
			require.Len(t, calls, 2)
			require.Equal(t, apitypes.ThreadRoleTool, calls[1][0].Role)
			var env apitypes.ToolEnvelope
			require.NoError(t, json.Unmarshal([]byte(calls[1][0].Content), &env))
			require.NotNil(t, env.Error)
			require.Equal(t, *resultErr, *env.Error)
			return
		}
	}
}
