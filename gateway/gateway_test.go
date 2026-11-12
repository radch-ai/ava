package gateway_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/gateway"
)

func dialGateway(t *testing.T) (*coderws.Conn, context.Context) {
	t.Helper()
	handler, err := gateway.New(&gateway.Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
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

func TestPromptReceivedOverWS(t *testing.T) {
	conn, ctx := dialGateway(t)

	err := wsjson.Write(ctx, conn, map[string]string{
		"type":     "prompt",
		"agent_id": "_superagent",
	})
	if err != nil {
		t.Fatalf("writing prompt failed: %v", err)
	}

	// The turn-loop behind the surface is a placeholder: the prompt is
	// consumed and the connection stays open with nothing following it.
	rctx, rcancel := context.WithTimeout(ctx, 1*time.Second)
	defer rcancel()
	_, _, rerr := conn.Reader(rctx)
	if rerr == nil {
		t.Fatal("unexpected inbound frame")
	}
	if coderws.CloseStatus(rerr) != -1 {
		t.Fatalf("connection closed after prompt: %v", rerr)
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
	for _, key := range []string{"active", "model", "queue_len", "conn_count"} {
		if _, exists := data[key]; !exists {
			t.Errorf("lane_status data missing %s", key)
		}
	}
}
