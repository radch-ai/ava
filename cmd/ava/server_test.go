package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/gateway"
	"github.com/radch-ai/ava/utils"
)

// TestServerChain exercises the full wiring: utils.Serve's outer mux
// (healthz + delegation) on top of main's component mounts. Every
// endpoint is reached the way a real client would.
func TestServerChain(t *testing.T) {
	logger := zerolog.Nop()

	apiHandler, err := backend.New(&backend.Config{}, logger)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	wsHandler, err := gateway.New(&gateway.Config{}, logger)
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pick a free port, then hand it to Serve (the wait loop below covers
	// the gap while it binds).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	// Same mounts main builds: backend under /api, gateway at /ws. The
	// backend mount is wrapped to stamp a header, proving requests are
	// delegated to the mounted handler even while its route table is empty.
	stampedAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Chain", "api")
		apiHandler.ServeHTTP(w, r)
	})
	root := http.NewServeMux()
	root.Handle("/api/", stampedAPI)
	root.Handle("GET /ws", wsHandler)

	done := make(chan error, 1)
	go func() {
		done <- utils.Serve(ctx, root, addr, logger)
	}()

	waitUp := func(path string) {
		t.Helper()
		for i := 0; i < 100; i++ {
			resp, err := http.Get("http://" + addr + path)
			if err == nil {
				_ = resp.Body.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("server never came up for %s", path)
	}
	check := func(method, path string, wantStatus int) {
		t.Helper()
		req, _ := http.NewRequest(method, "http://"+addr+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != wantStatus {
			t.Errorf("%s %s status = %d, want %d", method, path, resp.StatusCode, wantStatus)
		}
	}

	checkHeader := func(method, path, name, want string) {
		t.Helper()
		req, _ := http.NewRequest(method, "http://"+addr+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if got := resp.Header.Get(name); got != want {
			t.Errorf("%s %s header %s = %q, want %q", method, path, name, got, want)
		}
	}

	waitUp("/healthz")

	// Serve's own endpoint.
	check(http.MethodGet, "/healthz", http.StatusOK)
	check(http.MethodPost, "/healthz", http.StatusMethodNotAllowed)

	// Delegated to the app mux: gateway rejects a plain GET without
	// upgrade headers; backend has no routes yet; unknown root paths 404.
	check(http.MethodGet, "/ws", http.StatusUpgradeRequired)
	check(http.MethodGet, "/api/", http.StatusNotFound)
	checkHeader(http.MethodGet, "/api/", "X-Chain", "api")
	check(http.MethodGet, "/", http.StatusNotFound)

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
