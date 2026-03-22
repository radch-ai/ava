package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerTokenFromRequest(t *testing.T) {
	t.Run("prefers bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/agents", nil)
		req.Header.Set("Authorization", "Bearer access-token")

		if got := bearerTokenFromRequest(req); got != "access-token" {
			t.Fatalf("expected bearer token, got %q", got)
		}
	})

	t.Run("returns empty for session cookie without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/agents", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-token"})

		if got := bearerTokenFromRequest(req); got != "" {
			t.Fatalf("expected no token, got %q", got)
		}
	})

	t.Run("returns empty when auth missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/agents", nil)

		if got := bearerTokenFromRequest(req); got != "" {
			t.Fatalf("expected no token, got %q", got)
		}
	})
}

func TestCORSMiddleware(t *testing.T) {
	allowedOrigins := []string{"http://localhost:4173", "http://host.docker.internal:4173"}
	corsMiddleware := newCORSMiddleware(allowedOrigins)

	t.Run("handles preflight for allowed origin without calling next handler", func(t *testing.T) {
		handlerCalled := false
		handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodOptions, "/api/user/agent/test", nil)
		req.Header.Set("Origin", "http://host.docker.internal:4173")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")

		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if res.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", res.Code)
		}
		if handlerCalled {
			t.Fatal("expected preflight to short-circuit next handler")
		}
		if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://host.docker.internal:4173" {
			t.Fatalf("expected allow origin header, got %q", got)
		}
	})

	t.Run("handles preflight for disallowed origin without cors headers", func(t *testing.T) {
		handlerCalled := false
		handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodOptions, "/api/user/agent/test", nil)
		req.Header.Set("Origin", "http://evil.com")

		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if res.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", res.Code)
		}
		if handlerCalled {
			t.Fatal("expected preflight to short-circuit next handler")
		}
		if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("expected no allow origin header for disallowed origin, got %q", got)
		}
	})

	t.Run("handles preflight without origin header", func(t *testing.T) {
		handlerCalled := false
		handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodOptions, "/api/user/agent/test", nil)

		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if res.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", res.Code)
		}
		if handlerCalled {
			t.Fatal("expected preflight to short-circuit next handler")
		}
		if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("expected no allow origin header without origin, got %q", got)
		}
	})

	t.Run("adds cors headers to normal requests for allowed origin", func(t *testing.T) {
		handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/user/agents", nil)
		req.Header.Set("Origin", "http://host.docker.internal:4173")

		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", res.Code)
		}
		if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://host.docker.internal:4173" {
			t.Fatalf("expected allow origin header, got %q", got)
		}
	})

	t.Run("does not add cors headers for disallowed origin", func(t *testing.T) {
		handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/user/agents", nil)
		req.Header.Set("Origin", "http://evil.com")

		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", res.Code)
		}
		if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("expected no allow origin header for disallowed origin, got %q", got)
		}
	})
}
