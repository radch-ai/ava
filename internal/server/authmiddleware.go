package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nevaria-ai/zitadel-validate"

	internalctx "github.com/nevaria-ai/backend/internal/ctx"
)

// AuthMiddleware handles authentication for incoming requests.
type AuthMiddleware struct {
	introspector *zitadelvalidate.Introspector
}

// NewAuthMiddleware creates a new authentication middleware with the given introspector.
func NewAuthMiddleware(introspector *zitadelvalidate.Introspector) *AuthMiddleware {
	return &AuthMiddleware{
		introspector: introspector,
	}
}

// Authenticate returns a middleware handler that validates the bearer token
// and adds user_id to context before calling next handler.
//
// Simple Auth Flow (no sliding sessions):
//   - Backend only validates token authenticity
//   - Client handles all token management via refresh tokens
//
// No Auth Cache:
//   - Auth happens on each request
//   - Self-hosted Zitadel has low latency to backend
//   - Real-time revocation checking (no stale cache)
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionToken := bearerTokenFromRequest(r)
		if sessionToken == "" {
			m.writeAuthError(w, "authentication_required", "Valid session required. Please login.")
			return
		}

		// Validate with Zitadel introspection
		sessionInfo, err := m.introspector.ValidateSession(r.Context(), sessionToken)
		if err != nil {
			if errors.Is(err, zitadelvalidate.ErrSessionExpired) {
				m.writeAuthError(w, "session_expired", "Your session has expired. Please login again.")
				return
			}
			if errors.Is(err, zitadelvalidate.ErrSessionInvalid) {
				m.writeAuthError(w, "authentication_required", "Valid session required. Please login.")
				return
			}
			// Other errors (timeout, unavailable) - return 503
			http.Error(w, "Auth service unavailable", http.StatusServiceUnavailable)
			return
		}

		// Add user ID to context and call next handler
		ctx := internalctx.WithUserID(r.Context(), sessionInfo.UserID)
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

// bearerTokenFromRequest extracts the bearer token from the Authorization header.
// Returns empty string if no valid bearer token is found.
func bearerTokenFromRequest(r *http.Request) string {
	authorization := r.Header.Get("Authorization")
	if bearer, ok := strings.CutPrefix(authorization, "Bearer "); ok {
		return strings.TrimSpace(bearer)
	}

	return ""
}
