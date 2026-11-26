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

// settledTurn is a completed turn's committed outcome in the shape these
// tests assert on: the item id and its terminal answer text.
type settledTurn struct {
	ID      string
	Content string
	Event   apitypes.LifecycleEvent
}

// terminalText is the answer text a terminal event committed: its last
// assistant row.
func terminalText(rows []apitypes.ThreadMessage) string {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Role == apitypes.ThreadRoleAssistant {
			return rows[i].Content
		}
	}
	return ""
}

// readUntilMessage drains mid-turn frames until the turn's terminal
// lifecycle event and reports the settled answer.
func readUntilMessage(t *testing.T, c *websocket.Conn) (wstypes.Event, settledTurn) {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		for _, lifecycle := range lifecycleBatch(t, ev) {
			if lifecycle.Kind != apitypes.LifecycleItemFinished || lifecycle.Outcome != apitypes.ItemCompleted {
				continue
			}
			return ev, settledTurn{
				ID:      lifecycle.ItemID,
				Content: terminalText(lifecycle.Rows),
				Event:   lifecycle,
			}
		}
	}
}

// readUntilError drains frames until a turn failure is reported: the
// transient error frame (tool-attributed failures, unpersisted
// outcomes), or a failed item's terminal event, whose error row
// carries the message.
func readUntilError(t *testing.T, c *websocket.Conn) wstypes.Event {
	t.Helper()
	for {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeError:
			return ev
		case wstypes.EventTypeLifecycle:
			for _, lifecycle := range lifecycleBatch(t, ev) {
				if lifecycle.Kind != apitypes.LifecycleItemFinished {
					continue
				}
				if lifecycle.Outcome != apitypes.ItemFailed || len(lifecycle.Rows) == 0 {
					// A stop is not a failure; a tool-attributed
					// failure carries its own row and keeps the
					// transient bubble, which follows.
					continue
				}
				return wstypes.Event{
					Type:    wstypes.EventTypeError,
					AgentID: ev.AgentID,
					Data: wstypes.ErrorEvent{
						TurnID:  lifecycle.ItemID,
						Message: lifecycle.Rows[len(lifecycle.Rows)-1].Content,
					},
				}
			}
		}
	}
}

// lifecycleBatch decodes every event carried by one lifecycle frame.
func lifecycleBatch(t *testing.T, ev wstypes.Event) []apitypes.LifecycleEvent {
	t.Helper()
	batch := eventPayload[wstypes.LifecycleBatchEvent](t, ev)
	out := make([]apitypes.LifecycleEvent, 0, len(batch.Events))
	for _, raw := range batch.Events {
		var lifecycle apitypes.LifecycleEvent
		require.NoError(t, json.Unmarshal(raw, &lifecycle))
		out = append(out, lifecycle)
	}
	return out
}

// readUntilSettled drains mid-turn frames until the turn's terminal
// lifecycle event — any outcome, a stop included — and reports the
// settled turn.
func readUntilSettled(t *testing.T, c *websocket.Conn) (wstypes.Event, settledTurn) {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		for _, lifecycle := range lifecycleBatch(t, ev) {
			if lifecycle.Kind != apitypes.LifecycleItemFinished {
				continue
			}
			return ev, settledTurn{
				ID:      lifecycle.ItemID,
				Content: terminalText(lifecycle.Rows),
				Event:   lifecycle,
			}
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

// submit accommodates one prompt on a subscribed connection and returns the
// admitted item's stable id — the turn's stream id on the wire.
func submit(t *testing.T, mgr *agent.UserAgentManager, agentID, connID string, conn *websocket.Conn, content string) string {
	t.Helper()
	subscribe(mgr, agentID, connID, conn)
	id, err := mgr.Submit(agentID, content, nil, "")
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

	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

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
			require.Positive(t, p.Seq, "chunks carry the stream's own sequence")
			deltas.WriteString(p.Delta)
		case wstypes.EventTypeLifecycle:
			for _, lifecycle := range lifecycleBatch(t, ev) {
				if lifecycle.Kind != apitypes.LifecycleItemFinished ||
					lifecycle.Outcome != apitypes.ItemCompleted {
					continue
				}
				require.Equal(t, itemID, lifecycle.ItemID)
				require.Equal(t, "hello streamed world", terminalText(lifecycle.Rows))
				require.Equal(t, "hello streamed world", deltas.String())
				sawFinal = true
			}
		default:
			t.Fatalf("unexpected event %v", ev.Type)
		}
	}
}

func TestTurnBroadcastsAnswer(t *testing.T) {
	llm := fakeOpenAI(t, "hello from the model")
	defer llm.Close()

	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

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

	var resolveCalls atomic.Int32
	// Every resolver call reports the selection it ran on: the routing
	// choice is not projected on the wire, so the rebind is proven where
	// it takes effect. The first two calls fail — the pre-prompt
	// checker's measurement first, then the turn's own resolve — so
	// the failure the pause gate records is the turn's, not the
	// checker's degraded one.
	resolved := make(chan string, 8)
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		selection := ""
		if entry != nil {
			selection = entry.ID
		}
		resolved <- selection
		if resolveCalls.Add(1) <= 2 {
			return backend.ResolvedModel{}, errors.New("read default model: no rows")
		}
		return backend.ResolvedModel{BaseURL: llm.URL, APIKey: "sk-test", Model: "test-model"}, nil
	}
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: resolver})

	c, sc := wsPair(t)

	// The first item claims on default routing; a second waits behind
	// it, so the resolver failure arms the pause with work to gate.
	item1 := submit(t, mgr, "agent-rerr", "conn-1", sc, "hi")
	item2 := submit(t, mgr, "agent-rerr", "conn-1", sc, "again")

	// The failure is durable: it settles the item as failed with the
	// cause as its error row.
	terminal := readTerminal(t, c, item1)
	require.Equal(t, apitypes.ItemFailed, terminal.Outcome)
	require.Len(t, terminal.Rows, 1)
	require.Contains(t, terminal.Rows[0].Content, "no rows")

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
	_, p2 := readUntilMessage(t, c)
	require.Equal(t, "recovered", p2.Content)
	require.Equal(t, "", <-resolved, "the checker measured the first item on default routing")
	require.Equal(t, "", <-resolved, "the failed turn ran on default routing")
	require.Equal(t, "recovered", <-resolved, "the paused rebind replaced the gated item's selection")
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

	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

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

	// ...and, a stop not being a failure, the queue is free to run
	// the next item.
	submit(t, mgr, "agent-int", "conn-1", sc, "again")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "after interrupt", p.Content)
}

// The durable copies of the model's own text scrub at persistence:
// the settled terminal row and an interrupted turn's partial carry the
// static marker, while the live chunk stream stays the model's text.
func TestAssistantTextScrubbedAtPersistence(t *testing.T) {
	secret := "sk_live_QX9SLYN1SZV7wxyz0"
	flushes := atomic.Int32{}
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("the key is " + secret)))
		flusher.Flush()
		if flushes.Add(1) > 1 {
			// Second turn: hold the stream open so the interrupt
			// lands mid-answer with a partial on record.
			arrived <- struct{}{}
			<-release
			return
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llm.Close()
	defer close(release) // LIFO: runs before llm.Close()

	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-scrub", "conn-1", sc, "hi")
	_, p := readUntilMessage(t, c)
	require.Equal(t, "the key is [REDACTED:api_key]", p.Content, "the terminal frame rides the scrubbed durable copy")

	submit(t, mgr, "agent-scrub", "conn-1", sc, "again")
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("second query never reached the model")
	}
	// The chunk on the wire proves the answer already holds text, so
	// the stop lands mid-answer instead of racing an empty partial.
	for chunked := false; !chunked; {
		chunked = readEvent(t, c).Type == wstypes.EventTypeMessageChunk
	}
	mgr.Interrupt("agent-scrub")

	// The stopped turn's terminal batch carries the partial row: wait
	// for it instead of an idle status — the lane's trailing frames
	// from turn one would satisfy an idle check before turn two
	// settles.
	_, stopped := readUntilSettled(t, c)
	require.Equal(t, apitypes.ItemStopped, stopped.Event.Outcome)
	require.NotContains(t, stopped.Content, secret, "the broadcast partial leaked the secret")
	require.Contains(t, stopped.Content, "[REDACTED:api_key]", "the stopped turn's partial")

	rows := storedRows(t, sqlDB, "agent-scrub")
	for _, row := range rows {
		if row.Role != apitypes.ThreadRoleAssistant {
			continue
		}
		require.NotContains(t, row.Content, secret, "durable assistant copy leaked the secret")
		require.Contains(t, row.Content, "[REDACTED:api_key]", describe(rows))
	}
}

// A resolved scrubber owns the observable copies even when the toolset
// is empty: an empty Impls list is still a resolved seam, and its
// scrubber — here a zero scrubber that redacts nothing — must ride the
// turn instead of the nil fallback's pattern battery.
func TestEmptyToolsetKeepsResolvedScrubber(t *testing.T) {
	secret := "sk_live_QX9SLYN1SZV7wxyz0"
	llm := newScriptedLLM(t, sseChunk("the key is "+secret))
	mgr, _ := journaledLane(t, &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(llm.URL),
		GetAgentThread: emptyThread,
		Tools: func(context.Context, string) (backend.TurnTools, error) {
			// Zero tools, but the scrubber is resolved: the seam
			// owns scrub policy for this turn.
			return backend.TurnTools{Scrubber: new(backend.Scrubber)}, nil
		},
	})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-scrub-seam", "conn-1", sc, "hi")
	_, p := readUntilMessage(t, c)
	require.Contains(t, p.Content, secret,
		"the resolved scrubber owns the durable copy; the pattern battery must not re-apply")
}

// A prompt turn never consumed a context window: a resolution that
// answers zero — nothing declared, discovered, or cataloged — must not
// fail the turn. Only window-aware paths (the compact lane) name the
// failure.
func TestPromptTurnRunsOnUnresolvedWindow(t *testing.T) {
	llm := newScriptedLLM(t, sseChunk("still answering"))
	mgr, _ := journaledLane(t, &agent.UserAgentManagerOptions{
		ResolveModel: func(_ context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
			model := "test-model"
			if entry != nil {
				model = entry.ID
			}
			return backend.ResolvedModel{
				ProviderID: "prov-1", BaseURL: llm.URL, APIKey: "sk-test",
				Model: model, ContextWindowTokens: 0,
			}, nil
		},
		GetAgentThread: emptyThread,
	})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-zerowin", "conn-1", sc, "hi")
	_, p := readUntilMessage(t, c)
	require.Contains(t, p.Content, "still answering",
		"a zero-window resolution must not fail a prompt turn")
}

// A canceled turn settles as a stop even when the failing seam
// returned its own error: classification goes by the turn's context,
// which has exactly one cancel source.
func TestTurnCanceledDuringResolveSettlesAsStop(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		// The checker resolves first and must degrade quickly; the
		// parked call below is the turn's own — the stop-during-
		// resolve scenario under test.
		if calls.Add(1) == 1 {
			return backend.ResolvedModel{}, errors.New("no default model set")
		}
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

	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: resolver})

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
			wstypes.EventTypeMessageChunk:
		case wstypes.EventTypeLifecycle:
			for _, lifecycle := range lifecycleBatch(t, ev) {
				if lifecycle.Kind != apitypes.LifecycleItemFinished {
					continue
				}
				require.Equal(t, apitypes.ItemStopped, lifecycle.Outcome,
					"a stopped turn must not settle a message")
				require.Empty(t, lifecycle.Rows, "a quiet stop commits no rows")
			}
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

	// Only the user row persisted — no error row, no assistant row.
	rows := storedRows(t, sqlDB, "agent-cancel-stop")
	require.Len(t, rows, 1, describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Equal(t, "hi", rows[0].Content)
}

// A fast stream coalesces: the answer costs fewer frames than it has
// chunks, and content and order are unchanged.
func TestStreamCoalescesFastChunks(t *testing.T) {
	llm := sseOpenAI(t, "a", "b", "c", "d", "e")
	defer llm.Close()

	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread, ResolveModel: fixedResolver(llm.URL)})

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
		case wstypes.EventTypeLifecycle:
			for _, lifecycle := range lifecycleBatch(t, ev) {
				if lifecycle.Kind != apitypes.LifecycleItemFinished ||
					lifecycle.Outcome != apitypes.ItemCompleted {
					continue
				}
				require.Equal(t, "abcde", terminalText(lifecycle.Rows))
				require.Equal(t, "abcde", deltas.String())
				require.Less(t, chunks, 5, "a fast stream coalesces: fewer frames than chunks")
				return
			}
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

	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
		})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-stop", "conn-1", sc, "hi")

	// Wait until the streamed partial reached this client — a stop
	// only preserves what the user has actually seen. Durable facts
	// (the start) may interleave; they are not stream output.
	var got strings.Builder
	for got.String() != "partial answer" {
		ev := readEvent(t, c)
		switch ev.Type {
		case wstypes.EventTypeLaneStatus, wstypes.EventTypeQueueState,
			wstypes.EventTypeQueueResult, wstypes.EventTypeLifecycle:
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
	// the stopped item's final row — no error event.
	_, p := readUntilSettled(t, c)
	require.Equal(t, itemID, p.ID)
	require.Equal(t, apitypes.ItemStopped, p.Event.Outcome)
	require.Equal(t, "partial answer", p.Content)

	// The turn bracket closes and nothing else follows.
	for ended := (wstypes.LaneStatus{Working: true}); ended.Working; {
		last := readEvent(t, c)
		if last.Type != wstypes.EventTypeLaneStatus {
			continue
		}
		ended = eventPayload[wstypes.LaneStatus](t, last)
		require.False(t, ended.Reasoning, "the bracket lowers reasoning with the turn")
	}
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

	rows := storedRows(t, sqlDB, "agent-stop")
	require.Len(t, rows, 2, describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Equal(t, apitypes.ThreadRoleAssistant, rows[1].Role)
	require.Equal(t, "partial answer", rows[1].Content)
	require.Equal(t, itemID, rows[1].TurnID)
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
	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel: func(_ context.Context, _ *apitypes.ModelEntry) (backend.ResolvedModel, error) {
				return backend.ResolvedModel{BaseURL: target, APIKey: "sk-test", Model: "test-model"}, nil
			},
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

	// The user row stands alone — no assistant draft, no error row.
	rows := storedRows(t, sqlDB, "agent-stop2")
	require.Len(t, rows, 1, describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)

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

	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
		})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-outlive", "conn-1", sc, "hi")

	// The submitting connection dies mid-turn: the worker owns the
	// turn's lifetime, not the connection that submitted it.
	closeConn(sc)
	closeConn(c)

	require.Eventually(t, func() bool {
		rows := storedRows(t, sqlDB, "agent-outlive")
		return len(rows) == 2 && rows[1].Content == "survivor"
	}, 5*time.Second, 10*time.Millisecond, "the turn must settle despite the submitter's loss")

	require.Eventually(t, func() bool {
		return agent.Worker(mgr, "agent-outlive") == nil
	}, 2*time.Second, 10*time.Millisecond,
		"the settled, connection-less queue releases its worker cleanly")
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

// parkedFinish parks the journal's first FinishItem call — the answer
// row's write — until released, then commits it against the wrapped
// journal: the mid-write window the persist-before-emit probes read
// through. Durable assertions read the store, so nothing is recorded.
type parkedFinish struct {
	agent.LifecycleJournal
	blocked     chan struct{} // closed when the parked write starts
	release     chan struct{}
	parkOnce    sync.Once
	releaseOnce sync.Once
}

func (p *parkedFinish) FinishItem(ctx context.Context, agentID, itemID, outcome, reason string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error) {
	p.parkOnce.Do(func() {
		close(p.blocked)
		<-p.release
	})
	return p.LifecycleJournal.FinishItem(ctx, agentID, itemID, outcome, reason, msgs)
}

// unblock lets a parked write finish; safe whether or not the write
// ever parked, and safe to call twice.
func (p *parkedFinish) unblock() {
	p.releaseOnce.Do(func() { close(p.release) })
}

// failingStart makes the journal's StartItem fail like a sick database
// — the required user-row write, before any model work.
type failingStart struct {
	agent.LifecycleJournal
	err error
}

func (f *failingStart) StartItem(ctx context.Context, agentID, itemID string) (*backend.ItemStart, *backend.JournalBatch, error) {
	return nil, nil, f.err
}

// errJournalWrite names the journal-write failure in tests.
var errJournalWrite = errors.New("journal write unavailable")

func TestQueuedPromptsRunOnePerTurn(t *testing.T) {
	llm := fakeOpenAI(t, "same reply")
	defer llm.Close()

	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
		})

	c, sc := wsPair(t)

	first := submit(t, mgr, "agent-persist", "conn-1", sc, "hello")
	second := submit(t, mgr, "agent-persist", "conn-1", sc, "again")

	// Two prompts, two turns: each is one model call with exactly
	// its own text — never a joined batch. Each prompt's user row
	// rides its own item_started, under the id the backend minted.
	started1 := readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleItemStarted && ev.ItemID == first
	})
	_, p := readUntilMessage(t, c)
	require.Equal(t, first, p.ID)
	require.Equal(t, "same reply", p.Content)
	started2 := readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleItemStarted && ev.ItemID == second
	})
	_, p = readUntilMessage(t, c)
	require.Equal(t, second, p.ID)

	rows := storedRows(t, sqlDB, "agent-persist")
	require.Len(t, rows, 4, "user+assistant rows per turn, one turn per item: "+describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Equal(t, "hello", rows[0].Content)
	require.Equal(t, apitypes.ThreadRoleAssistant, rows[1].Role)
	require.Equal(t, "same reply", rows[1].Content)
	require.Equal(t, apitypes.ThreadRoleUser, rows[2].Role)
	require.Equal(t, "again", rows[2].Content)
	require.Equal(t, apitypes.ThreadRoleAssistant, rows[3].Role)
	require.Equal(t, first, rows[0].TurnID)
	require.Equal(t, second, rows[2].TurnID)
	require.NotEmpty(t, rows[0].ID)
	require.Equal(t, rows[0].ID, started1.UserMessageID, "the started user row rides item_started")
	require.Equal(t, rows[2].ID, started2.UserMessageID)
}

func TestTurnPersistsAssistantBeforeFinalEvent(t *testing.T) {
	llm := fakeOpenAI(t, "ordered")
	defer llm.Close()

	// Park the answer row's journal write: the terminal event and the
	// row are one transaction, so while the write is parked neither
	// can exist.
	sqlDB, turns := journalFor(t)
	journal := &lazyJournal{TurnService: turns, sqlDB: sqlDB, seen: make(map[string]struct{})}
	parked := &parkedFinish{
		LifecycleJournal: journal,
		blocked:          make(chan struct{}),
		release:          make(chan struct{}),
	}
	defer parked.unblock() // a write parked past the body drains at teardown
	mgr := hookedManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
		}, &journalHook{LifecycleJournal: parked})

	c, sc := wsPair(t)

	submit(t, mgr, "agent-order", "conn-1", sc, "hi")

	select {
	case <-parked.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the answer write never reached the journal")
	}

	// A passive second connection joins the broadcaster; its only
	// possible delivery while the write is parked is runtime traffic,
	// never the terminal event — so it doubles as the ordering probe.
	// It submits nothing: the assertion must not race a second item's
	// own rows. (A short-timeout read closes a coder/websocket conn —
	// the probe conn is disposable on purpose; the submitting conn is
	// never touched.)
	_, probe := wsPair(t)
	subscribe(mgr, "agent-order", "conn-2", probe)

	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		ev, arrived := readEventMaybe(t, probe, time.Until(deadline))
		if !arrived {
			break
		}
		switch ev.Type {
		case wstypes.EventTypeLifecycle:
			for _, lifecycle := range lifecycleBatch(t, ev) {
				require.NotEqual(t, apitypes.LifecycleItemFinished, lifecycle.Kind,
					"the terminal event outran the answer write")
			}
		case wstypes.EventTypeError:
			t.Fatalf("an error event outran the answer write: %+v", ev.Data)
		}
	}

	parked.unblock()
	_, p := readUntilMessage(t, c)
	require.Equal(t, "ordered", p.Content)
	rows := storedRows(t, sqlDB, "agent-order")
	require.Len(t, rows, 2, describe(rows))
	require.Equal(t, apitypes.ThreadRoleAssistant, rows[1].Role)
	require.Equal(t, "ordered", rows[1].Content)
}

func TestTurnErrorPersistsUserThenErrorRow(t *testing.T) {
	llm := fakeOpenAI(t, "never reached")
	defer llm.Close()

	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows")
	}
	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   resolver,
		})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-errrow", "conn-1", sc, "hello")

	terminal := readTerminal(t, c, itemID)
	require.Equal(t, apitypes.ItemFailed, terminal.Outcome)
	require.Len(t, terminal.Rows, 1)

	// The failure lands as an error row after the user row, and the
	// terminal event carries that row — live and reloaded state
	// converge on it.
	rows := storedRows(t, sqlDB, "agent-errrow")
	require.Len(t, rows, 2, describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Equal(t, "hello", rows[0].Content)
	require.Equal(t, apitypes.ThreadRoleError, rows[1].Role)
	require.Equal(t, itemID, rows[1].TurnID, "the error row names its turn like every other row")
	require.Equal(t, "read provider p-1: no rows", rows[1].Content)
	require.NotEmpty(t, terminal.Rows[0].ID)
	require.Equal(t, terminal.Rows[0].ID, rows[1].ID, "the terminal event names the stored error row")
}

func TestTurnErrorPersistsBeforeEvent(t *testing.T) {
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows")
	}
	// Park the failed outcome's journal write — the error row and its
	// terminal event are one transaction.
	sqlDB, turns := journalFor(t)
	journal := &lazyJournal{TurnService: turns, sqlDB: sqlDB, seen: make(map[string]struct{})}
	parked := &parkedFinish{
		LifecycleJournal: journal,
		blocked:          make(chan struct{}),
		release:          make(chan struct{}),
	}
	defer parked.unblock()
	mgr := hookedManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   resolver,
		}, &journalHook{LifecycleJournal: parked})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-errorder", "conn-1", sc, "hello")

	select {
	case <-parked.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the failed outcome never reached the journal")
	}
	// While the outcome write is parked, the failure must not be on
	// the wire — a client acting on the event would miss the row. The
	// probe is a disposable subscribed connection: a timed-out read
	// closes a coder/websocket conn, and the submitting conn still
	// has to deliver the settlement.
	_, probe := wsPair(t)
	subscribe(mgr, "agent-errorder", "conn-2", probe)
	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		ev, arrived := readEventMaybe(t, probe, time.Until(deadline))
		if !arrived {
			break
		}
		switch ev.Type {
		case wstypes.EventTypeLifecycle:
			for _, lifecycle := range lifecycleBatch(t, ev) {
				require.NotEqual(t, apitypes.LifecycleItemFinished, lifecycle.Kind,
					"the terminal event outran the error write")
			}
		case wstypes.EventTypeError:
			t.Fatalf("an error event outran the error write: %+v", ev.Data)
		}
	}

	parked.unblock()
	terminal := readTerminal(t, c, itemID)
	require.Equal(t, apitypes.ItemFailed, terminal.Outcome)
	rows := storedRows(t, sqlDB, "agent-errorder")
	require.Len(t, rows, 2, describe(rows))
	require.Equal(t, apitypes.ThreadRoleError, rows[1].Role)
	require.Equal(t, terminal.Rows[0].ID, rows[1].ID, "the terminal event names the stored error row")
}

func TestTurnErrorSinkFailureStaysTransient(t *testing.T) {
	resolver := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows")
	}
	// The outcome write fails like a sick database: nothing durable
	// can settle the item, so the failure must stay transient.
	sqlDB, turns := journalFor(t)
	journal := &lazyJournal{TurnService: turns, sqlDB: sqlDB, seen: make(map[string]struct{})}
	hook := &journalHook{LifecycleJournal: journal}
	hook.finishItem = func(ctx context.Context, agentID, itemID, outcome, reason string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error) {
		return nil, errJournalWrite
	}
	mgr := hookedManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   resolver,
		}, hook)

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-errsink", "conn-1", sc, "hello")

	// The durable user row landed; the outcome's failed write leaves
	// the failure transient: the event keeps the original cause.
	p := eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
	require.Equal(t, "read provider p-1: no rows", p.Message)
	require.Equal(t, itemID, p.TurnID)

	// Nothing durable claims the failure: the user row stands alone
	// and the item stays unresolved for recovery to settle.
	rows := storedRows(t, sqlDB, "agent-errsink")
	require.Len(t, rows, 1, describe(rows))
	require.Equal(t, apitypes.ThreadRoleUser, rows[0].Role)
	require.Equal(t, "started", itemStates(t, sqlDB, "agent-errsink")[itemID])
}

func TestTurnStreamErrorPersistsErrorRow(t *testing.T) {
	// A model that answers 500 fails the query itself — a later
	// failure exit than the resolver.
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer llm.Close()

	mgr, sqlDB := journaledLane(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
		})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-errstream", "conn-1", sc, "hello")

	terminal := readTerminal(t, c, itemID)
	require.Equal(t, apitypes.ItemFailed, terminal.Outcome)
	rows := storedRows(t, sqlDB, "agent-errstream")
	require.Len(t, rows, 2, describe(rows))
	require.Equal(t, apitypes.ThreadRoleError, rows[1].Role)
	require.Equal(t, terminal.Rows[0].Content, rows[1].Content)
	require.Equal(t, terminal.Rows[0].ID, rows[1].ID, "the terminal event names the stored error row")
	require.NotEmpty(t, rows[1].Content)
}

func TestUserRowSinkFailureFailsTurnBeforeModel(t *testing.T) {
	llm := newScriptedLLM(t, sseChunk("never reached"))

	// The user-row write fails like a sick database: a prompt that
	// cannot become durable never reaches the model.
	sqlDB, turns := journalFor(t)
	journal := &lazyJournal{TurnService: turns, sqlDB: sqlDB, seen: make(map[string]struct{})}
	mgr := hookedManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel:   fixedResolver(llm.URL),
		}, &journalHook{LifecycleJournal: &failingStart{
			LifecycleJournal: journal, err: errJournalWrite,
		}})

	c, sc := wsPair(t)

	itemID := submit(t, mgr, "agent-sinkfail", "conn-1", sc, "hi")

	// The turn fails at message level naming the write — transiently:
	// the item never started, so no durable outcome exists to carry
	// the failure — and the model is never called.
	p := eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
	require.Contains(t, p.Message, "persist item start")
	require.Contains(t, p.Message, errJournalWrite.Error())
	require.Empty(t, llm.requests(), "the model must not be called")

	// Nothing durable: the user row rolled back with the failed start.
	rows := storedRows(t, sqlDB, "agent-sinkfail")
	require.Empty(t, rows, describe(rows))
	require.Equal(t, "accepted", itemStates(t, sqlDB, "agent-sinkfail")[itemID],
		"the never-started item stays accepted for restart recovery")
}
