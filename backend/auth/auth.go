// Package auth is the token→session auth layer: exchange, session
// guard, and the vault-backed session store (sessions.go). A session
// is a 256-bit cookie id sealed server-side, so logout revokes it
// there.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/radch-ai/ava/backend/apitypes"
)

// sessionCookie is the HttpOnly credential set by exchange.
const sessionCookie = "ava_session"

// Middleware guards the API: token exchange up front, a session
// demanded everywhere else.
type Middleware struct {
	token    string
	sessions Sessions
}

// NewMiddleware builds the middleware over a session store.
func NewMiddleware(token string, sessions Sessions) (*Middleware, error) {
	if token == "" {
		return nil, errors.New("ui token must not be empty")
	}
	return &Middleware{token: token, sessions: sessions}, nil
}

// Guard admits a request only with a session cookie that verifies —
// for wrapping non-API handlers (the gateway WebSocket) with the same
// session check the API gets, without the auth routes.
func (m *Middleware) Guard(h http.Handler) http.Handler {
	return guard(m.sessions, h)
}

// Authenticate wraps an API handler with this layer; both sides are
// mount-relative.
func (m *Middleware) Authenticate(apiHandler http.Handler) http.Handler {
	guarded := http.NewServeMux()
	// me is 200-empty proof: by the time it runs, the guard has already
	// verified the session (and answers 401 when it has not).
	guarded.HandleFunc("GET /auth/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded.Handle("/", apiHandler)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/exchange", exchange(m.token, m.sessions))
	mux.HandleFunc("POST /auth/logout", logout(m.sessions))
	mux.Handle("/", guard(m.sessions, guarded))
	return mux
}

// exchange trades the long-lived UI token for a sealed session and its
// cookie. The wire contract matches the UI's auth store and mocks:
// {token} in, empty 200 + Set-Cookie out, 401 "Invalid token" on
// mismatch.
func exchange(token string, sessions Sessions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.ExchangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "Invalid token")
			return
		}

		id, err := mintSessionID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Internal server error")
			return
		}
		meta, err := json.Marshal(sessionMeta{Created: time.Now().UTC().Format(time.RFC3339)})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Internal server error")
			return
		}
		if err := sessions.Create(r.Context(), id, meta); err != nil {
			writeError(w, http.StatusInternalServerError, "Internal server error")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    id,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusOK)
	}
}

// logout revokes the session and clears the cookie — always, so a
// missing or stale session logs out all the same (the UI ignores
// failures).
func logout(sessions Sessions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && validSessionID(c.Value) {
			if err := sessions.Revoke(r.Context(), c.Value); err != nil {
				writeError(w, http.StatusInternalServerError, "Internal server error")
				return
			}
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// guard admits a request only with a session cookie that verifies.
// 401 body shape matches the UI contract: ky surfaces the message and
// the layout guard sends the user to login.
func guard(sessions Sessions, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && validSessionID(c.Value) {
			if sessions.Verify(r.Context(), c.Value) == nil {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusUnauthorized, "No session")
	})
}

// writeError renders the UI's {message} error body.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apitypes.ErrorResponse{Message: message})
}

// sessionMeta is the JSON metadata sealed beside every session.
type sessionMeta struct {
	Created string `json:"created"`
}

// mintSessionID mints the 256-bit session id carried by the cookie.
func mintSessionID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// validSessionID checks the cookie's shape before it touches the
// store: base64url, exactly 32 decoded bytes.
func validSessionID(id string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(raw) == 32
}
