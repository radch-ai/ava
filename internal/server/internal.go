package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
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

// Cache defines the interface for session caching.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// MockCache is a mock implementation of Cache for testing.
type MockCache struct {
	GetFunc    func(ctx context.Context, key string) (string, error)
	SetFunc    func(ctx context.Context, key string, value string, ttl time.Duration) error
	DeleteFunc func(key string) error
}

func (m *MockCache) Get(ctx context.Context, key string) (string, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, key)
	}
	return "", nil
}

func (m *MockCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if m.SetFunc != nil {
		return m.SetFunc(ctx, key, value, ttl)
	}
	return nil
}

func (m *MockCache) Delete(ctx context.Context, key string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(key)
	}
	return nil
}

// NewInternalRouter creates a router for internal endpoints.
// All routes are automatically protected by Bearer auth middleware.
func NewInternalRouter(secret string, cache Cache) *mux.Router {
	internalMW := NewInternalMiddleware(secret)

	router := mux.NewRouter()
	router.Use(internalMW.RequireInternalAuth)

	router.HandleFunc("/cache/delete", handleCacheInvalidation(cache)).Methods("POST")

	return router
}

// handleCacheInvalidation deletes a session from the cache.
// Used by auth service webhook on logout.
func handleCacheInvalidation(cache Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			UserID    string `json:"user_id"`
			SessionID string `json:"session_id,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if reqBody.UserID == "" {
			http.Error(w, "user_id is required", http.StatusBadRequest)
			return
		}

		if cache != nil && reqBody.SessionID != "" {
			cacheKey := "session:" + reqBody.SessionID
			_ = cache.Delete(r.Context(), cacheKey)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// HandleCacheInvalidation returns the handler function (for testing).
func HandleCacheInvalidation(cache Cache) http.HandlerFunc {
	return handleCacheInvalidation(cache)
}
