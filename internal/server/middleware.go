package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	internalctx "github.com/nevaria-ai/gateway/internal/ctx"
)

// AuthCache defines the interface for session caching.
type AuthCache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// AuthMiddleware handles authentication for incoming requests.
type AuthMiddleware struct {
	authClient *AuthClient
	cache      AuthCache // can be nil
	cacheTTL   time.Duration
}

// NewAuthMiddleware creates a new authentication middleware.
func NewAuthMiddleware(authURL string, cache AuthCache, cacheTTL time.Duration) *AuthMiddleware {
	return &AuthMiddleware{
		authClient: NewAuthClient(authURL),
		cache:      cache,
		cacheTTL:   cacheTTL,
	}
}

// Authenticate validates the session cookie and adds user_id to context.
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
				if errors.Is(err, ErrSessionInvalid) {
					m.writeAuthError(w, "authentication_required", "Valid session required. Please login.")
					return
				}
				// Other errors (timeout, unavailable) - return 503
				http.Error(w, "Auth service unavailable", http.StatusServiceUnavailable)
				return
			}
			userID = sessionInfo.UserID

			// Store in cache
			if m.cache != nil && userID != "" {
				_ = m.cache.Set(r.Context(), "session:"+sessionToken, userID, m.cacheTTL)
			}
		}

		// Add user ID to context and call next handler
		ctx := internalctx.WithUserID(r.Context(), userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
