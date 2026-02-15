//go:build !integration
// +build !integration

package unit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_ValidSession(t *testing.T) {
	// Mock auth server
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header
		auth := r.Header.Get("Authorization")
		assert.Equal(t, "Bearer test-secret", auth)

		// Verify cookie
		cookies := r.Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, "session", cookies[0].Name)
		assert.Equal(t, "valid.session.token", cookies[0].Value)

		// Return success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"authenticated": true, "user_id": "user-123"}`))
	}))
	defer mockAuth.Close()

	// Create middleware without cache
	mw := server.NewAuthMiddleware(mockAuth.URL, "test-secret", nil, 5*time.Minute)

	// Create a handler that checks context
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := server.GetUserIDFromContext(r.Context())
		assert.Equal(t, "user-123", userID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler := mw.Authenticate(next)

	// Create request with valid session cookie
	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "valid.session.token"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "OK", w.Body.String())
}

func TestAuthMiddleware_MissingCookie(t *testing.T) {
	mw := server.NewAuthMiddleware("http://localhost:8081", "test-secret", nil, 5*time.Minute)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Authenticate(next)

	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "authentication_required", resp["error"])
	assert.Equal(t, "/auth/login/google", resp["login_url"])
}

func TestAuthMiddleware_InvalidSession(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mockAuth.Close()

	mw := server.NewAuthMiddleware(mockAuth.URL, "test-secret", nil, 5*time.Minute)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := mw.Authenticate(next)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "invalid.token"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.False(t, called, "next handler should not be called")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetUserIDFromContext(t *testing.T) {
	ctx := context.Background()
	ctx = server.WithUserID(ctx, "user-456")

	userID := server.GetUserIDFromContext(ctx)
	assert.Equal(t, "user-456", userID)
}

func TestGetUserIDFromContext_NotSet(t *testing.T) {
	ctx := context.Background()

	userID := server.GetUserIDFromContext(ctx)
	assert.Equal(t, "", userID)
}

func TestAuthMiddleware_CacheHit(t *testing.T) {
	// Mock cache that always returns a value
	mockCache := &mockCache{
		get: func(ctx context.Context, key string) (string, error) {
			return "cached-user", nil
		},
	}

	mw := server.NewAuthMiddleware("http://localhost:8081", "test-secret", mockCache, 5*time.Minute)

	authCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := server.GetUserIDFromContext(r.Context())
		assert.Equal(t, "cached-user", userID)
		authCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Authenticate(next)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "any.token"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, authCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_CacheMissThenHit(t *testing.T) {
	var cacheSet bool

	mockCache := &mockCache{
		get: func(ctx context.Context, key string) (string, error) {
			return "", nil // cache miss
		},
		set: func(ctx context.Context, key string, value string, ttl time.Duration) error {
			cacheSet = true
			assert.Equal(t, "user-123", value)
			assert.Equal(t, "session:valid.token", key)
			return nil
		},
	}

	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"authenticated": true, "user_id": "user-123"}`))
	}))
	defer mockAuth.Close()

	mw := server.NewAuthMiddleware(mockAuth.URL, "test-secret", mockCache, 5*time.Minute)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Authenticate(next)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "valid.token"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, cacheSet, "cache should be set on successful auth")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_ServiceUnavailable(t *testing.T) {
	// Server that returns 503
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mockAuth.Close()

	mw := server.NewAuthMiddleware(mockAuth.URL, "test-secret", nil, 5*time.Minute)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	handler := mw.Authenticate(next)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "valid.token"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestAuthMiddleware_SetCookieForwarding(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return success with Set-Cookie header (sliding expiration)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=new.token; Path=/; HttpOnly; Secure")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"authenticated": true, "user_id": "user-123"}`))
	}))
	defer mockAuth.Close()

	mw := server.NewAuthMiddleware(mockAuth.URL, "test-secret", nil, 5*time.Minute)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Authenticate(next)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "old.token"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Check that Set-Cookie was forwarded
	cookies := w.Header().Values("Set-Cookie")
	assert.NotEmpty(t, cookies, "Set-Cookie header should be forwarded")
}

func TestAuthMiddleware_EmptyCookieValue(t *testing.T) {
	mw := server.NewAuthMiddleware("http://localhost:8081", "test-secret", nil, 5*time.Minute)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	handler := mw.Authenticate(next)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: ""})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// mock cache implementation
type mockCache struct {
	get func(ctx context.Context, key string) (string, error)
	set func(ctx context.Context, key string, value string, ttl time.Duration) error
	del func(ctx context.Context, key string) error
}

func (m *mockCache) Get(ctx context.Context, key string) (string, error) {
	if m.get != nil {
		return m.get(ctx, key)
	}
	return "", errors.New("not found")
}

func (m *mockCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if m.set != nil {
		return m.set(ctx, key, value, ttl)
	}
	return nil
}

func (m *mockCache) Delete(ctx context.Context, key string) error {
	if m.del != nil {
		return m.del(ctx, key)
	}
	return nil
}
