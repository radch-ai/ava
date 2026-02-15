package server

import (
	"net/http"
	"strings"
)

// InternalMiddleware provides Bearer token authentication for internal endpoints.
type InternalMiddleware struct {
	secret string
}

// NewInternalMiddleware creates a new internal auth middleware.
func NewInternalMiddleware(secret string) *InternalMiddleware {
	return &InternalMiddleware{secret: secret}
}

// RequireInternalAuth wraps a handler to require Bearer token authentication.
// All internal endpoints MUST go through this middleware.
func (m *InternalMiddleware) RequireInternalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Expected format: "Bearer <secret>"
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, bearerPrefix)
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if token != m.secret {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
