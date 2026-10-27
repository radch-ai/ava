package utils

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

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
	ln.Close()

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
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}

	// Non-GET must be rejected by the method pattern.
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/healthz", nil)
	postResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
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
