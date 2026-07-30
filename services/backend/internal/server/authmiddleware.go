package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/pkg/zitadelvalidate"
	"github.com/rs/zerolog"

	internalctx "github.com/radch-ai/ava/services/backend/internal/ctx"
)

// AuthMiddleware handles authentication for incoming requests.
type AuthMiddleware struct {
	introspector *zitadelvalidate.Introspector
	logger       zerolog.Logger
}

// NewAuthMiddleware creates a new authentication middleware with the given introspector.
func NewAuthMiddleware(introspector *zitadelvalidate.Introspector, logger zerolog.Logger) *AuthMiddleware {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &AuthMiddleware{
		introspector: introspector,
		logger:       logger,
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
		m.logger.Debug().Str("path", r.URL.Path).Str("method", r.Method).Msg("authenticating request")

		sessionToken := bearerTokenFromRequest(r)
		if sessionToken == "" {
			m.logger.Debug().Str("path", r.URL.Path).Msg("no bearer token found")
			m.writeAuthError(w, "authentication_required", "Valid session required. Please login.")
			return
		}

		m.logger.Debug().Str("path", r.URL.Path).Msg("validating session with zitadel")

		// Validate with Zitadel introspection
		sessionInfo, err := m.introspector.ValidateSession(r.Context(), sessionToken)
		if err != nil {
			if errors.Is(err, zitadelvalidate.ErrSessionExpired) {
				m.logger.Debug().Str("path", r.URL.Path).Msg("session expired")
				m.writeAuthError(w, "session_expired", "Your session has expired. Please login again.")
				return
			}
			if errors.Is(err, zitadelvalidate.ErrSessionInvalid) {
				m.logger.Debug().Str("path", r.URL.Path).Msg("invalid session")
				m.writeAuthError(w, "authentication_required", "Valid session required. Please login.")
				return
			}
			m.logger.Error().Err(err).Str("path", r.URL.Path).Msg("auth service error")
			// Other errors (timeout, unavailable) - return 503
			http.Error(w, "Auth service unavailable", http.StatusServiceUnavailable)
			return
		}

		m.logger.Debug().Str("path", r.URL.Path).Str("user_id", sessionInfo.UserID).Msg("authentication successful")

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
