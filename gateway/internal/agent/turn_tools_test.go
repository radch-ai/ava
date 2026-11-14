package agent_test

// Tool pins: the config's enabled list decides which registered
// tools the turn attaches — empty attaches nothing — a tool round
// rides the ReAct loop (call, result fed back, streamed answer),
// and a failing tool errors the turn with the user row standing.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
	name   string
	result string
	err    error

	mu    sync.Mutex
	calls []string
}

func (t *fakeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
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

// fixedTools serves one registry to every lane.
func fixedTools(ts ...tool.BaseTool) agent.ToolSource {
	return func() []tool.BaseTool { return ts }
}

func enabledConfig(names ...string) *apitypes.AgentConfig {
	return &apitypes.AgentConfig{
		AgentEditableFields: apitypes.AgentEditableFields{
			ToolsEnabled: &names,
		},
	}
}

func TestToolRoundFeedsResultAndStreamsAnswer(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	llm := newScriptedLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(),
		sseChunk("final answer"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel:   fixedResolver(llm.URL),
			GetAgentConfig: fixedConfig(enabledConfig("echo_tool")),
			ToolSource:     fixedTools(echo),
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-tools", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "search for otters"})
	require.True(t, ok)

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

func TestEnabledListFiltersRegistry(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "unused"}
	weather := &fakeTool{name: "weather_tool", result: "sunny"}
	llm := newScriptedLLM(t, sseChunk("no tools needed"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel:   fixedResolver(llm.URL),
			GetAgentConfig: fixedConfig(enabledConfig("weather_tool")),
			ToolSource:     fixedTools(echo, weather),
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-filter", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "no tools needed", p.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 1)
	require.Len(t, reqs[0].Tools, 1, "only the enabled tool is offered")
	require.Equal(t, "weather_tool", reqs[0].Tools[0].Function.Name)
}

func TestEmptyEnabledListAttachesNoTools(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "unused"}
	llm := newScriptedLLM(t, sseChunk("plain answer"))

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel:   fixedResolver(llm.URL),
			GetAgentConfig: fixedConfig(enabledConfig()),
			ToolSource:     fixedTools(echo),
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-notools", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "plain answer", p.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 1)
	require.Empty(t, reqs[0].Tools, "empty enabled list attaches nothing")
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
			ResolveModel:   fixedResolver(llm.URL),
			GetAgentConfig: fixedConfig(enabledConfig("echo_tool")),
			ToolSource:     fixedTools(echo),
			PersistTurn:    sink.persist,
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-toolfail", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "search for otters"})
	require.True(t, ok)

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)

	// A tool failure fails the whole turn: the drain-written user row
	// stands with the failed invocation's tool row — its error kept
	// in the envelope — and no assistant row joins them.
	calls := sink.recorded()
	require.Len(t, calls, 2)
	require.Equal(t,
		[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "search for otters"}},
		calls[0])
	require.Equal(t, apitypes.ThreadRoleTool, calls[1][0].Role)
	require.JSONEq(t, `{"name":"echo_tool","args":{"query":"plush otters"},
		"result":"","error":"search backend down"}`, calls[1][0].Content)
}
