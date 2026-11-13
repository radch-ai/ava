package utils

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"
)

func TestServe_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, http.NotFoundHandler(), "127.0.0.1:0", zerolog.Nop())
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop after context cancel")
	}
}

func TestServe_ListenError(t *testing.T) {
	err := Serve(context.Background(), http.NotFoundHandler(), "bogus-addr", zerolog.Nop())
	if err == nil {
		t.Fatal("expected listen error, got nil")
	}
}

// TestServe_DrainCancelsLiveWebSocket proves the BaseContext wiring:
// a WebSocket handler blocked in a deadline-free read must unblock and
// close when Serve's context is cancelled, instead of stalling the
// graceful drain for its full 30s window.
func TestServe_DrainCancelsLiveWebSocket(t *testing.T) {
	wsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			var v json.RawMessage
			if err := wsjson.Read(r.Context(), conn, &v); err != nil {
				return
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	go func() {
		done <- Serve(ctx, wsHandler, addr, zerolog.Nop())
	}()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	var conn *websocket.Conn
	for range 50 {
		conn, _, err = websocket.Dial(dialCtx, "ws://"+addr, nil)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("ws dial never succeeded: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	cancel()

	// The server must close the live socket promptly — well under the
	// drain window. coder/websocket fails the connection when a read's
	// context cancels, so the peer sees EOF rather than a close frame;
	// either way the read unblocks instead of stalling the drain.
	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readCancel()
	var v json.RawMessage
	if rerr := wsjson.Read(readCtx, conn, &v); rerr == nil {
		t.Fatal("unexpected inbound message")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not drain after cancel")
	}
}

func TestServe_Healthz(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pick a free port, then hand it to Serve (the retry loop below covers
	// the gap while it binds).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, http.NotFoundHandler(), addr, zerolog.Nop())
	}()

	// Wait for the server to come up, then probe.
	var resp *http.Response
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("healthz never came up: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}

	// Non-GET must be rejected by the method pattern.
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/healthz", nil)
	postResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = postResp.Body.Close()
	if postResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz status = %d, want 405", postResp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("Serve did not stop after context cancel")
	}
}
