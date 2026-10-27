package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

func TestWSHelloThenClose(t *testing.T) {
	h, err := New(&Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, srv.URL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if string(data) != `{"type":"hello"}` {
		t.Errorf("hello = %s, want %s", data, `{"type":"hello"}`)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	conn.Ping(readCtx) // keep the peer engaged before expecting the close
	if _, _, err := conn.Read(readCtx); err == nil {
		t.Error("expected connection closed after hello, got another frame")
	}
}

func TestWSRejectsPlainGET(t *testing.T) {
	h, err := New(&Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("plain GET /ws = %d, want %d", resp.StatusCode, http.StatusUpgradeRequired)
	}
}
