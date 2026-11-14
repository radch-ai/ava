package agent_test

// Turn pins: a submitted prompt runs one LLM turn end to end —
// resolve, query, answer broadcast — and failures surface as error
// events without killing the lane. Tests use a fake resolver and an
// OpenAI-compatible test server; no real provider is touched.

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

// readUntilMessage drains chunk and lane_status events until the
// final message event; it returns the envelope and its payload.
func readUntilMessage(t *testing.T, c *websocket.Conn) (wstypes.Event, wstypes.MessageEvent) {
	t.Helper()
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeMessageChunk, wstypes.EventTypeLaneStatus:
			continue
		case wstypes.EventTypeMessage:
			return ev, eventPayload[wstypes.MessageEvent](t, ev)
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}
}

// readUntilError drains the turn's lane_status bracket until the
// error event; it returns the envelope.
func readUntilError(t *testing.T, c *websocket.Conn) wstypes.Event {
	t.Helper()
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus:
			continue
		case wstypes.EventTypeError:
			return ev
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}
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

	ok := mgr.SubmitMsg(context.Background(), "agent-stream", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	var deltas strings.Builder
	sawFinal := false
	for !sawFinal {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus:
			continue
		case wstypes.EventTypeMessageChunk:
			p := eventPayload[wstypes.MessageChunkEvent](t, ev)
			require.Equal(t, "msg-1", p.ID)
			deltas.WriteString(p.Delta)
		case wstypes.EventTypeMessage:
			p := eventPayload[wstypes.MessageEvent](t, ev)
			require.Equal(t, "msg-1", p.ID)
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

	ok := mgr.SubmitMsg(context.Background(), "agent-turn", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	ev, p := readUntilMessage(t, c)
	require.Equal(t, "agent-turn", ev.AgentID)

	require.Equal(t, "msg-1", p.ID)
	require.Equal(t, "hello from the model", p.Content)
}

func TestTurnResolverErrorEmitsErrorAndKeepsLane(t *testing.T) {
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

	ok := mgr.SubmitMsg(context.Background(), "agent-rerr", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	p := eventPayload[wstypes.ErrorEvent](t, ev)
	require.Contains(t, p.Message, "no rows")

	// The lane survives the failed turn and runs the next one.
	ok = mgr.SubmitMsg(context.Background(), "agent-rerr", "conn-1", sc,
		agent.Message{ID: "msg-2", Content: "again"})
	require.True(t, ok)
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

	ok := mgr.SubmitMsg(context.Background(), "agent-int", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("first query never reached the model")
	}

	ag := mgr.Get("agent-int")
	require.NotNil(t, ag)
	ag.Interrupt()

	// The interrupted turn reports its cancellation...
	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)

	// ...and the lane is free to run the next turn.
	ok = mgr.SubmitMsg(context.Background(), "agent-int", "conn-1", sc,
		agent.Message{ID: "msg-2", Content: "again"})
	require.True(t, ok)
	_, p := readUntilMessage(t, c)
	require.Equal(t, "after interrupt", p.Content)
}

func TestLaneOutlivesSubmittingContext(t *testing.T) {
	llm := fakeOpenAI(t, "survivor")
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	ok := mgr.SubmitMsg(ctx, "agent-outlive", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)
	cancel()

	// The turn still runs: the lane owns its lifetime, not the
	// submitting connection.
	_, _ = readUntilMessage(t, c)

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
	// Park only the first assistant draft; later turns' drafts flow
	// through.
	park := s.blocked != nil && msgs[0].Role == apitypes.ThreadRoleAssistant && !s.parked
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

func TestTurnPersistsUserThenAssistantDrafts(t *testing.T) {
	llm := fakeOpenAI(t, "persisted answer")
	defer llm.Close()

	sink := &recordingSink{}
	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{
			ResolveModel: fixedResolver(llm.URL),
			PersistTurn:  sink.persist,
		})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-persist", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hello"})
	require.True(t, ok)
	// The second prompt queues behind the first and drains as one
	// batch: one user row, exactly what the model saw.
	ok = mgr.SubmitMsg(context.Background(), "agent-persist", "conn-1", sc,
		agent.Message{ID: "msg-2", Content: "again"})
	require.True(t, ok)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "msg-1", p.ID)
	require.Equal(t, "persisted answer", p.Content)

	calls := sink.recorded()
	require.Len(t, calls, 2)
	require.Equal(t,
		[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "hello\nagain"}},
		calls[0])
	require.Equal(t,
		[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "persisted answer"}},
		calls[1])
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

	ok := mgr.SubmitMsg(context.Background(), "agent-order", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	select {
	case <-sink.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("assistant draft never reached the sink")
	}

	// A passive second connection joins the broadcaster; its only
	// possible delivery while the write is parked is the final
	// message, so it doubles as the ordering probe. (A short-timeout
	// read closes a coder/websocket conn — the probe conn is
	// disposable on purpose; the submitting conn is never touched.)
	_, probe := wsPair(t)
	ok = mgr.SubmitMsg(context.Background(), "agent-order", "conn-2", probe,
		agent.Message{ID: "msg-2", Content: "queued"})
	require.True(t, ok)

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

func TestTurnErrorPersistsUserOnly(t *testing.T) {
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

	ok := mgr.SubmitMsg(context.Background(), "agent-errrow", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hello"})
	require.True(t, ok)

	ev := readUntilError(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)

	calls := sink.recorded()
	require.Len(t, calls, 1)
	require.Equal(t,
		[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "hello"}},
		calls[0])
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

	ok := mgr.SubmitMsg(context.Background(), "agent-sinkfail", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	_, p := readUntilMessage(t, c)
	require.Equal(t, "still answers", p.Content)

	// A sick sink stays a server-side log line, never a wire error.
	// The turn's closing lane_status may still land; only an error
	// fails the pin.
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
