package agent_test

// Tool pins: the fetched set is what the turn attaches — empty
// attaches nothing — a tool round rides the ReAct loop (call, result
// fed back, streamed answer), and a failing tool errors the turn
// with the user row standing.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// toolsRequest is the request body shape the tool tests read.
type toolsRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

// toolCallChunk renders one SSE frame carrying a complete tool call.
func toolCallChunk(name string) string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"index": 0, "id": "call-1", "type": "function",
					"function": map[string]any{
						"name":      name,
						"arguments": `{"query":"plush otters"}`,
					},
				}},
			},
		}},
	}
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// toolCallDone renders the SSE frame that ends a tool-call round.
func toolCallDone() string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "tool_calls",
			"delta": map[string]any{},
		}},
	}
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// scriptedLLM serves one scripted SSE response per request, in
// order, and records every request body the model call carried.
type scriptedLLM struct {
	*httptest.Server
	mu        sync.Mutex
	bodies    []toolsRequest
	responses []string
}

func (s *scriptedLLM) requests() []toolsRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]toolsRequest, len(s.bodies))
	copy(out, s.bodies)
	return out
}

// newScriptedLLM builds the fake the tool tests assert against.
func newScriptedLLM(t *testing.T, responses ...string) *scriptedLLM {
	t.Helper()
	s := &scriptedLLM{responses: responses}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req toolsRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		s.bodies = append(s.bodies, req)
		i := len(s.bodies) - 1
		s.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		var frames string
		if i < len(s.responses) {
			frames = s.responses[i]
		}
		_, _ = w.Write([]byte(frames))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(s.Close)
	return s
}

// fakeTool is an invokable test tool: it answers a fixed result or
// a fixed error and records the arguments it ran with.
type fakeTool struct {
	name    string
	result  string
	err     error
	infoErr error

	mu    sync.Mutex
	calls []string
}

func (t *fakeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	if t.infoErr != nil {
		return nil, t.infoErr
	}
	return &schema.ToolInfo{
		Name: t.name,
		Desc: "test tool",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: schema.String, Desc: "the query", Required: true},
		}),
	}, nil
}

func (t *fakeTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	t.mu.Lock()
	t.calls = append(t.calls, args)
	t.mu.Unlock()
	if t.err != nil {
		return "", t.err
	}
	return t.result, nil
}

func (t *fakeTool) ranWith() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.calls))
	copy(out, t.calls)
	return out
}

// fixedTools answers every turn with the same tools.
func fixedTools(ts ...tool.InvokableTool) agent.AgentToolsFunc {
	return func(context.Context, string) ([]tool.InvokableTool, error) { return ts, nil }
}

// A selected tool that cannot describe itself fails the turn before
// the model is called — never silently dropped from the offered set.
func TestToolMetadataFailureFailsTurn(t *testing.T) {
	broken := &fakeTool{name: "echo_tool", infoErr: errors.New("schema unavailable")}
	llm := newScriptedLLM(t, sseChunk("never reached"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools:          fixedTools(broken),
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-toolinfo", "conn-1", sc, "hi")

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	require.Contains(t,
		eventPayload[wstypes.ErrorEvent](t, ev).Message, "schema unavailable")
	require.Empty(t, llm.requests(), "the model must not be called")
}

func TestToolRoundFeedsResultAndStreamsAnswer(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	llm := newScriptedLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(),
		sseChunk("final answer"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools:          fixedTools(echo),
		})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-tools", "conn-1", sc, "search for otters")

	_, p := readUntilMessage(t, c)
	require.Equal(t, "final answer", p.Content)

	// The tool ran once with the model's arguments.
	require.Equal(t, []string{`{"query":"plush otters"}`}, echo.ranWith())

	reqs := llm.requests()
	require.Len(t, reqs, 2, "model called twice: tool round then answer round")
	// Round one offered exactly the enabled tool.
	require.Len(t, reqs[0].Tools, 1)
	require.Equal(t, "echo_tool", reqs[0].Tools[0].Function.Name)
	// Round two carried the tool's result back to the model.
	var toolMsg *struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	for i := range reqs[1].Messages {
		if reqs[1].Messages[i].Role == "tool" {
			toolMsg = &reqs[1].Messages[i]
		}
	}
	require.NotNil(t, toolMsg, "second round carries a tool message")
	require.Equal(t, "tool says hi", toolMsg.Content)
}

func TestFetchedToolsAreAllOffered(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "unused"}
	weather := &fakeTool{name: "weather_tool", result: "sunny"}
	llm := newScriptedLLM(t, sseChunk("no tools needed"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools:          fixedTools(echo, weather),
		})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-filter", "conn-1", sc, "hi")

	_, p := readUntilMessage(t, c)
	require.Equal(t, "no tools needed", p.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 1)
	require.Len(t, reqs[0].Tools, 2, "everything the fetch returned is offered")
	require.Equal(t, "echo_tool", reqs[0].Tools[0].Function.Name)
	require.Equal(t, "weather_tool", reqs[0].Tools[1].Function.Name)
}

func TestEmptyFetchAttachesNoTools(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "unused"}
	llm := newScriptedLLM(t, sseChunk("plain answer"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools:          fixedTools(),
		})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-notools", "conn-1", sc, "hi")

	_, p := readUntilMessage(t, c)
	require.Equal(t, "plain answer", p.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 1)
	require.Empty(t, reqs[0].Tools, "empty fetch attaches nothing")
	require.Empty(t, echo.ranWith())
}
func TestToolFailureErrorsTurn(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", err: errors.New("search backend down")}
	llm := newScriptedLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(),
		sseChunk("never reached"))

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools:          fixedTools(echo),
			PersistTurn:    sink.persist,
		})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-toolfail", "conn-1", sc, "search for otters")

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	require.Equal(t, "echo_tool",
		eventPayload[wstypes.ErrorEvent](t, ev).Tool)

	// A tool failure fails the whole turn: the claim-written user row
	// stands with the native assistant group and the failed
	// invocation's outcome row — its error kept in the envelope — and
	// no terminal assistant row joins them. The queue is not paused: a
	// tool failure is not a message failure.
	calls := sink.recorded()
	require.Len(t, calls, 3)
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)
	require.Equal(t, "search for otters", calls[0][0].Content)
	require.Len(t, calls[1], 1, "the round's assistant call group")
	require.Equal(t, apitypes.ThreadRoleAssistant, calls[1][0].Role)
	require.Len(t, calls[2], 1)
	require.Equal(t, apitypes.ThreadRoleTool, calls[2][0].Role)
	require.JSONEq(t, `{"name":"echo_tool","callId":"call-1","args":{"query":"plush otters"},
		"result":"","error":"search backend down"}`, calls[2][0].Content)
}

// A failed fetch fails the turn outright — never a silent tool-less
// default — and the model is never called.
func TestToolFetchFailureFailsTurn(t *testing.T) {
	llm := newScriptedLLM(t, sseChunk("never reached"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools: func(context.Context, string) ([]tool.InvokableTool, error) {
				return nil, errors.New("tools store unreachable")
			},
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-toolsread", "conn-1", sc, "hi")

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	require.Contains(t,
		eventPayload[wstypes.ErrorEvent](t, ev).Message, "tools store unreachable")
	require.Empty(t, llm.requests(), "the model must not be called")
}

// A stop during tool setup is a quiet stop: no error event, no
// error row — the user message stands alone.
func TestInterruptDuringToolFetchEmitsNothing(t *testing.T) {
	llm := newScriptedLLM(t, sseChunk("never reached"))
	entered := make(chan struct{})
	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			PersistTurn:    sink.persist,
			Tools: func(ctx context.Context, _ string) ([]tool.InvokableTool, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-toolstop", "conn-1", sc, "hi")

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tools never fetched")
	}

	require.NotNil(t, mgr.Get("agent-toolstop"))
	mgr.Interrupt("agent-toolstop")

	for idle := false; !idle; {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeQueueState, wstypes.EventTypeMessageChunk,
			wstypes.EventTypeToolCall, wstypes.EventTypeToolResult:
		case wstypes.EventTypeLaneStatus:
			if !eventPayload[wstypes.LaneStatus](t, ev).Working {
				idle = true
			}
		case wstypes.EventTypeError:
			t.Fatalf("a stop must not emit an error event: %+v", ev.Data)
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}

	// The user row stands alone: a stop writes no error row.
	calls := sink.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)
	require.Empty(t, llm.requests(), "the model must not be called")
}
