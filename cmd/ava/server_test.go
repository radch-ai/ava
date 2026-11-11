package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/auth"
	"github.com/radch-ai/ava/backend/sqlstore"
	"github.com/radch-ai/ava/gateway"
	"github.com/radch-ai/ava/utils"
)

const chainToken = "chain-ui-token-0123456789"

// TestServerChain exercises the full wiring: utils.Serve's outer mux
// (healthz + delegation) on top of main's component mounts, auth
// included. Every endpoint is reached the way a real client would.
func TestServerChain(t *testing.T) {
	logger := zerolog.Nop()

	sqlDB, err := sqlstore.OpenSQLite(filepath.Join(t.TempDir(), "ava.db"))
	if err != nil {
		t.Fatalf("sqlstore.OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	apiRouter, authSessions, err := backend.New(sqlDB, logger, bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	authMiddleware, err := auth.NewMiddleware(chainToken, authSessions)
	if err != nil {
		t.Fatalf("auth.NewMiddleware: %v", err)
	}
	apiHandler := authMiddleware.Authenticate(apiRouter)

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

	// Same mounts main builds: the API guarded by the auth middleware,
	// stamped to prove delegation, then the /api prefix stripped at the
	// mount; gateway at /ws.
	stampedAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Chain", "api")
		apiHandler.ServeHTTP(w, r)
	})
	root := http.NewServeMux()
	root.Handle("/api/", http.StripPrefix("/api", stampedAPI))
	root.Handle("GET /ws", wsHandler)

	done := make(chan error, 1)
	go func() {
		done <- utils.Serve(ctx, root, addr, logger)
	}()
	waitUp := func(path string) {
		t.Helper()
		for range 100 {
			resp, err := http.Get("http://" + addr + path)
			if err == nil {
				_ = resp.Body.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("server never came up for %s", path)
	}

	// do runs one request, optionally carrying the session cookie.
	do := func(method, path, sessionID string) *http.Response {
		t.Helper()
		var body io.Reader
		if method == http.MethodPost && strings.HasSuffix(path, "/auth/exchange") {
			body = strings.NewReader(`{"token":"` + chainToken + `"}`)
		}
		req, _ := http.NewRequest(method, "http://"+addr+path, body)
		if sessionID != "" {
			req.AddCookie(&http.Cookie{Name: "ava_session", Value: sessionID})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	check := func(method, path string, wantStatus int, sessionID string) {
		t.Helper()
		resp := do(method, path, sessionID)
		if resp.StatusCode != wantStatus {
			t.Errorf("%s %s status = %d, want %d", method, path, resp.StatusCode, wantStatus)
		}
	}

	// exchange returns the session cookie the response set, if any.
	exchange := func() string {
		t.Helper()
		for _, c := range do(http.MethodPost, "/api/auth/exchange", "").Cookies() {
			if c.Name == "ava_session" {
				return c.Value
			}
		}
		return ""
	}

	waitUp("/healthz")

	// Serve's own endpoint, and the mounts it delegates to.
	check(http.MethodGet, "/healthz", http.StatusOK, "")
	check(http.MethodPost, "/healthz", http.StatusMethodNotAllowed, "")
	check(http.MethodGet, "/ws", http.StatusUpgradeRequired, "")
	check(http.MethodGet, "/", http.StatusNotFound, "")

	// Unauthenticated, the guard owns everything under /api.
	check(http.MethodGet, "/api/", http.StatusUnauthorized, "")
	check(http.MethodGet, "/api/agents", http.StatusUnauthorized, "")

	// Exchange mints a session through the whole chain, and the session
	// unlocks the backend — the stamp proves the request still reaches
	// the mounted handler.
	session := exchange()
	if session == "" {
		t.Fatal("exchange set no ava_session cookie")
	}
	check(http.MethodGet, "/api/auth/me", http.StatusOK, session)
	resp := do(http.MethodGet, "/api/agents", session)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authed agents status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Chain"); got != "api" {
		t.Errorf("authed agents header X-Chain = %q, want %q", got, "api")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}
