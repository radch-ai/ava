package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nevaria-ai/gateway/internal/auth"
)

// Context key type for user ID (prevents collisions)
type contextKey string

const userIDKey contextKey = "user_id"

// WithUserID adds a user ID to the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// GetUserIDFromContext retrieves the user ID from the context.
// Returns empty string if not set.
func GetUserIDFromContext(ctx context.Context) string {
	if userID, ok := ctx.Value(userIDKey).(string); ok {
		return userID
	}
	return ""
}

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
func NewAuthMiddleware(authURL, secret string, cache Cache, cacheTTL time.Duration) *AuthMiddleware {
	return &AuthMiddleware{
		authClient: auth.NewAuthClient(authURL, secret, nil),
		cache:      cache,
		cacheTTL:   cacheTTL,
	}
}

// Authenticate validates the session cookie and adds user_id to context.
// IMPORTANT: Uses a response wrapper to capture Set-Cookie headers from auth service
// for sliding expiration. Gateway must forward these back to the browser.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap response writer to capture Set-Cookie headers from auth service
		wrapper := &responseWrapper{ResponseWriter: w}

		// Extract session cookie
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value == "" {
			m.writeAuthError(wrapper, "authentication_required", "Valid session required. Please login.")
			return
		}

		sessionToken := cookie.Value
		var userID string

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
					m.writeAuthError(wrapper, "authentication_required", "Valid session required. Please login.")
					return
				}
				// Other errors (timeout, unavailable) - return 503
				http.Error(wrapper, "Auth service unavailable", http.StatusServiceUnavailable)
				return
			}
			userID = sessionInfo.UserID

			// Store in cache
			if m.cache != nil && userID != "" {
				_ = m.cache.Set(r.Context(), "session:"+sessionToken, userID, m.cacheTTL)
			}

			// Forward Set-Cookie header if auth service extended the session
			if sessionInfo.SetCookie != nil {
				http.SetCookie(wrapper, sessionInfo.SetCookie)
			}
		}

		// Add user ID to context and call next handler
		ctx := WithUserID(r.Context(), userID)
		next.ServeHTTP(wrapper, r.WithContext(ctx))
	})
}

// responseWrapper wraps http.ResponseWriter to capture Set-Cookie headers.
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
	rw.WriteHeader(http.StatusOK) // Ensure headers are written
	return rw.ResponseWriter.Write(b)
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
