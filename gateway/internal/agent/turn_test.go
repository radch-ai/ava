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

// fakeOpenAI answers every request with one completed chat
// completion carrying the given reply.
func fakeOpenAI(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 0,
			"model":   "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": reply},
				"finish_reason": "stop",
			}},
		})
	}))
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

func TestTurnBroadcastsAnswer(t *testing.T) {
	llm := fakeOpenAI(t, "hello from the model")
	defer llm.Close()

	mgr := agent.NewUserAgentManager(zerolog.Nop(),
		&agent.UserAgentManagerOptions{ResolveModel: fixedResolver(llm.URL)})

	c, sc := wsPair(t)

	ok := mgr.SubmitMsg(context.Background(), "agent-turn", "conn-1", sc,
		agent.Message{ID: "msg-1", Content: "hi"})
	require.True(t, ok)

	ev := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeMessage, ev.Type)
	require.Equal(t, "agent-turn", ev.AgentID)
	p := eventPayload[wstypes.MessageEvent](t, ev)
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

	ev := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)
	p := eventPayload[wstypes.ErrorEvent](t, ev)
	require.Contains(t, p.Message, "no rows")

	// The lane survives the failed turn and runs the next one.
	ok = mgr.SubmitMsg(context.Background(), "agent-rerr", "conn-1", sc,
		agent.Message{ID: "msg-2", Content: "again"})
	require.True(t, ok)
	ev = readEvent(t, c)
	require.Equal(t, wstypes.EventTypeMessage, ev.Type)
	p2 := eventPayload[wstypes.MessageEvent](t, ev)
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "created": 0,
			"model": "test-model",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": "after interrupt"},
			}},
		})
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
	ev := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeError, ev.Type)

	// ...and the lane is free to run the next turn.
	ok = mgr.SubmitMsg(context.Background(), "agent-int", "conn-1", sc,
		agent.Message{ID: "msg-2", Content: "again"})
	require.True(t, ok)
	ev = readEvent(t, c)
	require.Equal(t, wstypes.EventTypeMessage, ev.Type)
	p := eventPayload[wstypes.MessageEvent](t, ev)
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
	ev := readEvent(t, c)
	require.Equal(t, wstypes.EventTypeMessage, ev.Type)
	require.NotNil(t, mgr.Get("agent-outlive"))
}
