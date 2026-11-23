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
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
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

// fixedTools answers every turn with the same tools, their metadata
// resolved beside them the way the backend does.
func fixedTools(ts ...tool.InvokableTool) agent.AgentToolsFunc {
	return func(ctx context.Context, _ string) (backend.TurnTools, error) {
		infos := make([]*schema.ToolInfo, 0, len(ts))
		for _, t := range ts {
			info, err := t.Info(ctx)
			if err != nil {
				return backend.TurnTools{}, err
			}
			infos = append(infos, info)
		}
		return backend.TurnTools{Impls: ts, Infos: infos}, nil
	}
}

// A selected tool that cannot describe itself fails the turn before
// the model is called — never silently dropped from the offered set.
func TestToolMetadataFailureFailsTurn(t *testing.T) {
	broken := &fakeTool{name: "echo_tool", infoErr: errors.New("schema unavailable")}
	llm := newScriptedLLM(t, sseChunk("never reached"))

	mgr := newJournaledManager(t,
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

// backend.TurnTools crosses the module boundary as parallel slices; a
// mismatched pair must fail the turn, never panic mid-wrap.
func TestMismatchedToolResolutionFailsTurn(t *testing.T) {
	echo := &fakeTool{name: "echo_tool"}
	llm := newScriptedLLM(t, sseChunk("never reached"))

	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools: func(context.Context, string) (backend.TurnTools, error) {
				return backend.TurnTools{Impls: []tool.InvokableTool{echo}}, nil
			},
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-toolmismatch", "conn-1", sc, "hi")

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	require.Contains(t, eventPayload[wstypes.ErrorEvent](t, ev).Message,
		"impl/info length mismatch")
	require.Empty(t, llm.requests(), "the model must not be called")
}

func TestToolRoundFeedsResultAndStreamsAnswer(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	llm := newScriptedLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(),
		sseChunk("final answer"))

	mgr := newJournaledManager(t,
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

// countingInfoTool counts metadata reads: the turn must resolve the
// toolset once and read each tool's Info once — the start, result and
// outcome copies all ride that snapshot.
type countingInfoTool struct {
	*fakeTool
	infos atomic.Int64
}

func (c *countingInfoTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	c.infos.Add(1)
	return c.fakeTool.Info(ctx)
}

func TestToolResolutionCountedPerTurn(t *testing.T) {
	echo := &countingInfoTool{fakeTool: &fakeTool{name: "echo_tool", result: "tool says hi"}}
	snapshot := fixedTools(echo)
	var resolves atomic.Int64
	llm := newScriptedLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(),
		sseChunk("final answer"))

	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools: func(ctx context.Context, agentID string) (backend.TurnTools, error) {
				resolves.Add(1)
				return snapshot(ctx, agentID)
			},
		})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-toolcount", "conn-1", sc, "search for otters")

	_, p := readUntilMessage(t, c)
	require.Equal(t, "final answer", p.Content)

	require.Equal(t, []string{`{"query":"plush otters"}`}, echo.ranWith())
	require.Equal(t, int64(1), resolves.Load(), "toolset resolved per turn, not per observable copy")
	require.Equal(t, int64(1), echo.infos.Load(), "tool metadata read once per tool per turn")
}

func TestFetchedToolsAreAllOffered(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "unused"}
	weather := &fakeTool{name: "weather_tool", result: "sunny"}
	llm := newScriptedLLM(t, sseChunk("no tools needed"))

	mgr := newJournaledManager(t,
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

	mgr := newJournaledManager(t,
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
	mgr, sqlDB := toolRoundManager(t, echo,
		[]string{toolCallChunk("echo_tool") + toolCallDone(), sseChunk("never reached")})

	c, sc := wsPair(t)
	itemID := submit(t, mgr, "agent-toolfail", "conn-1", sc, "search for otters")

	// The whole report is durable: read until the item's failed
	// outcome commits, and never accept a transient error frame for a
	// tool-attributed failure.
	var toolName string
	for {
		ev := readEvent(t, c)
		require.NotEqual(t, wstypes.EventTypeError, ev.Type,
			"a tool-attributed failure is durable, never a transient error frame")
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		done := false
		for _, lifecycle := range lifecycleBatch(t, ev) {
			if lifecycle.Kind == apitypes.LifecycleToolFinished {
				toolName = lifecycle.ToolName
			}
			done = done || (lifecycle.Kind == apitypes.LifecycleItemFinished &&
				lifecycle.Outcome == apitypes.ItemFailed)
		}
		if done {
			break
		}
	}
	require.Equal(t, "echo_tool", toolName)

	// A tool failure fails the whole turn: the claim-written user row
	// stands with the native assistant group and the failed
	// invocation's outcome row — its error kept in the envelope — and
	// no terminal assistant row joins them. The queue is not paused: a
	// tool failure is not a message failure.
	rows := storedRows(t, sqlDB, "agent-toolfail")
	require.Len(t, rows, 3, describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Equal(t, "search for otters", rows[0].Content)
	require.Equal(t, apitypes.ThreadRoleAssistant, rows[1].Role, "the round's assistant call group")
	require.Equal(t, apitypes.ThreadRoleTool, rows[2].Role)
	require.Equal(t, itemID, rows[2].TurnID, "the outcome row names its turn like every other row")
	var envelope backend.ToolEnvelope
	require.NoError(t, json.Unmarshal([]byte(rows[2].Content), &envelope))
	require.Equal(t, "echo_tool", envelope.Name)
	require.Equal(t, "call-1", envelope.CallID)
	require.Equal(t, "search backend down", *envelope.Error)
}

// A failed fetch fails the turn outright — never a silent tool-less
// default — and the model is never called.
func TestToolFetchFailureFailsTurn(t *testing.T) {
	llm := newScriptedLLM(t, sseChunk("never reached"))

	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools: func(context.Context, string) (backend.TurnTools, error) {
				return backend.TurnTools{}, errors.New("tools store unreachable")
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
	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
			Tools: func(ctx context.Context, _ string) (backend.TurnTools, error) {
				close(entered)
				<-ctx.Done()
				return backend.TurnTools{}, ctx.Err()
			},
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-toolstop", "conn-1", sc, "hi")

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("tools never fetched")
	}

	require.NotNil(t, agent.Worker(mgr, "agent-toolstop"))
	mgr.Interrupt("agent-toolstop")

	for idle := false; !idle; {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeQueueState, wstypes.EventTypeMessageChunk,
			wstypes.EventTypeLifecycle:
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
	rows := storedRows(t, sqlDB, "agent-toolstop")
	require.Len(t, rows, 1, describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Empty(t, llm.requests(), "the model must not be called")
}

// After a tool round commits, a mid-turn join's prefix carries only
// what is not yet durable: the committed round's text and invocation
// ride the journal — delivered exactly once — and the prefix excludes
// them instead of repeating them.
func TestPrefixExcludesCommittedRounds(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	release := make(chan struct{})
	var releaseOnce sync.Once
	unpark := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unpark)
	var rounds atomic.Int64
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round := rounds.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		if round == 1 {
			_, _ = w.Write([]byte(sseChunk("round one text ")))
			_, _ = w.Write([]byte(toolCallChunk("echo_tool")))
			_, _ = w.Write([]byte(toolCallDone()))
		} else {
			<-release // park inside round two: the join happens here
			_, _ = w.Write([]byte(sseChunk("final answer")))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(llm.Close)

	mgr, sqlDB := journaledLane(t, &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
		Tools:          fixedTools(echo),
	})
	c1, sc1 := wsPair(t)
	itemID := submit(t, mgr, "agent-prefix", "conn-1", sc1, "search")

	// Wait for the round-one commit: its tool_finished event is the
	// durable proof the invocation's outcome row landed.
	for toolDone := false; !toolDone; {
		ev := readEvent(t, c1)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		batch := eventPayload[wstypes.LifecycleBatchEvent](t, ev)
		for _, raw := range batch.Events {
			var leaf struct {
				Kind string `json:"kind"`
			}
			require.NoError(t, json.Unmarshal(raw, &leaf))
			if leaf.Kind == apitypes.LifecycleToolFinished {
				toolDone = true
			}
		}
	}

	c2, sc2 := wsPair(t)
	journal := mgr.Journal()
	history, head, _, err := journal.JournalStatus(context.Background(), "agent-prefix")
	require.NoError(t, err)
	registered, _, err := mgr.HandOff(context.Background(), "agent-prefix", "conn-2", sc2, head)
	require.NoError(t, err)
	require.True(t, registered)
	var ready wstypes.SyncReadyEvent
	for {
		ev := readEvent(t, c2)
		if ev.Type == wstypes.EventTypeSyncReady {
			ready = eventPayload[wstypes.SyncReadyEvent](t, ev)
			break
		}
	}
	require.NotNil(t, ready.Prefix, "the turn is mid-round-two, so a prefix exists")
	require.Equal(t, itemID, ready.Prefix.ItemID)
	require.Empty(t, ready.Prefix.Text,
		"round-one text is committed and rides the journal; the prefix must not repeat it")
	require.Empty(t, ready.Prefix.Invocations,
		"the finished invocation is a durable tool row; the prefix must not repeat it")

	// Replay delivers each committed round exactly once.
	page, err := journal.ReplayAfter(context.Background(), "agent-prefix", history, 0, 100)
	require.NoError(t, err)
	committed, toolRows := 0, 0
	for _, raw := range page.Events {
		var ev apitypes.LifecycleEvent
		require.NoError(t, json.Unmarshal(raw, &ev))
		switch ev.Kind {
		case apitypes.LifecycleMessageCommitted:
			committed++
		case apitypes.LifecycleToolFinished:
			toolRows += len(ev.Rows)
		}
	}
	require.Equal(t, 1, committed, "exactly one message_committed round")
	require.Equal(t, 1, toolRows, "exactly one tool outcome row")
	var toolCount int
	require.NoError(t, sqlDB.QueryRow(
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = 'agent-prefix' AND role = 'tool'`).
		Scan(&toolCount))
	require.Equal(t, 1, toolCount)

	// The joiner still sees the live remainder and settlement.
	unpark()
	_, msg := readUntilMessage(t, c2)
	require.Equal(t, "final answer", msg.Content)
}
