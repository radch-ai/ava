package server

import (
	"net/http"
)

// newCORSMiddleware returns a middleware that adds CORS headers for browser clients.
//
// Behavior:
//   - If Origin header matches an allowed origin, CORS headers are set
//   - If Origin is missing or not allowed, request continues without CORS headers
//   - Preflight OPTIONS requests always return 204 No Content
//   - Non-OPTIONS requests always proceed to the next handler
//
// This middleware does NOT block non-browser clients:
//   - Native mobile apps (iOS URLSession, Android OkHttp) typically send no Origin
//   - Server-to-server calls typically send no Origin
//   - These requests pass through normally without CORS headers
//
// CORS only matters for browser/webview JavaScript clients that enforce CORS policy.
func newCORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		originSet[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := originSet[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
