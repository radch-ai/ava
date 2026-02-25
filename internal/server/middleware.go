package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nevaria-ai/gateway/internal/auth"
	internalctx "github.com/nevaria-ai/gateway/internal/ctx"
)

// Cache defines the interface for session caching.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// AuthMiddleware handles authentication for incoming requests.
type AuthMiddleware struct {
	authClient *auth.Client
	cache      Cache // can be nil
	cacheTTL   time.Duration
}

// NewAuthMiddleware creates a new authentication middleware.
func NewAuthMiddleware(authURL string, cache Cache, cacheTTL time.Duration) *AuthMiddleware {
	return &AuthMiddleware{
		authClient: auth.NewAuthClient(authURL, nil),
		cache:      cache,
		cacheTTL:   cacheTTL,
	}
}

// Authenticate validates the session cookie and adds user_id to context.
// IMPORTANT: Uses a response wrapper to capture Set-Cookie headers from auth service
// for sliding expiration. Gateway must forward these back to the browser.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract session cookie
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value == "" {
			m.writeAuthError(w, "authentication_required", "Valid session required. Please login.")
			return
		}

		sessionToken := cookie.Value
		var userID string
		var setCookie *http.Cookie // Set-Cookie to forward from auth service

		// Check cache first (if available)
		if m.cache != nil {
			cachedUserID, err := m.cache.Get(r.Context(), "session:"+sessionToken)
			if err == nil && cachedUserID != "" {
				userID = cachedUserID
			}
		}

		// Cache miss or no cache - validate with auth service
		if userID == "" {
			sessionInfo, err := m.authClient.ValidateSession(r.Context(), sessionToken)
			if err != nil {
				if errors.Is(err, auth.ErrSessionInvalid) {
					m.writeAuthError(w, "authentication_required", "Valid session required. Please login.")
					return
				}
				// Other errors (timeout, unavailable) - return 503
				http.Error(w, "Auth service unavailable", http.StatusServiceUnavailable)
				return
			}
			userID = sessionInfo.UserID
			setCookie = sessionInfo.SetCookie

			// Store in cache
			if m.cache != nil && userID != "" {
				_ = m.cache.Set(r.Context(), "session:"+sessionToken, userID, m.cacheTTL)
			}
		}

		// If we need to set a cookie, wrap the response writer
		wrapper := w
		if setCookie != nil {
			wrapper = &responseWrapper{ResponseWriter: w}
			http.SetCookie(wrapper, setCookie)
		}

		// Add user ID to context and call next handler
		ctx := internalctx.WithUserID(r.Context(), userID)
		next.ServeHTTP(wrapper, r.WithContext(ctx))
	})
}

// responseWrapper wraps http.ResponseWriter to capture Set-Cookie headers.
// It implements http.Hijacker to support WebSocket upgrades.
type responseWrapper struct {
	http.ResponseWriter
	wroteHeader bool
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(statusCode)
	}
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	// Don't call WriteHeader if it hasn't been called yet - let the handler do it
	// This is important for WebSocket upgrades
	return rw.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker for WebSocket upgrades.
// This is required for the WebSocket handler to work properly.
func (rw *responseWrapper) Hijack() (c interface{}, rw2 *bufio.ReadWriter, err error) {
	// Type assertion to http.Hijacker
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, errors.New("response writer cannot hijack connection")
}

// Flush implements http.Flusher for streaming responses.
func (rw *responseWrapper) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// writeAuthError writes a standardized authentication error response.
func (m *AuthMiddleware) writeAuthError(w http.ResponseWriter, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":     errorCode,
		"message":   message,
		"login_url": "/auth/login/google",
	})
}
