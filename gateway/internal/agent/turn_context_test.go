package agent_test

// Working-context pins over one real store: the next turn's model
// call carries the prior turns' replay-eligible history — user,
// assistant, and tool rounds with paired call ids — and the prompt
// appears exactly once, live or across a restart. Required writes
// fail the turn at message level; a stop during the history load is
// a quiet stop.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/cloudwego/eino/components/tool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/sqlstore"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// contextMessage is one model-request message, with the structure the
// replay assertions read.
type contextMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls,omitempty"`
}

type contextRequest struct {
	Messages []contextMessage `json:"messages"`
}

// contextLLM serves one scripted SSE response per request, in order,
// and records every raw request body.
type contextLLM struct {
	*httptest.Server
	mu        sync.Mutex
	bodies    []string
	responses []string
}

func (s *contextLLM) requests() []contextRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]contextRequest, len(s.bodies))
	for i, b := range s.bodies {
		_ = json.Unmarshal([]byte(b), &out[i])
	}
	return out
}

func newContextLLM(t *testing.T, responses ...string) *contextLLM {
	t.Helper()
	s := &contextLLM{responses: responses}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		_, _ = body.ReadFrom(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body.String())
		i := len(s.bodies) - 1
		s.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		if i < len(s.responses) {
			_, _ = w.Write([]byte(s.responses[i]))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(s.Close)
	return s
}

// contextBackend boots a migrated store and its backend, and seeds one
// agent — the real append/replay path the gateway binds.
func contextBackend(t *testing.T, agentID string) (*sql.DB, *backend.TurnService) {
	t.Helper()
	sqlDB, err := sqlstore.OpenSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x43}, 32))
	require.NoError(t, err)
	_, err = sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES (?, 'Context', 'globe', '', '', '', '')`, agentID)
	require.NoError(t, err)
	return sqlDB, svc
}

// contextManager wires a manager whose persistence and replay are the
// real backend methods; an optional sink override injects failures.
var errSink = errors.New("sink unavailable")

// queueStateUntil drains frames until a queue snapshot satisfies the
// predicate.
func queueStateUntil(t *testing.T, c *websocket.Conn, done func(wstypes.QueueStateEvent) bool) wstypes.QueueStateEvent {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeQueueState {
			continue
		}
		p := eventPayload[wstypes.QueueStateEvent](t, ev)
		if done(p) {
			return p
		}
	}
}

func contextManager(t *testing.T, llm *contextLLM, svc *backend.TurnService,
	sink agent.TurnSink, tools ...tool.InvokableTool) *agent.UserAgentManager {
	t.Helper()
	opts := &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(llm.URL),
		PersistTurn:    svc.AppendThreadMessages,
		GetAgentThread: svc.GetAgentThread,
	}
	if sink != nil {
		opts.PersistTurn = sink
	}
	if len(tools) > 0 {
		opts.Tools = fixedTools(tools...)
	}
	return agent.NewUserAgentManager(zerolog.Nop(), opts)
}

// rawThreadRow is this file's private scan shape for one storage row:
// the shared row type is backend-internal, and the assertions here
// only read fields.
type rawThreadRow struct {
	ID        string
	Role      apitypes.ThreadRole
	Content   string
	CreatedAt int64
	TurnID    string
	ToolCalls string
}

func threadRows(t *testing.T, sqlDB *sql.DB, agentID string) []rawThreadRow {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT id, role, content, created_at, turn_id, COALESCE(tool_calls, '')
		 FROM thread_messages WHERE agent_id = ? ORDER BY created_at ASC, id ASC`, agentID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []rawThreadRow
	for rows.Next() {
		var r rawThreadRow
		require.NoError(t, rows.Scan(&r.ID, &r.Role, &r.Content, &r.CreatedAt, &r.TurnID, &r.ToolCalls))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestNextTurnReceivesPriorContext(t *testing.T) {
	llm := newContextLLM(t, sseChunk("answer one"), sseChunk("answer two"))
	sqlDB, svc := contextBackend(t, "agent-ctx")
	mgr := contextManager(t, llm, svc, nil)

	c, sc := wsPair(t)
	submit(t, mgr, "agent-ctx", "conn-1", sc, "first")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "answer one", p.Content)
	submit(t, mgr, "agent-ctx", "conn-1", sc, "second")
	_, p2 := readUntilMessage(t, c)
	require.Equal(t, "answer two", p2.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 2)

	// The first turn carries its prompt alone — the committed user row
	// came back from the store and was not appended a second time.
	require.Len(t, reqs[0].Messages, 1)
	require.Equal(t, "user", reqs[0].Messages[0].Role)
	require.Equal(t, "first", reqs[0].Messages[0].Content)

	// The second turn receives A before B — prompt, answer, prompt.
	require.Len(t, reqs[1].Messages, 3)
	require.Equal(t, "first", reqs[1].Messages[0].Content)
	require.Equal(t, "answer one", reqs[1].Messages[1].Content)
	require.Equal(t, "second", reqs[1].Messages[2].Content)

	// One durable row per turn message: no duplicated prompt.
	var users int
	require.NoError(t, sqlDB.QueryRow(
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = 'agent-ctx' AND role = 'user'`).Scan(&users))
	require.Equal(t, 2, users)
}

func TestQueuedSubmissionsShareContext(t *testing.T) {
	llm := newContextLLM(t, sseChunk("a"), sseChunk("b"))
	_, svc := contextBackend(t, "agent-q")
	mgr := contextManager(t, llm, svc, nil)

	c, sc := wsPair(t)
	submit(t, mgr, "agent-q", "conn-1", sc, "queued first")
	_, err := mgr.Submit("agent-q", "conn-1", sc, "queued second", nil)
	require.NoError(t, err)
	for range 2 {
		_, p := readUntilMessage(t, c)
		require.NotEmpty(t, p.Content)
	}

	reqs := llm.requests()
	require.Len(t, reqs, 2)
	require.Equal(t, "queued first", reqs[0].Messages[0].Content)
	require.Len(t, reqs[1].Messages, 3)
	require.Equal(t, "a", reqs[1].Messages[1].Content)
	require.Equal(t, "queued second", reqs[1].Messages[2].Content)
}

func TestToolContextReplayedWithCallIDs(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	llm := newContextLLM(t,
		toolCallChunk("echo_tool")+toolCallDone(), sseChunk("after tool"), sseChunk("done"))
	_, svc := contextBackend(t, "agent-tctx")
	mgr := contextManager(t, llm, svc, nil, echo)

	c, sc := wsPair(t)
	submit(t, mgr, "agent-tctx", "conn-1", sc, "search for otters")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "after tool", p.Content)
	submit(t, mgr, "agent-tctx", "conn-1", sc, "what did it say")
	_, p2 := readUntilMessage(t, c)
	require.Equal(t, "done", p2.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 3, "a tool turn makes two model calls, the follow-up one")

	// The react loop itself carries the round within turn one: its
	// second model call sees the assistant call and its outcome.
	inTurn := reqs[1].Messages
	require.Len(t, inTurn, 3)
	require.Equal(t, "assistant", inTurn[1].Role)
	require.Equal(t, "call-1", inTurn[1].ToolCalls[0].ID)
	require.Equal(t, "tool", inTurn[2].Role)
	require.Equal(t, "call-1", inTurn[2].ToolCallID)

	// The follow-up carries the tool round before the new prompt,
	// rebuilt from the durable rows.
	second := reqs[2].Messages
	require.Len(t, second, 5)
	require.Equal(t, "user", second[0].Role)
	require.Equal(t, "search for otters", second[0].Content)
	require.Equal(t, "assistant", second[1].Role)
	require.Len(t, second[1].ToolCalls, 1)
	require.Equal(t, "call-1", second[1].ToolCalls[0].ID)
	require.Equal(t, "echo_tool", second[1].ToolCalls[0].Function.Name)
	require.Equal(t, `{"query":"plush otters"}`, second[1].ToolCalls[0].Function.Arguments)
	require.Equal(t, "tool", second[2].Role)
	require.Equal(t, "call-1", second[2].ToolCallID)
	require.Equal(t, "tool says hi", second[2].Content)
	require.Equal(t, "assistant", second[3].Role)
	require.Equal(t, "after tool", second[3].Content)
	require.Equal(t, "user", second[4].Role)
	require.Equal(t, "what did it say", second[4].Content)
}

func TestColdRestartRebuildsContextFromReplay(t *testing.T) {
	llm := newContextLLM(t, sseChunk("first answer"), sseChunk("second answer"))
	sqlDB, svc := contextBackend(t, "agent-restart")
	mgr := contextManager(t, llm, svc, nil)

	c, sc := wsPair(t)
	submit(t, mgr, "agent-restart", "conn-1", sc, "old prompt")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "first answer", p.Content)

	// A fresh backend over the same store: the read is cold, exactly
	// as after a restart.
	_, _, cold, err := backend.New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x43}, 32))
	require.NoError(t, err)
	coldMgr := contextManager(t, llm, cold, nil)
	submit(t, coldMgr, "agent-restart", "conn-1", sc, "new prompt")
	_, p2 := readUntilMessage(t, c)
	require.Equal(t, "second answer", p2.Content)

	reqs := llm.requests()
	require.Len(t, reqs, 2)
	msgs := reqs[1].Messages
	require.Len(t, msgs, 3)
	require.Equal(t, "old prompt", msgs[0].Content)
	require.Equal(t, "first answer", msgs[1].Content)
	require.Equal(t, "new prompt", msgs[2].Content)
}

func TestStopDuringReplayLoadIsQuietStop(t *testing.T) {
	llm := newContextLLM(t, sseChunk("never reached"))
	sqlDB, svc := contextBackend(t, "agent-stopload")

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	replay := func(ctx context.Context, agentID string) ([]backend.AgentThreadMessage, error) {
		once.Do(func() { close(started) })
		<-release
		return nil, ctx.Err()
	}
	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(llm.URL),
		PersistTurn:    svc.AppendThreadMessages,
		GetAgentThread: replay,
	})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-stopload", "conn-1", sc, "hi")
	<-started
	mgr.Interrupt("agent-stopload")
	close(release)

	// The queue state after the turn is the deterministic signal: a
	// quiet stop does not arm the pause gate.
	snapshot := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
		return p.Active == nil && len(p.Items) == 0
	})
	require.False(t, snapshot.Paused, "a stop during load must not pause the queue")

	rows := threadRows(t, sqlDB, "agent-stopload")
	require.Len(t, rows, 1, "only the user row stands; no error row")
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Empty(t, llm.requests(), "the model was never called")
}

func TestToolRowPersistFailureFailsTurn(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	llm := newContextLLM(t, toolCallChunk("echo_tool")+toolCallDone(), sseChunk("never shown"))
	sqlDB, svc := contextBackend(t, "agent-tsink")

	var sawTool bool
	var mu sync.Mutex
	sink := func(ctx context.Context, agentID string, msgs []backend.ThreadMessageDraft) error {
		for _, m := range msgs {
			if m.Role == apitypes.ThreadRoleTool {
				mu.Lock()
				sawTool = true
				mu.Unlock()
				return errSink
			}
		}
		return svc.AppendThreadMessages(ctx, agentID, msgs)
	}
	mgr := contextManager(t, llm, svc, sink, echo)

	c, sc := wsPair(t)
	_, err := mgr.Submit("agent-tsink", "conn-1", sc, "search", nil)
	require.NoError(t, err)
	_, err = mgr.Submit("agent-tsink", "conn-1", sc, "waits behind", nil)
	require.NoError(t, err)

	p := eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
	require.Contains(t, p.Message, "persist turn message")
	mu.Lock()
	require.True(t, sawTool, "the wire still reported the invocation")
	mu.Unlock()

	snapshot := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
		return p.Active == nil && p.Paused
	})
	require.Len(t, snapshot.Items, 1)

	// The follow-up never reaches the model; the durable rows hold
	// the user, the recorded group, and the error row — no outcome.
	reqs := llm.requests()
	for _, req := range reqs {
		last := req.Messages[len(req.Messages)-1]
		require.NotEqual(t, "waits behind", last.Content)
	}
	var roles []apitypes.ThreadRole
	for _, row := range threadRows(t, sqlDB, "agent-tsink") {
		roles = append(roles, row.Role)
	}
	require.Equal(t, []apitypes.ThreadRole{
		apitypes.ThreadRoleUser, apitypes.ThreadRoleAssistant, apitypes.ThreadRoleError,
	}, roles)
}

func TestAssistantSettleFailureFailsTurn(t *testing.T) {
	llm := newContextLLM(t, sseChunk("streamed but not durable"))
	_, svc := contextBackend(t, "agent-asink")

	sink := func(ctx context.Context, agentID string, msgs []backend.ThreadMessageDraft) error {
		for _, m := range msgs {
			if m.Role == apitypes.ThreadRoleAssistant && len(m.ToolCalls) == 0 {
				return errSink
			}
		}
		return svc.AppendThreadMessages(ctx, agentID, msgs)
	}
	mgr := contextManager(t, llm, svc, sink)

	c, sc := wsPair(t)
	submit(t, mgr, "agent-asink", "conn-1", sc, "hi")

	var p wstypes.ErrorEvent
	for {
		ev := readEvent(t, c)
		if ev.Type == wstypes.EventTypeError {
			p = eventPayload[wstypes.ErrorEvent](t, ev)
			break
		}
	}
	require.Contains(t, p.Message, "persist assistant message")
}

// A required-write failure racing a user stop is still a message
// failure: the persist classification wins over the cancel check.
func TestPersistFailureRacingStopIsMessageFailure(t *testing.T) {
	echo := &fakeTool{name: "echo_tool", result: "tool says hi"}
	llm := newContextLLM(t, toolCallChunk("echo_tool")+toolCallDone(), sseChunk("never shown"))
	sqlDB, svc := contextBackend(t, "agent-race")

	var mgr *agent.UserAgentManager
	var once sync.Once
	sink := func(ctx context.Context, agentID string, msgs []backend.ThreadMessageDraft) error {
		for _, m := range msgs {
			if m.Role == apitypes.ThreadRoleAssistant && len(m.ToolCalls) > 0 {
				once.Do(func() { mgr.Interrupt(agentID) })
				return errSink
			}
		}
		return svc.AppendThreadMessages(ctx, agentID, msgs)
	}
	mgr = contextManager(t, llm, svc, sink, echo)

	c, sc := wsPair(t)
	_, err := mgr.Submit("agent-race", "conn-1", sc, "search", nil)
	require.NoError(t, err)

	p := eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
	require.Contains(t, p.Message, "persist turn message",
		"the write failure must not be reclassified as a quiet stop")

	var errorsRows int
	require.NoError(t, sqlDB.QueryRow(
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = 'agent-race' AND role = 'error'`).
		Scan(&errorsRows))
	require.Equal(t, 1, errorsRows, "the failure persists an error row")
}
