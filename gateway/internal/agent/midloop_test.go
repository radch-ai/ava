package agent_test

// The mid-loop compaction lane end to end, over a real backend, real
// WS pairs, and a scripted provider: the accounting matrix (reported
// usage, estimator fallback, thresholds, unknown window), ordinary and
// rolling ranges, one operation per checkpoint, the degrades (summary
// and rotation), stops before and after commit, the provider-rejection
// backstop, the compacting status, and the reconnect hand-off after a
// committed rotation — ordinary and rolling both.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// midLoopLLM serves the three call shapes a mid-turn checkpoint makes:
// scripted SSE rounds for the prompt's model requests (optionally gated
// per request), and scripted JSON completions for the summarization
// calls (optionally parked). Every request body is recorded verbatim.
type midLoopLLM struct {
	*httptest.Server
	url  string
	mu   sync.Mutex
	body []string

	rounds  []string        // one SSE payload per streaming request, in order
	gates   []chan struct{} // optional release gate per streaming request
	streams atomic.Int32
	// streamStatus, when set per request, rejects that streaming
	// request with the given status instead of serving its round.
	streamStatus []int

	answers     []string      // one content per non-streaming request, in order
	park        chan struct{} // when non-nil, every summarization waits for it
	parkArrived chan struct{} // the test's arrival signal, closed on first park
	status      int           // non-streaming status when non-200
}

func newMidLoopLLM(t *testing.T, rounds []string, answers []string) *midLoopLLM {
	t.Helper()
	s := &midLoopLLM{rounds: rounds, answers: answers}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		s.mu.Lock()
		s.body = append(s.body, string(raw))
		streaming := strings.Contains(string(raw), `"stream":true`)
		var gate, park chan struct{}
		var answer string
		var round string
		var fail int
		if streaming {
			i := int(s.streams.Add(1)) - 1
			if i < len(s.rounds) {
				round = s.rounds[i]
			}
			if i < len(s.gates) {
				gate = s.gates[i]
			}
			if i < len(s.streamStatus) {
				fail = s.streamStatus[i]
			}
		} else {
			if len(s.answers) > 0 {
				answer, s.answers = s.answers[0], s.answers[1:]
			}
			park = s.park
			if park != nil && s.parkArrived != nil {
				arrived := s.parkArrived
				s.parkArrived = nil
				close(arrived)
			}
		}
		status := s.status
		s.mu.Unlock()

		if gate != nil {
			<-gate
		}
		if park != nil {
			<-park
		}
		if fail != 0 {
			w.WriteHeader(fail)
			_, _ = w.Write([]byte(`{"error":{"message":"maximum context length exceeded","type":"invalid_request_error"}}`))
			return
		}
		if streaming {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			if round != "" {
				_, _ = w.Write([]byte(round))
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			return
		}
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"summarizer unavailable","type":"server_error"}}`))
			return
		}
		completion := map[string]any{
			"id": "chatcmpl-midloop", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": answer},
			}},
		}
		payload, _ := json.Marshal(completion)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(s.Close)
	s.url = s.URL
	return s
}

// gateStream parks the n-th streaming request (zero-based) until the
// returned release runs.
func (s *midLoopLLM) gateStream(n int) (release func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.gates) <= n {
		s.gates = append(s.gates, nil)
	}
	ch := make(chan struct{})
	s.gates[n] = ch
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// failStream rejects the n-th streaming request (zero-based) with the
// given status instead of serving its round.
func (s *midLoopLLM) failStream(n, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.streamStatus) <= n {
		s.streamStatus = append(s.streamStatus, 0)
	}
	s.streamStatus[n] = status
}

// parkSummaries parks every summarization call; the returned channel
// closes when the first one arrives.
func (s *midLoopLLM) parkSummaries() (arrived chan struct{}, release func()) {
	arrived = make(chan struct{})
	s.mu.Lock()
	s.park = make(chan struct{})
	park := s.park
	s.mu.Unlock()
	var once sync.Once
	s.mu.Lock()
	s.parkArrived = arrived
	s.mu.Unlock()
	return arrived, func() { once.Do(func() { close(park) }) }
}

func (s *midLoopLLM) summarizerCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.body {
		if strings.Contains(b, "compressing an agent conversation") {
			n++
		}
	}
	return n
}

// bodies snapshots every recorded request body.
func (s *midLoopLLM) bodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.body...)
}

// --- SSE frame builders: rounds carry their tool calls; usage rides
// its own choices-empty chunk, the only shape the stream parser reads
// it from. ---

func mlData(payload map[string]any) string {
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// mlToolRound frames one complete round: an assistant message whose
// calls name the tool once per id, finished for tool dispatch, with
// the reported total usage on the trailing chunk.
func mlToolRound(name string, callIDs []string, usage int) string {
	calls := make([]any, 0, len(callIDs))
	for i, id := range callIDs {
		calls = append(calls, map[string]any{
			"index": i, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": `{"query":"run it"}`},
		})
	}
	round := mlData(map[string]any{
		"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"role": "assistant", "tool_calls": calls},
		}},
	})
	round += mlData(map[string]any{
		"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "delta": map[string]any{}}},
	})
	if usage > 0 {
		round += mlData(map[string]any{
			"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
			"choices": []any{},
			"usage":   map[string]any{"prompt_tokens": usage - 10, "completion_tokens": 10, "total_tokens": usage},
		})
	}
	return round
}

// mlToolRoundArgs is mlToolRound with model-authored arguments large
// enough to move the estimator — the fallback path's fuel.
func mlToolRoundArgs(name, callID, arguments string, usage int) string {
	round := mlData(map[string]any{
		"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"index": 0, "id": callID, "type": "function",
				"function": map[string]any{"name": name, "arguments": arguments},
			}}},
		}},
	})
	round += mlData(map[string]any{
		"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "delta": map[string]any{}}},
	})
	if usage > 0 {
		round += mlData(map[string]any{
			"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
			"choices": []any{},
			"usage":   map[string]any{"prompt_tokens": usage - 10, "completion_tokens": 10, "total_tokens": usage},
		})
	}
	return round
}

// mlToolRoundContent is mlToolRound with assistant content riding the
// same delta as the calls — user-visible bulk the in-range reduction
// cannot shrink.
func mlToolRoundContent(name string, callIDs []string, content string, usage int) string {
	calls := make([]any, 0, len(callIDs))
	for i, id := range callIDs {
		calls = append(calls, map[string]any{
			"index": i, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": `{"query":"run it"}`},
		})
	}
	round := mlData(map[string]any{
		"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"role": "assistant", "content": content, "tool_calls": calls},
		}},
	})
	round += mlData(map[string]any{
		"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "delta": map[string]any{}}},
	})
	if usage > 0 {
		round += mlData(map[string]any{
			"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
			"choices": []any{},
			"usage":   map[string]any{"prompt_tokens": usage - 10, "completion_tokens": 10, "total_tokens": usage},
		})
	}
	return round
}

// mlFinal frames the terminal answer, with optional reported usage.
func mlFinal(content string, usage int) string {
	out := sseChunk(content)
	out += mlData(map[string]any{
		"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "delta": map[string]any{}}},
	})
	if usage > 0 {
		out += mlData(map[string]any{
			"id": "chatcmpl-midloop", "object": "chat.completion.chunk", "created": 0, "model": "test-model",
			"choices": []any{},
			"usage":   map[string]any{"prompt_tokens": usage - 5, "completion_tokens": 5, "total_tokens": usage},
		})
	}
	return out
}

// midLoopManager wires the lane to a real backend, a named window, and
// one counting fake tool.
func midLoopManager(t *testing.T, llmURL string, svc *backend.TurnService, window int,
	echo *fakeTool, reads *atomic.Int32) *agent.UserAgentManager {
	t.Helper()
	hook := &journalHook{}
	hook.LifecycleJournal = svc
	opts := &agent.UserAgentManagerOptions{
		ResolveModel: windowedResolver(llmURL, window),
	}
	if reads != nil {
		opts.GetAgentThread = countingThreadSource(svc, reads)
	} else {
		opts.GetAgentThread = svc.GetAgentThread
	}
	if echo != nil {
		opts.Tools = fixedTools(echo)
	}
	return hookedManager(t, opts, hook)
}

func threadRefs(t *testing.T, sqlDB *sql.DB, agent, thread string) []string {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT message_id FROM thread_context_refs WHERE agent_id = ? AND thread_id = ? ORDER BY position`,
		agent, thread)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		out = append(out, id)
	}
	return out
}

func seedSummaryOnlyThread(t *testing.T, sqlDB *sql.DB, agent, summary string) {
	t.Helper()
	thread := agent + "-sum"
	base := time.Now().Add(-time.Hour).UnixMilli()
	tx, err := sqlDB.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(
		`INSERT INTO threads (id, agent_id, previous_thread_id, kind, created_at)
		 VALUES (?, ?, ?, 'compact', ?)`, thread, agent, agent+"-t1", base)
	require.NoError(t, err)
	_, err = tx.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id, is_summary)
		 VALUES (?, ?, 'assistant', ?, ?, '', ?, 1)`, agent+"-summary", agent, summary, base+1, thread)
	require.NoError(t, err)
	_, err = tx.Exec(`UPDATE agents SET active_thread_id = ? WHERE id = ?`, thread, agent)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func userRowID(t *testing.T, sqlDB *sql.DB, agent, item string) string {
	t.Helper()
	var id string
	require.NoError(t, sqlDB.QueryRow(
		`SELECT id FROM thread_messages WHERE agent_id = ? AND role = 'user' AND turn_id = ?`,
		agent, item).Scan(&id))
	return id
}

// 1. Below threshold, usage present: no rotation, no extra thread read,
// the turn completes normally.
func TestMidLoopBelowThresholdNoRotation(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "moderate outcome"}
	llm := newMidLoopLLM(t,
		[]string{mlToolRound("echo_tool", []string{"call-1"}, 300), mlFinal("the answer", 0)},
		nil)
	sqlDB, svc := contextBackend(t, "agent-mlbelow")
	seedConversation(t, sqlDB, "agent-mlbelow", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	var reads atomic.Int32
	mgr := midLoopManager(t, llm.url, svc, 100000, echo, &reads)
	before := activeThreadID(t, sqlDB, "agent-mlbelow")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlbelow", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.NotContains(t, kinds, apitypes.LifecycleConversationRefresh)
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-mlbelow"))
	require.Equal(t, int32(1), reads.Load(), "the handoff carries the checker's single read; the checkpoint adds none")
}

// 2. Reported usage at exactly 80% of the window triggers.
func TestMidLoopExactThresholdTriggers(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small outcome"}
	llm := newMidLoopLLM(t,
		[]string{mlToolRound("echo_tool", []string{"call-1"}, 1600), mlFinal("the answer", 0)},
		[]string{"mid summary"})
	sqlDB, svc := contextBackend(t, "agent-mlexact")
	seedConversation(t, sqlDB, "agent-mlexact", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)
	before := activeThreadID(t, sqlDB, "agent-mlexact")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlexact", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	refresh := readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleConversationRefresh && ev.ItemID == prompt
	})
	require.NotNil(t, refresh.Page)
	_, _ = drainToTerminal(t, c, prompt)
	require.NotEqual(t, before, activeThreadID(t, sqlDB, "agent-mlexact"))
	require.Equal(t, 1, llm.summarizerCalls())
}

// 3. Reported 78% plus the new tool results crossing 80%: ordinary
// rotation — successor summary and references to the prompt and its
// rounds, the item rebound and still running, the refresh event with
// the page, one user row, no tool rerun, queued prompts untouched.
func TestMidLoopReportedUsagePlusToolDeltaRotates(t *testing.T) {
	big := strings.Repeat("measurement observation record ", 40) // ~180 tokens
	echo := &fakeTool{name: "echo_tool", result: big}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1", "call-2"}, 1560), // 78% + delta crosses
			mlFinal("the answer", 0),
		},
		[]string{"mid summary"})
	sqlDB, svc := contextBackend(t, "agent-mlrotate")
	seedConversation(t, sqlDB, "agent-mlrotate", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)
	source := activeThreadID(t, sqlDB, "agent-mlrotate")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlrotate", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	// A second prompt waits behind the running turn the whole time.
	queued, err := admitItem(t, mgr, "agent-mlrotate", "conn-1", sc, "behind", nil, "")
	require.NoError(t, err)

	refresh := readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleConversationRefresh && ev.ItemID == prompt
	})
	successor := refresh.ThreadID
	require.Equal(t, source, refresh.PreviousThreadID)
	require.NotNil(t, refresh.Page)
	require.NotEmpty(t, refresh.Page.Items)
	require.Equal(t, apitypes.ThreadRoleDivider, refresh.Page.Items[0].Role)

	// The queued prompt has not advanced; the running item is started,
	// its writes rebound.
	require.Equal(t, "accepted", itemStates(t, sqlDB, "agent-mlrotate")[queued])
	require.Equal(t, successor, itemThreadOfLane(t, sqlDB, "agent-mlrotate", prompt))
	require.Equal(t, "started", itemStates(t, sqlDB, "agent-mlrotate")[prompt])

	// Ordinary retention: the successor references the prompt row and
	// the round's group and both parallel outcomes.
	refs := threadRefs(t, sqlDB, "agent-mlrotate", successor)
	require.Len(t, refs, 4)
	require.Equal(t, userRowID(t, sqlDB, "agent-mlrotate", prompt), refs[0])

	// The turn finishes in the successor; one user row, no tool rerun.
	terminal, _ := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.Equal(t, successor, terminal.ThreadID)
	var users int
	require.NoError(t, sqlDB.QueryRow(
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = 'agent-mlrotate' AND role = 'user' AND turn_id = ?`,
		prompt).Scan(&users))
	require.Equal(t, 1, users)
	require.Len(t, echo.ranWith(), 2, "each scripted call ran exactly once")

	// The summarizer saw the older prefix, not the live round.
	require.Equal(t, 1, llm.summarizerCalls())

	// The next model request ran in the successor: its answer row and
	// the retained rows project as the successor's context.
	rows := compactRows(t, sqlDB, "agent-mlrotate", successor)
	require.GreaterOrEqual(t, len(rows), 3)
	require.Equal(t, [2]string{"assistant", "mid summary"}, rows[0])
}

// 4. Usage absent: the estimator over the last request and response
// triggers the same rotation — a small window and a heavy call group.
func TestMidLoopEstimatorFallbackRotates(t *testing.T) {
	blob := strings.Repeat("measurement observation record ", 300) // ~900 BPE tokens
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{mlToolRoundArgs("echo_tool", "call-1", `{"query":"`+blob+`"}`, 0), mlFinal("the answer", 0)},
		[]string{"fallback summary"})
	sqlDB, svc := contextBackend(t, "agent-mlfallback")
	seedConversation(t, sqlDB, "agent-mlfallback", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 1000, echo, nil)
	before := activeThreadID(t, sqlDB, "agent-mlfallback")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlfallback", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, _ := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.NotEqual(t, before, activeThreadID(t, sqlDB, "agent-mlfallback"))
	require.Equal(t, 1, llm.summarizerCalls())
}

// 5. A summary-only older prefix takes rolling fallback: only the
// latest complete batch is referenced; the prompt row stays in its
// physical thread.
func TestMidLoopRollingRetainsLatestBatch(t *testing.T) {
	big := strings.Repeat("measurement observation record ", 40)
	echo := &fakeTool{name: "echo_tool", result: big}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1"}, 1000), // 50%: no trigger
			mlToolRound("echo_tool", []string{"call-2"}, 1700), // 85%: trigger
			mlFinal("the answer", 0),
		},
		[]string{"rolling summary"})
	sqlDB, svc := contextBackend(t, "agent-mlrolling")
	seedSummaryOnlyThread(t, sqlDB, "agent-mlrolling", "prior summary")
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)
	source := activeThreadID(t, sqlDB, "agent-mlrolling")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlrolling", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	refresh := readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleConversationRefresh && ev.ItemID == prompt
	})
	successor := refresh.ThreadID
	require.Equal(t, source, refresh.PreviousThreadID)

	// Rolling: exactly the trailing group and its outcome.
	refs := threadRefs(t, sqlDB, "agent-mlrolling", successor)
	require.Len(t, refs, 2)
	var roles []string
	for _, r := range refs {
		var role string
		require.NoError(t, sqlDB.QueryRow(
			`SELECT role FROM thread_messages WHERE id = ?`, r).Scan(&role))
		roles = append(roles, role)
	}
	require.Equal(t, []string{"assistant", "tool"}, roles)

	// The prompt row stays physically in the source thread.
	require.Equal(t, source, rowThreadOf(t, sqlDB, userRowID(t, sqlDB, "agent-mlrolling", prompt)))

	// The refresh page shows divider, summary, then the batch.
	var pageRoles []string
	for _, it := range refresh.Page.Items {
		pageRoles = append(pageRoles, string(it.Role))
	}
	require.Equal(t, []string{"divider", "assistant", "tool"}, pageRoles[:3])

	_, _ = drainToTerminal(t, c, prompt)
	require.Equal(t, 1, llm.summarizerCalls())
}

// 6. One operation per checkpoint: usage stays over the threshold
// after the rotation, and the turn finishes with exactly one
// summarization — no same-checkpoint second pass.
func TestMidLoopOneOperationPerCheckpoint(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1"}, 1900), // 95%: trigger at checkpoint one
			mlToolRound("echo_tool", []string{"call-2"}, 1900), // still 95%: a later round
			mlFinal("the answer", 0),
		},
		[]string{"first summary", "second summary"})
	sqlDB, svc := contextBackend(t, "agent-mlone")
	seedConversation(t, sqlDB, "agent-mlone", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlone", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	rotations := 0
	for _, k := range kinds {
		if k == apitypes.LifecycleConversationRefresh {
			rotations++
		}
	}
	require.Equal(t, 2, rotations, "one rotation per completed round, none extra")
	require.Equal(t, 2, llm.summarizerCalls())
}

// 7. An unknown window skips the percentage policy — no invented
// capacity, no rotation.
func TestMidLoopUnknownWindowSkips(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{mlToolRound("echo_tool", []string{"call-1"}, 999999), mlFinal("the answer", 0)},
		nil)
	sqlDB, svc := contextBackend(t, "agent-mlzero")
	seedConversation(t, sqlDB, "agent-mlzero", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 0, echo, nil)
	before := activeThreadID(t, sqlDB, "agent-mlzero")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlzero", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.NotContains(t, kinds, apitypes.LifecycleConversationRefresh)
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-mlzero"))
}

// 8. A summarization failure degrades: no successor, no refresh, the
// turn completes uncompacted in the original thread.
func TestMidLoopSummarizerFailureDegrades(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{mlToolRound("echo_tool", []string{"call-1"}, 1900), mlFinal("the answer", 0)},
		[]string{""}) // empty summary: the engine's no-content failure
	sqlDB, svc := contextBackend(t, "agent-mldegrade")
	seedConversation(t, sqlDB, "agent-mldegrade", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)
	before := activeThreadID(t, sqlDB, "agent-mldegrade")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mldegrade", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.NotContains(t, kinds, apitypes.LifecycleConversationRefresh)
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-mldegrade"))
	require.Equal(t, before, terminal.ThreadID, "the answer settles in the unchanged thread")
}

// 8b. A rotation the commit's guards reject — here a forged open
// invocation, the boundary the rotation must never cross — degrades
// like a summarization failure: no successor, no refresh, and the
// turn completes in the unchanged thread.
func TestMidLoopRotationFailureDegrades(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{mlToolRound("echo_tool", []string{"call-1"}, 1900), mlFinal("the answer", 0)},
		[]string{"mid summary"})
	parked, release := llm.parkSummaries()
	sqlDB, svc := contextBackend(t, "agent-mlrotfail")
	seedConversation(t, sqlDB, "agent-mlrotfail", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)
	before := activeThreadID(t, sqlDB, "agent-mlrotfail")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlrotfail", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	<-parked
	// The rotation is parked behind its summarization: forge an open
	// invocation for the running item, so the commit's between-rounds
	// guard rejects the attempt instead of crossing it.
	_, err = sqlDB.Exec(`INSERT INTO tool_invocations
		(id, agent_id, item_id, call_id, name, args, started_ms, state)
		VALUES ('ghost', 'agent-mlrotfail', ?, 'call-x', 'echo_tool', '{}', 0, 'open')`, prompt)
	require.NoError(t, err)
	release()

	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.NotContains(t, kinds, apitypes.LifecycleConversationRefresh)
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-mlrotfail"))
	require.Equal(t, before, terminal.ThreadID, "the answer settles in the unchanged thread")
	require.Equal(t, 1, llm.summarizerCalls())
}

// 8c. No fit gate: an oversized successor the provider rejects fails
// through the ordinary turn-error path — no second rotation, no retry,
// no truncation — and the committed transition stays real.
func TestMidLoopProviderRejectionAfterRotation(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1"}, 1900), // 95%: rotate
			mlToolRound("echo_tool", []string{"call-2"}, 1900), // still oversized
		},
		[]string{"mid summary"})
	llm.failStream(1, 400)
	sqlDB, svc := contextBackend(t, "agent-mlrej")
	seedConversation(t, sqlDB, "agent-mlrej", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlrej", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemFailed, terminal.Outcome)
	require.Contains(t, kinds, apitypes.LifecycleConversationRefresh)
	require.Equal(t, 1, llm.summarizerCalls(), "no retry: exactly the one rotation's summarization")
	require.Equal(t, int32(2), llm.streams.Load(), "the rejected request was sent, not retried")
	successor := activeThreadID(t, sqlDB, "agent-mlrej")
	require.Equal(t, successor, terminal.ThreadID, "settlement anchors to the committed successor")
	require.Equal(t, successor, itemThreadOfLane(t, sqlDB, "agent-mlrej", prompt))
}

// 9. A stop while the summarization is in flight cancels the turn: no
// successor, no refresh, and the queue's stop semantics hold.
func TestMidLoopStopDuringSummarizeCancels(t *testing.T) {
	big := strings.Repeat("measurement observation record ", 40)
	echo := &fakeTool{name: "echo_tool", result: big}
	llm := newMidLoopLLM(t,
		[]string{mlToolRound("echo_tool", []string{"call-1"}, 1560), mlFinal("the answer", 0)},
		[]string{"mid summary"})
	parked, release := llm.parkSummaries()
	sqlDB, svc := contextBackend(t, "agent-mlstop1")
	seedConversation(t, sqlDB, "agent-mlstop1", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)
	before := activeThreadID(t, sqlDB, "agent-mlstop1")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlstop1", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	behind, err := admitItem(t, mgr, "agent-mlstop1", "conn-1", sc, "behind", nil, "")
	require.NoError(t, err)
	<-parked
	mgr.Interrupt("agent-mlstop1")
	release()

	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemStopped, terminal.Outcome)
	require.NotContains(t, kinds, apitypes.LifecycleConversationRefresh)
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-mlstop1"))
	// Waiting work means the stop arms the pause gate: the queued
	// prompt does not run on its own.
	require.Equal(t, "accepted", itemStates(t, sqlDB, "agent-mlstop1")[behind])
	final := queueStateUntil(t, c, func(q wstypes.QueueStateEvent) bool {
		return q.Active == nil && len(q.Items) == 1
	})
	require.True(t, final.Paused, "a stop with waiting work pauses the queue")
}

// 10. A stop after the rotation committed keeps the transition: the
// item settles stopped in the successor, and the refresh was
// delivered.
func TestMidLoopStopAfterCommitKeepsSuccessor(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1"}, 1900),
			mlToolRound("echo_tool", []string{"call-2"}, 1900),
		},
		[]string{"mid summary"})
	release2 := llm.gateStream(1) // park the second model request
	sqlDB, svc := contextBackend(t, "agent-mlstop2")
	seedConversation(t, sqlDB, "agent-mlstop2", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlstop2", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	refresh := readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleConversationRefresh && ev.ItemID == prompt
	})
	successor := refresh.ThreadID
	require.NotNil(t, refresh.Page)

	// The second request is parked mid-flight: the stop lands here.
	mgr.Interrupt("agent-mlstop2")
	release2()
	terminal, _ := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemStopped, terminal.Outcome)
	require.Equal(t, successor, activeThreadID(t, sqlDB, "agent-mlstop2"))
	require.Equal(t, successor, itemThreadOfLane(t, sqlDB, "agent-mlstop2", prompt))
	require.Equal(t, successor, terminal.ThreadID, "settlement anchors to the committed successor")
}

// 11. The Compacting flag rides lane_status through the mid-loop
// summarization while the item stays Working.
func TestMidLoopCompactingStatusWhileWorking(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{mlToolRound("echo_tool", []string{"call-1"}, 1900), mlFinal("the answer", 0)},
		[]string{"mid summary"})
	parked, release := llm.parkSummaries()
	sqlDB, svc := contextBackend(t, "agent-mlstatus")
	seedConversation(t, sqlDB, "agent-mlstatus", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-mlstatus", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	<-parked

	var during []wstypes.LaneStatus
	compacting := laneStatusUntil(t, c, func(s wstypes.LaneStatus) bool { return s.Compacting }, &during)
	require.True(t, compacting.Working, "the item keeps working while mid-loop compaction runs")

	release()
	var after []wstypes.LaneStatus
	cleared := laneStatusUntil(t, c, func(s wstypes.LaneStatus) bool { return !s.Compacting && s.Working }, &after)
	require.True(t, cleared.Active)
	laneStatusUntil(t, c, func(s wstypes.LaneStatus) bool { return !s.Compacting && !s.Working }, nil)
}

// 12. A connection joining after a committed rotation gets the
// authoritative snapshot: the successor's page, and a live prefix that
// does not re-insert the original user row at the tail.
func TestMidLoopReconnectAfterRotation(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1"}, 1900),
			mlFinal("the answer", 0),
		},
		[]string{"mid summary"})
	release2 := llm.gateStream(1)
	sqlDB, svc := contextBackend(t, "agent-mljoin")
	seedConversation(t, sqlDB, "agent-mljoin", []seedTurn{
		{turnID: "t1", callID: "call-1", rowID: "row-old", name: "fetch", result: "small"},
	})
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)

	c1, sc1 := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mljoin", "conn-1", sc1, "continue", nil, "")
	require.NoError(t, err)
	refresh := readLifecycle(t, c1, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleConversationRefresh && ev.ItemID == prompt
	})
	successor := refresh.ThreadID

	// A device joins while the post-rotation request is parked: the
	// authoritative snapshot is the successor's projection, and the
	// restored live prefix does not re-insert the original user row.
	snap, err := svc.SyncSnapshot(context.Background(), "agent-mljoin", 20)
	require.NoError(t, err)
	require.Equal(t, successor, snap.Page.ThreadID)
	require.NotEmpty(t, snap.Page.Items)
	require.Equal(t, apitypes.ThreadRoleDivider, snap.Page.Items[0].Role)
	found := false
	for _, it := range snap.Page.Items {
		if it.ID == userRowID(t, sqlDB, "agent-mljoin", prompt) {
			found = true
		}
	}
	require.True(t, found, "the successor page carries the referenced prompt row")

	c2, sc2 := wsPair(t)
	ready := readSyncReady(t, mgr, "agent-mljoin", "conn-2", sc2, c2)
	require.NotNil(t, ready.Queue.Active)
	require.Equal(t, prompt, ready.Queue.Active.ID)
	require.Empty(t, ready.Queue.Active.UserMessageID,
		"the live tail must not re-insert the user row the projection already places")
	require.NotNil(t, ready.Prefix)
	require.Equal(t, prompt, ready.Prefix.ItemID)

	release2()
	terminal, _ := drainToTerminal(t, c1, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.Equal(t, successor, terminal.ThreadID)
}

// 12b. Reconnect after a ROLLING rotation: the prompt row stays in its
// earlier segment, so the successor's snapshot page must not carry it,
// and the restored live prefix must not re-insert it at the tail.
func TestMidLoopReconnectAfterRollingRotation(t *testing.T) {
	big := strings.Repeat("measurement observation record ", 40)
	echo := &fakeTool{name: "echo_tool", result: big}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1"}, 1000), // 50%: no trigger
			mlToolRound("echo_tool", []string{"call-2"}, 1700), // 85%: rolling rotation
			mlFinal("the answer", 0),
		},
		[]string{"rolling summary"})
	release3 := llm.gateStream(2) // park the post-rotation request
	sqlDB, svc := contextBackend(t, "agent-mljoinroll")
	seedSummaryOnlyThread(t, sqlDB, "agent-mljoinroll", "prior summary")
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)

	c1, sc1 := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mljoinroll", "conn-1", sc1, "continue", nil, "")
	require.NoError(t, err)
	refresh := readLifecycle(t, c1, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleConversationRefresh && ev.ItemID == prompt
	})
	successor := refresh.ThreadID

	// A device joins while the post-rotation request is parked: the
	// authoritative snapshot is the successor's page, which carries the
	// summary and the referenced batch but not the prompt row.
	snap, err := svc.SyncSnapshot(context.Background(), "agent-mljoinroll", 20)
	require.NoError(t, err)
	require.Equal(t, successor, snap.Page.ThreadID)
	userID := userRowID(t, sqlDB, "agent-mljoinroll", prompt)
	for _, it := range snap.Page.Items {
		require.NotEqual(t, userID, it.ID,
			"rolling: the prompt row stays in its earlier display segment")
	}

	c2, sc2 := wsPair(t)
	ready := readSyncReady(t, mgr, "agent-mljoinroll", "conn-2", sc2, c2)
	require.NotNil(t, ready.Queue.Active)
	require.Equal(t, prompt, ready.Queue.Active.ID)
	require.Empty(t, ready.Queue.Active.UserMessageID,
		"the live tail must not re-insert the user row the earlier segment already shows")
	require.NotNil(t, ready.Prefix)
	require.Equal(t, prompt, ready.Prefix.ItemID)

	release3()
	terminal, _ := drainToTerminal(t, c1, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.Equal(t, successor, terminal.ThreadID)
}

// 12c. A rolling summary input that overflows preflight is reduced in
// memory: the big first-round result inside the selected range becomes
// the neutral omission note — never a restore placeholder — and the
// rotation still commits so the turn completes in the successor.
func TestMidLoopRollingReductionUsesNeutralOmission(t *testing.T) {
	big := strings.Repeat("measurement observation record ", 300)
	echo := &fakeTool{name: "echo_tool", result: big}
	llm := newMidLoopLLM(t,
		[]string{
			// reported usage below the trigger; the estimated delta over
			// the big round-1 result still trips the first rotation
			mlToolRound("echo_tool", []string{"call-1"}, 1000),
			// crossing it: the second rolling rotation
			mlToolRound("echo_tool", []string{"call-2"}, 1700),
			mlFinal("the answer", 0),
		},
		[]string{"rolling one", "rolling two"})
	sqlDB, svc := contextBackend(t, "agent-mlnote")
	seedSummaryOnlyThread(t, sqlDB, "agent-mlnote", "prior summary")
	mgr := midLoopManager(t, llm.url, svc, 1300, echo, nil)

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlnote", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	refreshes := 0
	for _, k := range kinds {
		if k == apitypes.LifecycleConversationRefresh {
			refreshes++
		}
	}
	require.Equal(t, 2, refreshes,
		"two rolling rotations: the second one's summary input carries the big result")

	// The second rotation's summarization is the reduced one: its
	// selected range reaches the big first-round result, which the
	// preflight overflows on and the reduction replaces.
	var noted int
	nonStreaming := 0
	for _, b := range llm.bodies() {
		if strings.Contains(b, `"stream":true`) {
			continue
		}
		nonStreaming++
		if strings.Contains(b, "[tool output omitted from this summarization request:") {
			noted++
		}
		require.NotContains(t, b, "[elided tool output:",
			"a temporary omission is not a restore placeholder")
	}
	require.Equal(t, 2, nonStreaming, "one summarization per rotation")
	require.Equal(t, 1, noted, "exactly the second rotation's input was reduced")
}

// 13. A rolling selection too large to compact usefully — its bulk is
// assistant content, which the in-range reduction cannot shrink —
// degrades silently: no rotation, no summarization wire call (the fit
// check fails first), and the turn finishes in the unchanged thread.
// No retry loop: the checkpoint runs once and the turn ends.
func TestMidLoopOversizedRangeDegradesWithoutRetry(t *testing.T) {
	big := strings.Repeat("measurement observation record ", 600)
	echo := &fakeTool{name: "echo_tool", result: "small"}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRoundContent("echo_tool", []string{"call-1"}, big, 1000), // 50%: no trigger
			mlToolRound("echo_tool", []string{"call-2"}, 1700),             // 85%: trigger
			mlFinal("the answer", 0),
		},
		[]string{"rolling summary"})
	sqlDB, svc := contextBackend(t, "agent-mlbig")
	seedSummaryOnlyThread(t, sqlDB, "agent-mlbig", "prior summary")
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)
	before := activeThreadID(t, sqlDB, "agent-mlbig")

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mlbig", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.NotContains(t, kinds, apitypes.LifecycleConversationRefresh)
	require.Equal(t, before, activeThreadID(t, sqlDB, "agent-mlbig"))
	require.Equal(t, before, terminal.ThreadID, "the answer settles in the unchanged thread")
	require.Equal(t, 0, llm.summarizerCalls(), "the fit check fails before any summarization request")
	require.Equal(t, int32(3), llm.streams.Load(), "no retry: exactly the three scripted model requests")
}

// 14. The rolling continuation's replacement context opens with a
// user-side checkpoint frame: its request would otherwise carry no
// user-role message — rolling summarizes the prompt itself — and
// message-shape-strict providers reject such lists outright. The
// ordinary frame keeps the assistant seed beside its retained prompt.
func TestMidLoopRollingContinuationCarriesUserFrame(t *testing.T) {
	big := strings.Repeat("measurement observation record ", 40)
	echo := &fakeTool{name: "echo_tool", result: big}
	llm := newMidLoopLLM(t,
		[]string{
			mlToolRound("echo_tool", []string{"call-1"}, 1000), // 50%: no trigger
			mlToolRound("echo_tool", []string{"call-2"}, 1700), // 85%: rolling rotation
			mlFinal("the answer", 0),
		},
		[]string{"rolling summary"})
	sqlDB, svc := contextBackend(t, "agent-mluserframe")
	seedSummaryOnlyThread(t, sqlDB, "agent-mluserframe", "prior summary")
	mgr := midLoopManager(t, llm.url, svc, 2000, echo, nil)

	c, sc := wsPair(t)
	prompt, err := admitItem(t, mgr, "agent-mluserframe", "conn-1", sc, "continue", nil, "")
	require.NoError(t, err)
	terminal, kinds := drainToTerminal(t, c, prompt)
	require.Equal(t, apitypes.ItemCompleted, terminal.Outcome)
	require.Contains(t, kinds, apitypes.LifecycleConversationRefresh)

	// The post-rotation model request (the third streaming one) must
	// open with the user-side checkpoint frame, ahead of the retained
	// batch's assistant call group.
	type wireMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type wireReq struct {
		Messages []wireMsg `json:"messages"`
	}
	var modelReqs []wireReq
	llm.mu.Lock()
	for _, b := range llm.body {
		if !strings.Contains(b, `"stream":true`) {
			continue // the summarizer's non-streaming request
		}
		var r wireReq
		require.NoError(t, json.Unmarshal([]byte(b), &r))
		modelReqs = append(modelReqs, r)
	}
	llm.mu.Unlock()
	require.Len(t, modelReqs, 3, "two tool rounds plus the final answer")
	post := modelReqs[2]
	require.Equal(t, "user", post.Messages[0].Role,
		"the rolling checkpoint rides as a user-side frame")
	require.Equal(t, "rolling summary", post.Messages[0].Content)
	require.Equal(t, "assistant", post.Messages[1].Role,
		"the retained batch's call group follows the frame")
}

// --- shared helpers ---

func itemThreadOfLane(t *testing.T, sqlDB *sql.DB, agent, item string) string {
	t.Helper()
	var thread sql.NullString
	require.NoError(t, sqlDB.QueryRow(
		`SELECT thread_id FROM queue_items WHERE agent_id = ? AND id = ?`, agent, item).Scan(&thread))
	return thread.String
}

func rowThreadOf(t *testing.T, sqlDB *sql.DB, rowID string) string {
	t.Helper()
	var thread string
	require.NoError(t, sqlDB.QueryRow(
		`SELECT thread_id FROM thread_messages WHERE id = ?`, rowID).Scan(&thread))
	return thread
}
