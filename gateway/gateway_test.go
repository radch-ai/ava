package gateway_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway"
)

func dialGateway(t *testing.T) (*coderws.Conn, context.Context) {
	t.Helper()
	return dialConfig(t, &gateway.Config{})
}

// dialConfig dials a fresh server built from cfg and consumes the
// connected event.
func dialConfig(t *testing.T, cfg *gateway.Config) (*coderws.Conn, context.Context) {
	t.Helper()
	handler, err := gateway.New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return dialHandler(t, handler)
}

// dialHandler serves handler over HTTP and dials it once, consuming the
// connected event.
func dialHandler(t *testing.T, handler http.Handler) (*coderws.Conn, context.Context) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return dialServer(t, server.URL)
}

// dialServer dials a running gateway server and consumes the connected
// event.
func dialServer(t *testing.T, url string) (*coderws.Conn, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(url, "http"), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("reading connected event failed: %v", err)
	}
	if connected["type"] != "connected" {
		t.Fatalf("expected connected event, got %v", connected["type"])
	}
	data, ok := connected["data"].(map[string]any)
	if !ok {
		t.Fatalf("connected event data is not a map: %+v", connected["data"])
	}
	if _, exists := data["conn_id"]; !exists {
		t.Fatal("connected event should include conn_id")
	}
	return conn, ctx
}

func TestPromptWithoutResolverEmitsError(t *testing.T) {
	conn, ctx := dialGateway(t)

	err := wsjson.Write(ctx, conn, map[string]any{
		"type":     "prompt",
		"agent_id": "_superagent",
		"data": map[string]any{
			"content": "hello",
			"model":   map[string]string{"id": "gpt-4o", "provider_id": "p-1"},
		},
	})
	if err != nil {
		t.Fatalf("writing prompt failed: %v", err)
	}

	// The lane runs the turn; with no model resolver bound, the turn
	// fails and the failure reaches the wire. Queue admission and the
	// turn's lane_status bracket come first; read past them.
	var event map[string]any
	for {
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("reading turn outcome failed: %v", err)
		}
		switch event["type"] {
		case "lane_status", "queue_result", "queue_state":
			continue
		}
		break
	}
	if event["type"] != "error" {
		t.Fatalf("expected error event, got %v", event["type"])
	}
	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatalf("error event data is not a map: %+v", event["data"])
	}
	msg, _ := data["message"].(string)
	if msg == "" {
		t.Fatal("error event should carry a message")
	}
}

func TestPromptResolverErrorReachesWire(t *testing.T) {
	resolve := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows in result set")
	}
	conn, ctx := dialConfig(t, &gateway.Config{ResolveModel: resolve})

	err := wsjson.Write(ctx, conn, map[string]any{
		"type":     "prompt",
		"agent_id": "_superagent",
		"data": map[string]any{
			"content": "hello",
			"model":   map[string]string{"id": "gpt-4o", "provider_id": "p-1"},
		},
	})
	if err != nil {
		t.Fatalf("writing prompt failed: %v", err)
	}

	// A resolver failure — provider missing from the store — reaches
	// the submitting connection verbatim, past the queue admission
	// ack and the turn's lane_status bracket.
	var event map[string]any
	for {
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("reading turn outcome failed: %v", err)
		}
		switch event["type"] {
		case "lane_status", "queue_result", "queue_state":
			continue
		}
		break
	}
	if event["type"] != "error" {
		t.Fatalf("expected error event, got %v", event["type"])
	}
	data, _ := event["data"].(map[string]any)
	msg, _ := data["message"].(string)
	if !strings.Contains(msg, "read provider p-1") {
		t.Fatalf("error event should carry the resolver failure, got %q", msg)
	}
}

func TestStatusRepliesLaneStatus(t *testing.T) {
	conn, ctx := dialGateway(t)

	err := wsjson.Write(ctx, conn, map[string]string{
		"type":     "status",
		"agent_id": "_superagent",
	})
	if err != nil {
		t.Fatalf("writing status request failed: %v", err)
	}

	var event map[string]any
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("reading lane_status failed: %v", err)
	}
	if event["type"] != "lane_status" {
		t.Fatalf("expected lane_status event, got %v", event["type"])
	}
	if event["agent_id"] != "_superagent" {
		t.Fatalf("expected lane_status for _superagent, got %v", event["agent_id"])
	}
	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatalf("lane_status data is not a map: %+v", event["data"])
	}
	for _, key := range []string{"active", "working", "reasoning", "model", "queue_len", "conn_count"} {
		if _, exists := data[key]; !exists {
			t.Errorf("lane_status data missing %s", key)
		}
	}
	if data["reasoning"] != false {
		t.Errorf("status for a lane-less agent should not be reasoning, got %v", data["reasoning"])
	}
	if data["active"] != false {
		t.Errorf("status for a lane-less agent should be inactive, got %v", data["active"])
	}
	if data["conn_count"] != float64(0) {
		t.Errorf("status for a lane-less agent should have no connections, got %v", data["conn_count"])
	}
}

// A status frame binds the socket to one agent exactly like a prompt:
// a cross-agent frame afterwards is rejected, not a silent re-bind.
func TestStatusBindsAgentLikePrompt(t *testing.T) {
	conn, ctx := dialGateway(t)

	// Join agent A through the status frame and read its reply.
	err := wsjson.Write(ctx, conn, map[string]string{
		"type": "status", "agent_id": "agent-a",
	})
	if err != nil {
		t.Fatalf("writing status failed: %v", err)
	}
	var laneStatus map[string]any
	if err := wsjson.Read(ctx, conn, &laneStatus); err != nil {
		t.Fatalf("reading lane_status failed: %v", err)
	}
	if laneStatus["type"] != "lane_status" {
		t.Fatalf("expected lane_status, got %v", laneStatus["type"])
	}

	// A prompt for agent B on the same socket is dropped; then a
	// prompt for the bound agent is admitted. The first queue_result
	// to arrive must answer the bound prompt — the cross-agent frame
	// is never answered.
	err = wsjson.Write(ctx, conn, map[string]any{
		"type": "prompt", "agent_id": "agent-b",
		"data": map[string]any{"request_id": "r1", "content": "hi"},
	})
	if err != nil {
		t.Fatalf("writing cross-agent prompt failed: %v", err)
	}
	err = wsjson.Write(ctx, conn, map[string]any{
		"type": "prompt", "agent_id": "agent-a",
		"data": map[string]any{"request_id": "r2", "content": "hi"},
	})
	if err != nil {
		t.Fatalf("writing bound prompt failed: %v", err)
	}
	for {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("reading queue_result failed: %v", err)
		}
		if event["type"] != "queue_result" {
			continue
		}
		data := event["data"].(map[string]any)
		if data["request_id"] == "r1" {
			t.Fatal("cross-agent prompt must not be answered")
		}
		if data["request_id"] != "r2" {
			continue
		}
		if data["accepted"] != true {
			t.Fatalf("bound-agent prompt must be accepted, got %+v", data)
		}
		break
	}
}

// Prompt content is validated at admission: whitespace-only and
// over-length prompts are rejected through queue_result with no queue
// state change, and admitted content is trimmed.
func TestPromptContentValidated(t *testing.T) {
	conn, ctx := dialGateway(t)

	write := func(requestID, content string) {
		t.Helper()
		err := wsjson.Write(ctx, conn, map[string]any{
			"type": "prompt", "agent_id": "_superagent",
			"data": map[string]any{"request_id": requestID, "content": content},
		})
		if err != nil {
			t.Fatalf("writing prompt %s failed: %v", requestID, err)
		}
	}
	// readResult waits for the named reply. A rejected prompt must not
	// mutate the queue, so any queue_state seen while waiting for a
	// rejection's reply fails the test; the accepted prompt's state
	// is expected only after its reply.
	readResult := func(requestID string, allowQueueState bool) map[string]any {
		t.Helper()
		for {
			var event map[string]any
			if err := wsjson.Read(ctx, conn, &event); err != nil {
				t.Fatalf("reading events failed: %v", err)
			}
			if event["type"] == "queue_state" {
				if !allowQueueState {
					t.Fatalf("rejected prompt %s changed queue state: %+v", requestID, event["data"])
				}
				continue
			}
			if event["type"] != "queue_result" {
				continue
			}
			data := event["data"].(map[string]any)
			if data["request_id"] == requestID {
				return data
			}
		}
	}

	write("ws", "   ")
	if data := readResult("ws", false); data["accepted"] != false {
		t.Fatalf("whitespace prompt must be rejected, got %+v", data)
	} else if msg, _ := data["error"].(string); msg == "" {
		t.Fatal("rejection must carry a reason")
	}

	write("long", strings.Repeat("x", 64<<10+1))
	if data := readResult("long", false); data["accepted"] != false {
		t.Fatalf("over-length prompt must be rejected, got %+v", data)
	}

	write("trim", "  hi  ")
	if data := readResult("trim", true); data["accepted"] != true {
		t.Fatalf("trimmable prompt must be accepted, got %+v", data)
	}
	for {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("reading queue snapshot failed: %v", err)
		}
		if event["type"] != "queue_state" {
			continue
		}
		snapshot := event["data"].(map[string]any)
		items, _ := snapshot["items"].([]any)
		if len(items) == 0 {
			t.Fatalf("expected the admitted item in the snapshot, got %+v", snapshot)
		}
		first := items[0].(map[string]any)
		if first["content"] != "hi" {
			t.Fatalf("admitted content must be trimmed, got %q", first["content"])
		}
		break
	}
}

// sseFrame renders one chat.completion.chunk SSE payload.
func sseFrame(delta string) string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"role": "assistant", "content": delta},
		}},
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

// A status request doubles as the lane join: a page reopened mid-turn
// must receive the streamed prefix and everything after it, not a
// frozen working indicator.
func TestStatusJoinsLaneAndReplaysPrefix(t *testing.T) {
	release := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + sseFrame("par ") + "\n\n"))
		flusher.Flush()
		<-release
		_, _ = w.Write([]byte("data: " + sseFrame("tial") + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llm.Close()

	handler, err := gateway.New(&gateway.Config{
		ResolveModel: func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
			return backend.ResolvedModel{BaseURL: llm.URL, APIKey: "sk-test", Model: "test-model"}, nil
		},
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	conn1, ctx1 := dialServer(t, server.URL)
	conn2, ctx2 := dialServer(t, server.URL)

	// conn-1 starts the turn and watches the first chunk land.
	err = wsjson.Write(ctx1, conn1, map[string]any{
		"type":     "prompt",
		"agent_id": "_superagent",
		"data": map[string]any{
			"content": "hello",
			"model":   map[string]string{"id": "gpt-4o", "provider_id": "p-1"},
		},
	})
	if err != nil {
		t.Fatalf("writing prompt failed: %v", err)
	}
	for {
		var event map[string]any
		if err := wsjson.Read(ctx1, conn1, &event); err != nil {
			t.Fatalf("reading first chunk failed: %v", err)
		}
		if event["type"] == "message_chunk" {
			break
		}
	}

	// conn-2 arrives mid-turn, asks for status, and is joined into
	// the lane: the reply reports the turn, then the replayed prefix
	// and the streamed remainder follow.
	err = wsjson.Write(ctx2, conn2, map[string]string{
		"type":     "status",
		"agent_id": "_superagent",
	})
	if err != nil {
		t.Fatalf("writing status request failed: %v", err)
	}

	read := func() map[string]any {
		var event map[string]any
		if err := wsjson.Read(ctx2, conn2, &event); err != nil {
			t.Fatalf("reading joined event failed: %v", err)
		}
		return event
	}

	reply := read()
	if reply["type"] != "lane_status" {
		t.Fatalf("expected lane_status reply, got %v", reply["type"])
	}

	close(release)

	// The join backlog carries the queue snapshot before the prefix.
	chunk := read()
	for chunk["type"] == "queue_state" {
		chunk = read()
	}
	if chunk["type"] != "message_chunk" {
		t.Fatalf("expected replayed chunk, got %v", chunk["type"])
	}
	chunkData, _ := chunk["data"].(map[string]any)
	if chunkData["delta"] != "par " {
		t.Fatalf("expected replayed prefix %q, got %v", "par ", chunkData["delta"])
	}

	for {
		event := read()
		if event["type"] == "message_chunk" {
			continue
		}
		if event["type"] != "message" {
			t.Fatalf("expected final message, got %v", event["type"])
		}
		data, _ := event["data"].(map[string]any)
		if data["content"] != "par tial" {
			t.Fatalf("expected settled answer %q, got %v", "par tial", data["content"])
		}
		break
	}
}

// Server shutdown closes live connections with a GoingAway frame —
// the polite goodbye that tells clients not to reconnect instantly.
// The request context is cancelled mid-connection, exactly what a
// draining server does to its long-lived handlers.
func TestShutdownClosesConnectionsGoingAway(t *testing.T) {
	gw, err := gateway.New(&gateway.Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		time.AfterFunc(50*time.Millisecond, cancel)
		gw.ServeHTTP(w, r.WithContext(ctx))
	})
	server := httptest.NewServer(wrapped)
	defer server.Close()

	conn, ctx := dialServer(t, server.URL)

	// The handler's read loop unblocks on the cancelled context and
	// closes with GoingAway; the client sees the close frame.
	var event map[string]any
	err = wsjson.Read(ctx, conn, &event)
	if err == nil {
		t.Fatal("expected a close, got an event")
	}
	if got := coderws.CloseStatus(err); got != coderws.StatusGoingAway {
		t.Fatalf("expected GoingAway close, got status %v (%v)", got, err)
	}
}

func TestStatusReflectsLiveLane(t *testing.T) {
	conn, ctx := dialGateway(t)

	err := wsjson.Write(ctx, conn, map[string]any{
		"type":     "prompt",
		"agent_id": "_superagent",
		"data": map[string]any{
			"content": "hello",
			"model":   map[string]string{"id": "gpt-4o", "provider_id": "p-1"},
		},
	})
	if err != nil {
		t.Fatalf("writing prompt failed: %v", err)
	}

	err = wsjson.Write(ctx, conn, map[string]string{
		"type":     "status",
		"agent_id": "_superagent",
	})
	if err != nil {
		t.Fatalf("writing status request failed: %v", err)
	}

	// The connection carries the admission ack and queue snapshots
	// before the status reply; drain until the live lane reports
	// itself with the submitted model.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var event map[string]any
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("reading lane_status failed: %v", err)
		}
		if event["type"] != "lane_status" {
			continue
		}
		data, ok := event["data"].(map[string]any)
		if !ok {
			t.Fatalf("lane_status data is not a map: %+v", event["data"])
		}
		if data["active"] == true && data["model"] == "gpt-4o" {
			if data["conn_count"].(float64) < 1 {
				t.Errorf("submitting connection should be counted, got %v", data["conn_count"])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live lane never reported the submitted model: %+v", data)
		}
	}
}

func TestDisconnectDropsFromLane(t *testing.T) {
	handler, err := gateway.New(&gateway.Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	conn1, ctx := dialServer(t, server.URL)
	conn2, _ := dialServer(t, server.URL)

	prompt := map[string]any{
		"type":     "prompt",
		"agent_id": "_superagent",
		"data": map[string]any{
			"content": "hello",
			"model":   map[string]string{"id": "gpt-4o", "provider_id": "p-1"},
		},
	}
	for _, conn := range []*coderws.Conn{conn1, conn2} {
		if err := wsjson.Write(ctx, conn, prompt); err != nil {
			t.Fatalf("writing prompt failed: %v", err)
		}
	}

	// conn2 leaves; its lane membership must leave with it.
	_ = conn2.CloseNow()

	// The handler notices the closed peer asynchronously, so poll the
	// lane until it stops counting conn2.
	for range 20 {
		if err := wsjson.Write(ctx, conn1, map[string]string{
			"type":     "status",
			"agent_id": "_superagent",
		}); err != nil {
			t.Fatalf("writing status request failed: %v", err)
		}
		// Drain queue traffic from the prompts; the next lane_status
		// this poll reads is the reply.
		var event map[string]any
		for {
			if err := wsjson.Read(ctx, conn1, &event); err != nil {
				t.Fatalf("reading lane_status failed: %v", err)
			}
			if event["type"] != "lane_status" {
				continue
			}
			break
		}
		if event["data"].(map[string]any)["conn_count"].(float64) == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("disconnected connection was never removed from the lane")
}

// TestIdleConnectionSurvivesKeepalive proves the pre-keepalive failure
// mode is gone: a connection that sends nothing stays fully usable far
// past the old 60s read-idle drop, while the ping ticker runs against
// it the whole time (the client auto-pongs; a missed pong closes us).
func TestIdleConnectionSurvivesKeepalive(t *testing.T) {
	interval := 50 * time.Millisecond
	conn, ctx := dialConfig(t, &gateway.Config{PingInterval: interval})

	// Idle for many ping cycles — hundreds of pings with no inbound
	// frames from this side.
	time.Sleep(300 * time.Millisecond)

	err := wsjson.Write(ctx, conn, map[string]string{
		"type":     "status",
		"agent_id": "_superagent",
	})
	if err != nil {
		t.Fatalf("status write after idle failed: %v", err)
	}

	var event map[string]any
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("lane_status after idle failed: %v", err)
	}
	if event["type"] != "lane_status" {
		t.Fatalf("expected lane_status event, got %v", event["type"])
	}
}

// TestDeadPeerReaped proves a peer that stops answering pings without a
// TCP close (half-open: sleep, NAT drop) is closed by the gateway
// within one ping cycle + pong wait.
func TestDeadPeerReaped(t *testing.T) {
	interval := 50 * time.Millisecond
	cfg := &gateway.Config{PingInterval: interval}
	handler, err := gateway.New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Raw TCP + by-hand upgrade: a real client library would answer the
	// server's pings itself; this peer never reads a byte.
	nc, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("tcp dial failed: %v", err)
	}
	t.Cleanup(func() { _ = nc.Close() })

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + nc.RemoteAddr().String() + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := nc.Write([]byte(req)); err != nil {
		t.Fatalf("upgrade write failed: %v", err)
	}

	// Read the 101 handshake response, then consume the connected-event
	// frame the handler writes right after accept. From here on this
	// peer never reads a byte — no pongs.
	reader := bufio.NewReader(nc)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("handshake read failed: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101 upgrade, got %q", statusLine)
	}

	// Server frames are unmasked: 2-byte header + ~66-byte payload.
	if err := nc.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		t.Fatalf("setting drain deadline failed: %v", err)
	}
	if _, err := io.ReadFull(reader, make([]byte, 96)); err != nil {
		t.Fatalf("draining connected event failed: %v", err)
	}

	// The gateway must reap this silent peer within interval (first
	// ping) + pong wait (interval*10) + slack. The wire meanwhile
	// carries ping frames (opcode 0x9) — only a close frame (0x8) or
	// a FIN counts as reaped.
	reap := interval*12 + time.Second
	if err := nc.SetReadDeadline(time.Now().Add(reap)); err != nil {
		t.Fatalf("setting reap deadline failed: %v", err)
	}
	buf := make([]byte, 64)
	for {
		n, rerr := nc.Read(buf)
		if rerr != nil {
			if errors.Is(rerr, os.ErrDeadlineExceeded) {
				t.Fatal("dead peer not reaped in time")
			}
			break // EOF counts as reaped
		}
		if n == 0 {
			continue
		}
		if opcode := buf[0] & 0x0f; opcode == 0x8 {
			break // close frame: reaped
		}
	}
}
