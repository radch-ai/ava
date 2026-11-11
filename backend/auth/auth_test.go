package auth_test

// The auth surface over the real backend stack: exchange mints a
// session, me probes it, logout revokes it, and every backend route
// behind the guard demands it. Wire shapes mirror the UI contract
// (lib/auth/store.svelte.ts, msw handlers); paths are mount-relative
// like the handler this exercises.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/auth"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

const testUIToken = "test-ui-token-0123456789abcdef"

// newAuthStack builds the surface exactly as the binary mounts it:
// backend.New's router guarded by the middleware, over a real store.
func newAuthStack(t *testing.T) http.Handler {
	t.Helper()

	apiHandler, authSessions, err := backend.New(testdb.Open(t, true), zerolog.Nop(),
		bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	middleware, err := auth.NewMiddleware(testUIToken, authSessions)
	if err != nil {
		t.Fatalf("auth.NewMiddleware: %v", err)
	}
	return middleware.Authenticate(apiHandler)
}

// do runs one request, optionally carrying a session cookie.
func do(h http.Handler, method, path, body, sessionID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: "ava_session", Value: sessionID})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doExchange(h http.Handler, token string) *httptest.ResponseRecorder {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		panic(err)
	}
	return do(h, http.MethodPost, "/auth/exchange", string(body), "")
}

// sessionCookieOf returns the ava_session cookie a response set, if any.
func sessionCookieOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "ava_session" {
			return c
		}
	}
	return nil
}

// login exchanges the test token and returns the minted session id.
func login(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := doExchange(h, testUIToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange = %d, want 200", rec.Code)
	}
	c := sessionCookieOf(t, rec)
	if c == nil {
		t.Fatal("exchange set no ava_session cookie")
	}
	return c.Value
}

func TestExchangeRejectsWrongToken(t *testing.T) {
	h := newAuthStack(t)

	rec := doExchange(h, "wrong-token")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("exchange wrong token = %d, want 401", rec.Code)
	}
	if want := `"message":"Invalid token"`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("exchange wrong token body = %q, want %s", rec.Body.String(), want)
	}
	if c := sessionCookieOf(t, rec); c != nil {
		t.Errorf("exchange wrong token set %q cookie, want none", c.Value)
	}
}

func TestExchangeMalformedBody(t *testing.T) {
	h := newAuthStack(t)

	rec := do(h, http.MethodPost, "/auth/exchange", "not-json", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("exchange malformed body = %d, want 400", rec.Code)
	}
}

func TestExchangeMintsVerifiableSession(t *testing.T) {
	h := newAuthStack(t)

	rec := doExchange(h, testUIToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange = %d, want 200", rec.Code)
	}
	c := sessionCookieOf(t, rec)
	if c == nil {
		t.Fatal("exchange set no ava_session cookie")
	}
	if c.Value == "" {
		t.Fatal("session cookie value is empty")
	}
	if !c.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("session cookie SameSite = %v, want Strict", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("session cookie Path = %q, want /", c.Path)
	}

	// The cookie's id must open a live session — the me probe says so;
	// where it seals is the bridge's own contract (sessions_test.go).
	if rec := do(h, http.MethodGet, "/auth/me", "", c.Value); rec.Code != http.StatusOK {
		t.Errorf("me with minted session = %d, want 200", rec.Code)
	}
}

func TestMeProbesSession(t *testing.T) {
	h := newAuthStack(t)

	rec := do(h, http.MethodGet, "/auth/me", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("me without cookie = %d, want 401", rec.Code)
	}
	if want := `"message":"No session"`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("me without cookie body = %q, want %s", rec.Body.String(), want)
	}

	id := login(t, h)
	rec = do(h, http.MethodGet, "/auth/me", "", id)
	if rec.Code != http.StatusOK {
		t.Errorf("me with session = %d, want 200", rec.Code)
	}
}

func TestGuardDemandsSessionOnBackendRoutes(t *testing.T) {
	h := newAuthStack(t)

	rec := do(h, http.MethodGet, "/agents", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("agents without session = %d, want 401", rec.Code)
	}
	if want := `"message":"No session"`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("agents without session body = %q, want %s", rec.Body.String(), want)
	}

	id := login(t, h)
	rec = do(h, http.MethodGet, "/agents", "", id)
	if rec.Code != http.StatusOK {
		t.Errorf("agents with session = %d, want 200", rec.Code)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	h := newAuthStack(t)

	id := login(t, h)

	rec := do(h, http.MethodPost, "/auth/logout", "", id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", rec.Code)
	}
	clearing := sessionCookieOf(t, rec)
	if clearing == nil || clearing.MaxAge >= 0 {
		t.Fatalf("logout cookie = %+v, want a clearing cookie (MaxAge < 0)", clearing)
	}

	// The session is revoked server-side: me 401s with the old cookie.
	if rec := do(h, http.MethodGet, "/auth/me", "", id); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", rec.Code)
	}
}

func TestStaleCookieIsRejected(t *testing.T) {
	h := newAuthStack(t)

	// Well-formed id that was never minted: no entry, no session.
	fake := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 32 zero bytes, base64url
	rec := do(h, http.MethodGet, "/agents", "", fake)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("agents with never-minted cookie = %d, want 401", rec.Code)
	}
}

func TestLogoutWithoutSessionStillSucceeds(t *testing.T) {
	h := newAuthStack(t)

	rec := do(h, http.MethodPost, "/auth/logout", "", "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("logout without cookie = %d, want 204", rec.Code)
	}
	if sessionCookieOf(t, rec) == nil {
		t.Error("logout without cookie set no clearing cookie")
	}
}
