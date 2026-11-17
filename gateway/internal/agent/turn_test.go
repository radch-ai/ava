package agent_test

// Turn pins: a submitted prompt runs one LLM turn end to end —
// resolve, query, answer broadcast — and failures surface as error
// events without killing the worker. A message-level failure pauses
// the queue; an explicit selection resumes it. Tests use a fake
// resolver and an OpenAI-compatible test server; no real provider
// is touched.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// fakeOpenAI streams one assembled reply as a single-delta SSE turn.
func fakeOpenAI(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return sseOpenAI(t, reply)
}

// readUntilMessage drains mid-turn events — chunks, statuses, tool
// activity, queue traffic — until the final message event; it
// returns the envelope and its payload.
func readUntilMessage(t *testing.T, c *websocket.Conn) (wstypes.Event, wstypes.MessageEvent) {
	t.Helper()
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeMessageChunk, wstypes.EventTypeLaneStatus,
			wstypes.EventTypeToolCall, wstypes.EventTypeToolResult,
			wstypes.EventTypeQueueState, wstypes.EventTypeQueueResult:
			continue
		case wstypes.EventTypeMessage:
			return ev, eventPayload[wstypes.MessageEvent](t, ev)
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}
}

// readUntilError drains statuses, tool activity, and queue traffic
// until the error event; it returns the envelope.
func readUntilError(t *testing.T, c *websocket.Conn) wstypes.Event {
	t.Helper()
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus, wstypes.EventTypeToolCall,
			wstypes.EventTypeToolResult, wstypes.EventTypeQueueState,
			wstypes.EventTypeQueueResult:
			continue
		case wstypes.EventTypeError:
			return ev
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}
}

// readQueueState drains until the next queue snapshot and returns
// its payload.
func readQueueState(t *testing.T, c *websocket.Conn) wstypes.QueueStateEvent {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type == wstypes.EventTypeQueueState {
			return eventPayload[wstypes.QueueStateEvent](t, ev)
		}
	}
}

// submit enqueues one prompt and returns the admitted item's stable
// id — the turn's stream id on the wire.
func submit(t *testing.T, mgr *agent.UserAgentManager, agentID, connID string, conn *websocket.Conn, content string) string {
	t.Helper()
	id, err := mgr.Submit(agentID, connID, conn, content, nil)
	require.NoError(t, err)
	return id
}

// wsPair dials a mock server and returns the client and server ends
// of one connection — the shape the gateway hands to lanes.
func wsPair(t *testing.T) (client, server *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *websocket.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer closeConn(c)
		serverCh <- c
		for { // keep the connection alive
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.Close)
	client = connectWS(t, s.URL)
	t.Cleanup(func() { closeConn(client) })
	select {
	case server = <-serverCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server end never accepted")
	}
	return client, server
}

// readEvent reads one server-to-client event from the connection.
func readEvent(t *testing.T, c *websocket.Conn) wstypes.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ev wstypes.Event
	require.NoError(t, wsjson.Read(ctx, c, &ev))
	return ev
}

// eventPayload decodes an event's Data into the payload type.
func eventPayload[T any](t *testing.T, ev wstypes.Event) T {
	t.Helper()
	raw, err := json.Marshal(ev.Data)
	require.NoError(t, err)
	var p T
	require.NoError(t, json.Unmarshal(raw, &p))
	return p
}

func fixedResolver(url string) func(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
	return func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{BaseURL: url, APIKey: "sk-test", Model: "test-model"}, nil
	}
}

// sseChunk renders one chat.completion.chunk SSE frame.
func sseChunk(delta string) string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"role": "assistant", "content": delta},
		}},
	}
	raw, _ := json.Marshal(payload)
	return "data: " + string(raw) + "\n\n"
}

// sseOpenAI streams the given deltas as SSE chunks, then [DONE].
func sseOpenAI(t *testing.T, deltas ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, d := range deltas {
			_, _ = w.Write([]byte(sseChunk(d)))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func TestTurnStreamsChunks(t *testing.T) {
	llm := sseOpenAI(t, "hello ", "streamed ", "world")
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-stream", "conn-1", sc, "hi")

	var deltas strings.Builder
	sawFinal := false
	for !sawFinal {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus, wstypes.EventTypeQueueState,
			wstypes.EventTypeQueueResult:
			continue
		case wstypes.EventTypeMessageChunk:
			p := eventPayload[wstypes.MessageChunkEvent](t, ev)
			require.Equal(t, itemID, p.ID)
			deltas.WriteString(p.Delta)
		case wstypes.EventTypeMessage:
			p := eventPayload[wstypes.MessageEvent](t, ev)
			require.Equal(t, itemID, p.ID)
			require.Equal(t, "hello streamed world", p.Content)
			require.Equal(t, "hello streamed world", deltas.String())
			sawFinal = true
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}
}

func TestTurnBroadcastsAnswer(t *testing.T) {
	llm := fakeOpenAI(t, "hello from the model")
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-turn", "conn-1", sc, "hi")

	ev, p := readUntilMessage(t, c)
	require.Equal(t, "agent-turn", ev.AgentID)

	require.Equal(t, itemID, p.ID)
	require.Equal(t, "hello from the model", p.Content)
}

func TestTurnResolverErrorPausesUntilExplicitSelection(t *testing.T) {
	llm := fakeOpenAI(t, "recovered")
	defer llm.Close()

	var failFirst atomic.Bool
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		if failFirst.CompareAndSwap(false, true) {
			return backend.ResolvedModel{}, errors.New("read default model: no rows")
		}
		return backend.ResolvedModel{BaseURL: llm.URL, APIKey: "sk-test", Model: "test-model"}, nil
	}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: resolver})

	c, sc := wsPair(t)

	// The first item claims on default routing; a second waits behind
	// it, so the resolver failure arms the pause with work to gate.
	item1 := submit(t, mgr, "agent-rerr", "conn-1", sc, "hi")
	item2 := submit(t, mgr, "agent-rerr", "conn-1", sc, "again")

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	p := eventPayload[wstypes.ErrorEvent](t, ev)
	require.Contains(t, p.Message, "no rows")
	// No sink is bound: the error is transient — nothing stored to
	// name, but the turn is.
	require.Empty(t, p.ID)
	require.Equal(t, item1, p.TurnID)

	// The message-level failure paused the queue with item2 gated;
	// the silence is probed on a disposable conn, because a timed-out
	// read closes a coder/websocket connection.
	snapshot := readQueueState(t, c)
	require.True(t, snapshot.Paused)
	require.Len(t, snapshot.Items, 1)
	require.Equal(t, item2, snapshot.Items[0].ID)
	_, probe := wsPair(t)
	quiet := time.Now().Add(250 * time.Millisecond)
	for {
		ev, arrived := readEventMaybe(t, probe, time.Until(quiet))
		if !arrived {
			break
		}
		if ev.Type != wstypes.EventTypeQueueState && ev.Type != wstypes.EventTypeLaneStatus {
			t.Fatalf("paused queue emitted %v", ev.Type)
		}
	}

	// The explicit selection resumes — and, paused, carries the
	// recovery rebind: item2 was captured on default routing like the
	// failed turn, so the caller's current selection replaces it
	// before the claim.
	_, err := mgr.SendNow("agent-rerr", item2,
		&apitypes.ModelEntry{ID: "recovered", ProviderID: "prov"})
	require.NoError(t, err)
	snapshot = readQueueState(t, c)
	require.NotNil(t, snapshot.Next)
	require.Equal(t, item2, snapshot.Next.ID)
	require.NotNil(t, snapshot.Next.Model, "the paused rebind replaced the gated item's selection")
	require.Equal(t, "recovered", snapshot.Next.Model.ID)
	_, p2 := readUntilMessage(t, c)
	require.Equal(t, "recovered", p2.Content)
}

func TestTurnInterruptCancelsQuery(t *testing.T) {
	var hits atomic.Int32
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			arrived <- struct{}{}
			// Hold the first query open. The abandoned request may
			// never see its context done, so the test releases it
			// itself.
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("after interrupt")))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llm.Close()
	defer close(release) // LIFO: runs before llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-int", "conn-1", sc, "hi")

	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("first query never reached the model")
	}

	mgr.Interrupt("agent-int")

	// The stopped turn winds down through status and queue traffic
	// only — an error would turn the stop into a failure.
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

	// ...and, a stop not being a failure, the queue is free to run
	// the next item.
	submit(t, mgr, "agent-int", "conn-1", sc, "again")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "after interrupt", p.Content)
}

// A canceled turn settles as a stop even when the failing seam
// returned its own error: classification goes by the turn's context,
// which has exactly one cancel source.
func TestTurnCanceledDuringResolveSettlesAsStop(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
		case <-release:
		}
		// A short-circuit failure that is not context.Canceled.
		return backend.ResolvedModel{}, errors.New("no default model set")
	}

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: resolver, PersistTurn: sink.persist})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-cancel-stop", "conn-1", sc, "hi")

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("a turn never reached the resolver")
	}

	// Stop with nothing waiting: interrupt only.
	mgr.Interrupt("agent-cancel-stop")

	for idle := false; !idle; {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeQueueState, wstypes.EventTypeQueueResult,
			wstypes.EventTypeMessageChunk, wstypes.EventTypeToolCall,
			wstypes.EventTypeToolResult:
		case wstypes.EventTypeMessage:
			t.Fatalf("a stopped turn must not settle a message: %+v", ev.Data)
		case wstypes.EventTypeLaneStatus:
			if !eventPayload[wstypes.LaneStatus](t, ev).Working {
				idle = true
			}
		case wstypes.EventTypeError:
			t.Fatalf("a canceled turn must not emit an error: %+v", ev.Data)
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}

	// Only the user row persisted — no error row.
	calls := sink.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)
}

// A fast stream coalesces: the answer costs fewer frames than it has
// chunks, and content and order are unchanged.
func TestStreamCoalescesFastChunks(t *testing.T) {
	llm := sseOpenAI(t, "a", "b", "c", "d", "e")
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-coalesce", "conn-1", sc, "hi")

	chunks := 0
	var deltas strings.Builder
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus, wstypes.EventTypeQueueState,
			wstypes.EventTypeQueueResult:
			continue
		case wstypes.EventTypeMessageChunk:
			p := eventPayload[wstypes.MessageChunkEvent](t, ev)
			require.Equal(t, itemID, p.ID)
			chunks++
			deltas.WriteString(p.Delta)
		case wstypes.EventTypeMessage:
			p := eventPayload[wstypes.MessageEvent](t, ev)
			require.Equal(t, "abcde", p.Content)
			require.Equal(t, "abcde", deltas.String())
			require.Less(t, chunks, 5, "a fast stream coalesces: fewer frames than chunks")
			return
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}
}

func TestInterruptMidStreamSettlesPartial(t *testing.T) {
	flushed := make(chan struct{})
	release := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("partial ")))
		flusher.Flush()
		_, _ = w.Write([]byte(sseChunk("answer")))
		flusher.Flush()
		close(flushed)
		// Hold the stream open: the user stops mid-answer.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer llm.Close()
	defer close(release)

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-stop", "conn-1", sc, "hi")

	// Wait until the streamed partial reached this client — a stop
	// only preserves what the user has actually seen.
	var got strings.Builder
	for got.String() != "partial answer" {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus, wstypes.EventTypeQueueState,
			wstypes.EventTypeQueueResult:
			continue
		case wstypes.EventTypeMessageChunk:
			p := eventPayload[wstypes.MessageChunkEvent](t, ev)
			require.Equal(t, itemID, p.ID)
			got.WriteString(p.Delta)
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}

	mgr.Interrupt("agent-stop")

	// A deliberate stop keeps what was said: the partial settles as
	// the final message — no error event.
	_, p := readUntilMessage(t, c)
	require.Equal(t, itemID, p.ID)
	require.Equal(t, "partial answer", p.Content)

	// The turn bracket closes and nothing else follows.
	last := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeLaneStatus, last.Type)
	ended := eventPayload[wstypes.LaneStatus](t, last)
	require.False(t, ended.Working)
	// Queue traffic may trail the bracket; further turn output may not.
	quiet := time.Now().Add(250 * time.Millisecond)
	for {
		ev, ok := readEventMaybe(t, c, time.Until(quiet))
		if !ok {
			break
		}
		if ev.Type != wstypes.EventTypeLaneStatus && ev.Type != wstypes.EventTypeQueueState &&
			ev.Type != wstypes.EventTypeQueueResult {
			t.Fatalf("unexpected event after stop: %v", ev.Type)
		}
	}

	calls := sink.recorded()
	require.Len(t, calls, 2)
	require.Equal(t,
		[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "partial answer"}},
		calls[1])
}

// A stop before any output is a quiet stop: no error, no assistant
// row — the user message stands alone until the next prompt.
func TestInterruptBeforeOutputEmitsNothing(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer llm.Close()
	defer close(release)

	target := llm.URL
	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: func(_ context.Context, _ *apitypes.ModelEntry) (backend.ResolvedModel, error) {
				return backend.ResolvedModel{BaseURL: target, APIKey: "sk-test", Model: "test-model"}, nil
			},
			PersistTurn: sink.persist,
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-stop2", "conn-1", sc, "hi")

	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("query never reached the model")
	}

	mgr.Interrupt("agent-stop2")

	// The turn winds down through status and queue traffic only; an
	// error event here would turn the stop into a failure.
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

	// The user row stands alone — no assistant draft, no error row.
	calls := sink.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)

	// The lane keeps serving: the next prompt runs and settles.
	fast := fakeOpenAI(t, "quick reply")
	defer fast.Close()
	target = fast.URL
	c2, sc2 := wsPair(t)
	submit(t, mgr, "agent-stop2", "conn-2", sc2, "again")
	_, p := readUntilMessage(t, c2)
	require.Equal(t, "quick reply", p.Content)
}

func TestTurnSurvivesSubmittingConnectionLoss(t *testing.T) {
	llm := fakeOpenAI(t, "survivor")
	defer llm.Close()

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-outlive", "conn-1", sc, "hi")

	// The submitting connection dies mid-turn: the worker owns the
	// turn's lifetime, not the connection that submitted it.
	closeConn(sc)
	closeConn(c)

	require.Eventually(t, func() bool {
		calls := sink.recorded()
		return len(calls) == 2 && calls[1][0].Content == "survivor"
	}, 5*time.Second, 10*time.Millisecond, "the turn must settle despite the submitter's loss")

	require.NotNil(t, mgr.Get("agent-outlive"))
}

// readEventMaybe reads one event, or reports false when the wait
// elapsed first — the "nothing arrived" probe.
func readEventMaybe(t *testing.T, c *websocket.Conn, wait time.Duration) (wstypes.Event, bool) {
	t.Helper()
	if wait <= 0 {
		return wstypes.Event{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	var ev wstypes.Event
	if err := wsjson.Read(ctx, c, &ev); err != nil {
		return wstypes.Event{}, false
	}
	return ev, true
}

// recordingSink captures PersistTurn calls. With a handshake armed it
// parks the assistant write so tests can pin the write-then-emit
// ordering; fail makes every write fail like a sick database.
type recordingSink struct {
	mu          sync.Mutex
	calls       [][]apitypes.ThreadMessageDraft
	fail        error
	parked      bool
	blocked     chan struct{} // closed when the assistant write starts
	release     chan struct{} // closed by unblock to let the write finish
	releaseOnce sync.Once
}

func (s *recordingSink) persist(ctx context.Context, agentID string, msgs []apitypes.ThreadMessageDraft) error {
	s.mu.Lock()
	s.calls = append(s.calls, msgs)
	fail := s.fail
	// Park only the first assistant or error draft; later turns'
	// drafts flow through.
	park := s.blocked != nil && !s.parked &&
		(msgs[0].Role == apitypes.ThreadRoleAssistant || msgs[0].Role == apitypes.ThreadRoleError)
	if park {
		s.parked = true
	}
	s.mu.Unlock()

	if park {
		close(s.blocked)
		<-s.release
	}
	return fail
}

// unblock lets a parked write finish; safe whether or not the write
// ever parked, and safe to call twice.
func (s *recordingSink) unblock() {
	if s.release == nil {
		return
	}
	s.releaseOnce.Do(func() { close(s.release) })
}

func (s *recordingSink) recorded() [][]apitypes.ThreadMessageDraft {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]apitypes.ThreadMessageDraft, len(s.calls))
	copy(out, s.calls)
	return out
}

func TestQueuedPromptsRunOnePerTurn(t *testing.T) {
	llm := fakeOpenAI(t, "same reply")
	defer llm.Close()

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	first := submit(t, mgr, "agent-persist", "conn-1", sc, "hello")
	second := submit(t, mgr, "agent-persist", "conn-1", sc, "again")

	// Two prompts, two turns: each is one model call with exactly
	// its own text — never a joined batch.
	_, p := readUntilMessage(t, c)
	require.Equal(t, first, p.ID)
	require.Equal(t, "same reply", p.Content)
	_, p = readUntilMessage(t, c)
	require.Equal(t, second, p.ID)

	calls := sink.recorded()
	require.Len(t, calls, 4, "user+assistant drafts per turn, one turn per item")
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)
	require.Equal(t, "hello", calls[0][0].Content)
	require.Equal(t, apitypes.ThreadRoleAssistant, calls[1][0].Role)
	require.Equal(t, apitypes.ThreadRoleUser, calls[2][0].Role)
	require.Equal(t, "again", calls[2][0].Content)
	require.NotEmpty(t, calls[0][0].ID, "the started user row carries the claim's id")
}

func TestTurnPersistsAssistantBeforeFinalEvent(t *testing.T) {
	llm := fakeOpenAI(t, "ordered")
	defer llm.Close()

	sink := &recordingSink{blocked: make(chan struct{}), release: make(chan struct{})}
	defer sink.unblock() // a write parked past the body drains at teardown
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-order", "conn-1", sc, "hi")

	select {
	case <-sink.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("assistant draft never reached the sink")
	}

	// A passive second connection joins the broadcaster; its only
	// possible delivery while the write is parked is queue traffic,
	// never the final message — so it doubles as the ordering probe.
	// (A short-timeout read closes a coder/websocket conn — the
	// probe conn is disposable on purpose; the submitting conn is
	// never touched.)
	_, probe := wsPair(t)
	_, err := mgr.Submit("agent-order", "conn-2", probe, "queued", nil)
	require.NoError(t, err)

	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		ev, arrived := readEventMaybe(t, probe, time.Until(deadline))
		if !arrived {
			break
		}
		require.NotEqual(t, wstypes.EventTypeMessage, ev.Type,
			"final message outran the assistant write")
	}

	sink.unblock()
	_, p := readUntilMessage(t, c)
	require.Equal(t, "ordered", p.Content)
}

func TestTurnErrorPersistsUserThenErrorRow(t *testing.T) {
	llm := fakeOpenAI(t, "never reached")
	defer llm.Close()

	sink := &recordingSink{}
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows")
	}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: resolver,
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-errrow", "conn-1", sc, "hello")

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	p := eventPayload[wstypes.ErrorEvent](t, ev)
	require.Equal(t, itemID, p.TurnID)

	// The failure lands as an error row after the user row, and the
	// event names that row — live and reloaded state converge on it.
	calls := sink.recorded()
	require.Len(t, calls, 2)
	require.Equal(t, apitypes.ThreadRoleUser, calls[0][0].Role)
	require.Equal(t, "hello", calls[0][0].Content)
	require.Len(t, calls[1], 1)
	require.Equal(t, apitypes.ThreadRoleError, calls[1][0].Role)
	require.Equal(t, "read provider p-1: no rows", calls[1][0].Content)
	require.NotEmpty(t, p.ID)
	require.Equal(t, p.ID, calls[1][0].ID)
}

func TestTurnErrorPersistsBeforeEvent(t *testing.T) {
	sink := &recordingSink{blocked: make(chan struct{}), release: make(chan struct{})}
	defer sink.unblock()
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows")
	}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: resolver,
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-errorder", "conn-1", sc, "hello")

	select {
	case <-sink.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("error draft never reached the sink")
	}
	// While the error write is parked, the failure must not be on the
	// wire — a client refetching on the event would miss the row.
	if ev, arrived := readEventMaybe(t, c, 150*time.Millisecond); arrived {
		require.NotEqual(t, wstypes.EventTypeError, ev.Type,
			"error event outran the error write")
	}

	sink.unblock()
	p := eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
	require.NotEmpty(t, p.ID)
	calls := sink.recorded()
	require.Equal(t, p.ID, calls[1][0].ID)
}

func TestTurnErrorSinkFailureStaysTransient(t *testing.T) {
	sink := &recordingSink{fail: errors.New("db down")}
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows")
	}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: resolver,
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-errsink", "conn-1", sc, "hello")

	// The row never confirmed: the event keeps the original failure,
	// claims no stored id, and the write was still attempted.
	p := eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
	require.Equal(t, "read provider p-1: no rows", p.Message)
	require.Empty(t, p.ID)
	require.Equal(t, itemID, p.TurnID)
	calls := sink.recorded()
	require.Len(t, calls, 2)
	require.Equal(t, apitypes.ThreadRoleError, calls[1][0].Role)
}

func TestTurnStreamErrorPersistsErrorRow(t *testing.T) {
	// A model that answers 500 fails the query itself — a later
	// failure exit than the resolver.
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer llm.Close()

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-errstream", "conn-1", sc, "hello")

	p := eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
	require.NotEmpty(t, p.ID)
	require.NotEmpty(t, p.Message)
	calls := sink.recorded()
	require.Len(t, calls, 2)
	require.Equal(t, apitypes.ThreadRoleError, calls[1][0].Role)
	require.Equal(t, p.Message, calls[1][0].Content)
	require.Equal(t, p.ID, calls[1][0].ID)
}

func TestTurnSinkFailureDoesNotFailTurn(t *testing.T) {
	llm := fakeOpenAI(t, "still answers")
	defer llm.Close()

	sink := &recordingSink{fail: errors.New("db down")}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-sinkfail", "conn-1", sc, "hi")

	_, p := readUntilMessage(t, c)
	require.Equal(t, "still answers", p.Content)

	// A sick sink stays a server-side log line, never a wire error.
	// The turn's closing lane_status and queue traffic may still
	// land; only an error fails the pin.
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		ev, arrived := readEventMaybe(t, c, time.Until(deadline))
		if !arrived {
			break
		}
		require.NotEqual(t, wstypes.EventTypeError, ev.Type,
			"sink failure must not emit a wire error")
	}
}
