# Gateway-Auth Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add authentication middleware to gateway WebSocket service that validates session tokens against the auth service, with Redis caching for performance.

**Architecture:**
1. Create `internal/auth` package with HTTP client for auth service communication
2. Add authentication middleware that validates session cookies at WebSocket upgrade time
3. Implement Redis caching layer (reuse existing quota Redis client)
4. Add internal webhook endpoint for cache invalidation from auth service

**Tech Stack:**
- Go 1.24
- stdlib `net/http` (no external HTTP client dependencies)
- Existing Redis client from `internal/llm-proxy/quota/redis.go`
- testify for testing

**Prerequisites:**
- Auth service must have `INTERNAL_SECRET` configured
- Auth service must protect `/auth/session` with Bearer token validation (you implement this)

---

## Implementation Phases

**Phase 1: Auth Client** - HTTP client for session validation
**Phase 2: Configuration** - Environment variables and config loading
**Phase 3: Middleware** - Authentication middleware with Redis caching
**Phase 4: WebSocket Integration** - Wire middleware into WebSocket handler
**Phase 5: Cache Invalidation** - Internal webhook endpoint
**Phase 6: Testing** - Comprehensive unit and integration tests

---

## Phase 1: Auth Client

### Task 1.1: Create auth package directory

**Files:**
- Create: `internal/auth/`

**Step 1: Create directory**

Run: `mkdir -p /Projects/nevaria/gateway/internal/auth`

**Step 2: Create .gitkeep**

Run: `touch /Projects/nevaria/gateway/internal/auth/.gitkeep`

**Step 3: Commit**

```bash
git add internal/auth/.gitkeep
git commit -m "feat(auth): create auth package directory"
```

### Task 1.2: Create auth client types and errors

**Files:**
- Create: `internal/auth/client.go`

**Step 1: Write the failing test**

Create `tests/unit/auth_client_test.go`:

```go
//go:build !integration
// +build !integration

package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuthClient(t *testing.T) {
	client := NewAuthClient("http://localhost:8081", "test-secret", nil)

	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8081", client.baseURL)
	assert.Equal(t, "test-secret", client.secret)
	assert.NotNil(t, client.client)
}

func TestNewAuthClientDefaults(t *testing.T) {
	client := NewAuthClient("", "", nil)

	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8081", client.baseURL) // default
}

func TestAuthClientValidateSession_Success(t *testing.T) {
	// Mock auth server
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header
		auth := r.Header.Get("Authorization")
		assert.Equal(t, "Bearer test-secret", auth)

		// Verify cookie
		cookies := r.Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, "session", cookies[0].Name)

		// Return success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"authenticated": true, "user_id": "user-123"}`))
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", nil)
	info, err := client.ValidateSession(context.Background(), "session.token")

	require.NoError(t, err)
	assert.Equal(t, "user-123", info.UserID)
}

func TestAuthClientValidateSession_Unauthorized(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid token"}`))
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", nil)
	info, err := client.ValidateSession(context.Background(), "bad.token")

	assert.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestAuthClientValidateSession_Timeout(t *testing.T) {
	// Server that doesn't respond
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", &http.Client{Timeout: 100 * time.Millisecond})
	info, err := client.ValidateSession(context.Background(), "session.token")

	assert.Error(t, err)
	assert.Nil(t, info)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./tests/unit -run TestAuthClient -v`
Expected: FAIL with "undefined: NewAuthClient", "undefined: ErrSessionInvalid"

**Step 3: Write minimal implementation**

Create `internal/auth/client.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Errors returned by the auth client.
var (
	ErrSessionInvalid   = errors.New("session invalid")
	ErrSessionExpired   = errors.New("session expired")
	ErrAuthUnavailable  = errors.New("auth service unavailable")
	ErrInvalidResponse  = errors.New("invalid response from auth service")
	ErrContextCanceled  = errors.New("context canceled")
)

const (
	defaultAuthURL     = "http://localhost:8081"
	defaultTimeout      = 2 * time.Second
	sessionCookieName   = "session"
)

// SessionInfo contains validated session information.
type SessionInfo struct {
	UserID    string
	ExpiresAt time.Time
	// SetCookie is the cookie from auth service if session was extended (sliding expiration).
	// Gateway must forward this back to the browser using http.SetCookie().
	SetCookie *http.Cookie // nil if session wasn't extended
}

// Client is an HTTP client for the auth service.
type Client struct {
	baseURL string
	secret  string
	client  *http.Client
	logger  interface{} // zerolog.Logger, but interface{} to avoid import
}

// NewAuthClient creates a new auth service client.
func NewAuthClient(baseURL, secret string, logger interface{}) *Client {
	if baseURL == "" {
		baseURL = defaultAuthURL
	}

	return &Client{
		baseURL: baseURL,
		secret:  secret,
		client: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// ValidateSession validates a session token with the auth service.
// The token should be in the format {sessionId}.{secret}.
func (c *Client) ValidateSession(ctx context.Context, sessionToken string) (*SessionInfo, error) {
	// Build request URL
	reqURL, err := url.Parse(c.baseURL + "/auth/session")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid auth URL: %w", ErrAuthUnavailable, err)
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create request: %w", ErrAuthUnavailable, err)
	}

	// Add Authorization header with internal secret
	req.Header.Set("Authorization", "Bearer "+c.secret)
	req.Header.Set("Content-Type", "application/json")

	// Add session cookie
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: sessionToken,
	})

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("%w: %w", ErrContextCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: request timeout", ErrAuthUnavailable)
		}
		return nil, fmt.Errorf("%w: %w", ErrAuthUnavailable, err)
	}
	defer resp.Body.Close()

	// Handle response status
	switch resp.StatusCode {
	case http.StatusOK:
		// Parse successful response
		var result struct {
			Authenticated bool   `json:"authenticated"`
			UserID        string `json:"user_id"`
			ExpiresAt     int64  `json:"expires_at,omitempty"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("%w: failed to decode response: %w", ErrInvalidResponse, err)
		}
		if !result.Authenticated || result.UserID == "" {
			return nil, ErrSessionInvalid
		}

		// Capture session cookie for sliding expiration (auth service may extend session)
		// Gateway must forward this back to the browser
		var setCookie *http.Cookie
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "session" {
				setCookie = cookie
				break
			}
		}

		info := &SessionInfo{
			UserID: result.UserID,
		}
		if result.ExpiresAt > 0 {
			info.ExpiresAt = time.Unix(result.ExpiresAt, 0)
		}
		if setCookie != nil {
			info.SetCookie = setCookie
		}
		return info, nil

	case http.StatusUnauthorized:
		return nil, ErrSessionInvalid

	case http.StatusForbidden:
		return nil, fmt.Errorf("%w: invalid internal secret", ErrAuthUnavailable)

	case http.StatusServiceUnavailable:
		return nil, ErrAuthUnavailable

	default:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrAuthUnavailable, resp.StatusCode)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestAuthClient -v`
Expected: PASS

**Step 5: Add more comprehensive tests**

Add to `tests/unit/auth_client_test.go`:

```go
func TestAuthClientValidateSession_MalformedToken(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Even with malformed token, we should send the request
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", nil)

	testCases := []struct {
		name string
		token string
	}{
		{"empty", ""},
		{"no separator", "abc123"},
		{"two separators", "a.b.c"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := client.ValidateSession(context.Background(), tc.token)
			assert.Error(t, err)
			assert.Nil(t, info)
		})
	}
}

func TestAuthClientValidateSession_InvalidJSON(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json}`))
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", nil)
	info, err := client.ValidateSession(context.Background(), "session.token")

	assert.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, ErrInvalidResponse)
}

func TestAuthClientValidateSession_MissingUserID(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"authenticated": true}`))
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", nil)
	info, err := client.ValidateSession(context.Background(), "session.token")

	assert.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, ErrSessionInvalid)
}

func TestAuthClientValidateSession_ContextCanceled(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	info, err := client.ValidateSession(ctx, "session.token")

	assert.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, ErrContextCanceled)
}

func TestAuthClientValidateSession_NonAuthenticatedResponse(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"authenticated": false}`))
	}))
	defer mockAuth.Close()

	client := NewAuthClient(mockAuth.URL, "test-secret", nil)
	info, err := client.ValidateSession(context.Background(), "session.token")

	assert.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, ErrSessionInvalid)
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestAuthClient -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/auth/client.go tests/unit/auth_client_test.go
git commit -m "feat(auth): add auth client with session validation"
```

---

## Phase 2: Configuration

### Task 2.1: Add auth configuration to config package

**Files:**
- Modify: `config/config.go`
- Create: `config/auth.go`

**Step 1: Write the failing test**

Add to `config/config_test.go`:

```go
func TestAuthConfigDefaults(t *testing.T) {
	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:8081", cfg.AuthURL)
	assert.Equal(t, 2*time.Second, cfg.AuthTimeout)
	assert.Equal(t, "", cfg.InternalSecret) // must be set in prod
}

func TestAuthConfigFromEnv(t *testing.T) {
	t.Setenv("AUTH_SERVICE_URL", "http://auth:9090")
	t.Setenv("AUTH_TIMEOUT", "5s")
	t.Setenv("INTERNAL_SECRET", "super-secret")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "http://auth:9090", cfg.AuthURL)
	assert.Equal(t, 5*time.Second, cfg.AuthTimeout)
	assert.Equal(t, "super-secret", cfg.InternalSecret)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./config -run TestAuthConfig -v`
Expected: FAIL with "unknown field AuthURL"

**Step 3: Add auth configuration**

Create `config/auth.go`:

```go
package config

import "time"

// AuthConfig contains authentication service configuration.
type AuthConfig struct {
	// URL is the base URL of the auth service.
	URL string `env:"AUTH_SERVICE_URL" envDefault:"http://localhost:8081"`

	// Timeout is the timeout for auth service requests.
	Timeout time.Duration `env:"AUTH_TIMEOUT" envDefault:"2s"`

	// InternalSecret is the shared secret for service-to-service authentication.
	// This is required when calling auth service endpoints.
	Secret string `env:"INTERNAL_SECRET"`
}
```

Modify `config/config.go` - add Auth field to Config struct:

```go
type Config struct {
	// ... existing fields ...

	// Auth contains auth service configuration.
	Auth AuthConfig
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./config -run TestAuthConfig -v`
Expected: PASS

**Step 5: Commit**

```bash
git add config/auth.go config/config.go config/config_test.go
git commit -m "feat(config): add auth service configuration"
```

---

## Phase 3: Middleware

### Task 3.1: Create middleware package with authentication middleware

**Files:**
- Create: `internal/server/middleware.go`

**Step 1: Write the failing test**

Create `tests/unit/middleware_test.go`:

```go
//go:build !integration
// +build !integration

package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_ValidSession(t *testing.T) {
	// Mock auth server
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"authenticated": true, "user_id": "user-123"}`))
	}))
	defer mockAuth.Close()

	// Create middleware
	mw := NewAuthMiddleware(mockAuth.URL, "test-secret", nil, 5*time.Minute)

	// Create a handler that checks context
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		assert.Equal(t, "user-123", userID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
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
	mw := NewAuthMiddleware("http://localhost:8081", "test-secret", nil, 5*time.Minute)

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
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "authentication_required", resp["error"])
}

func TestAuthMiddleware_InvalidSession(t *testing.T) {
	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mockAuth.Close()

	mw := NewAuthMiddleware(mockAuth.URL, "test-secret", nil, 5*time.Minute)

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
	ctx = WithUserID(ctx, "user-456")

	userID := GetUserIDFromContext(ctx)
	assert.Equal(t, "user-456", userID)
}

func TestGetUserIDFromContext_NotSet(t *testing.T) {
	ctx := context.Background()

	userID := GetUserIDFromContext(ctx)
	assert.Equal(t, "", userID)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./tests/unit -run TestAuthMiddleware -v`
Expected: FAIL with "undefined: NewAuthMiddleware"

**Step 3: Write middleware implementation**

Create `internal/server/middleware.go`:

```go
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

// AuthMiddleware handles authentication for incoming requests.
type AuthMiddleware struct {
	authClient    *auth.Client
	cache         Cache // can be nil
	cacheTTL      time.Duration
	logger        interface{} // zerolog.Logger
}

// Cache defines the interface for session caching.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
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
	json.NewEncoder(w).Encode(map[string]string{
		"error":       errorCode,
		"message":     message,
		"login_url":   "/auth/login/google",
	})
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestAuthMiddleware -v`
Expected: PASS

**Step 5: Add more tests for cache behavior**

Add to `tests/unit/middleware_test.go`:

```go
func TestAuthMiddleware_CacheHit(t *testing.T) {
	// Mock cache that always returns a value
	mockCache := &mockCache{
		get: func(key string) (string, error) {
			return "cached-user", nil
		},
	}

	mw := NewAuthMiddleware("http://localhost:8081", "test-secret", mockCache, 5*time.Minute)

	authCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
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
		get: func(key string) (string, error) {
			return "", nil // cache miss
		},
		set: func(key, value string, ttl time.Duration) error {
			cacheSet = true
			assert.Equal(t, "user-123", value)
			return nil
		},
	}

	mockAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"authenticated": true, "user_id": "user-123"}`))
	}))
	defer mockAuth.Close()

	mw := NewAuthMiddleware(mockAuth.URL, "test-secret", mockCache, 5*time.Minute)

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

// mock cache implementation
type mockCache struct {
	get func(key string) (string, error)
	set func(key, value string, ttl time.Duration) error
	del func(key string) error
}

func (m *mockCache) Get(ctx context.Context, key string) (string, error) {
	if m.get != nil {
		return m.get(key)
	}
	return "", errors.New("not found")
}

func (m *mockCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if m.set != nil {
		return m.set(key, value, ttl)
	}
	return nil
}

func (m *mockCache) Delete(ctx context.Context, key string) error {
	if m.del != nil {
		return m.del(key)
	}
	return nil
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestAuthMiddleware -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/server/middleware.go tests/unit/middleware_test.go
git commit -m "feat(server): add authentication middleware with cache support"
```

---

## Phase 4: WebSocket Integration

### Task 4.1: Wire auth middleware into WebSocket handler

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/websocket/handler.go`

**Step 1: Write integration test**

Create `tests/integration/websocket_auth_test.go`:

```go
//go:build integration
// +build integration

package integration_test

import (
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// WebSocketAuthSuite tests WebSocket authentication
type WebSocketAuthSuite struct {
	suite.Suite
	server *testutil.TestGatewayServer
}

func TestWebSocketAuthSuite(t *testing.T) {
	suite.Run(t, new(WebSocketAuthSuite))
}

func (s *WebSocketAuthSuite) SetupSuite() {
	// TODO: Set up test gateway with auth
}

func (s *WebSocketAuthSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Cleanup()
	}
}

func (s *WebSocketAuthSuite) TestWebSocketUpgrade_ValidSession_Succeeds() {
	s.T().Skip("TODO: implement")
}

func (s *WebSocketAuthSuite) TestWebSocketUpgrade_MissingCookie_401() {
	s.T().Skip("TODO: implement")
}
```

**Step 2: Modify server to include auth middleware**

Modify `internal/server/server.go`:

```go
// Add auth middleware field
type Server struct {
	httpServer     *http.Server
	logger         zerolog.Logger
	wsHandler      *websocket.Handler
	authMiddleware *server.AuthMiddleware  // ADD THIS
	shutdownCh     chan struct{}
}

// Update New signature
func New(addr string, logger zerolog.Logger, wsHandler *websocket.Handler, authMW *server.AuthMiddleware) *Server {
	// ... existing code ...
	return &Server{
		// ... existing fields ...
		authMiddleware: authMW,
		shutdownCh:     make(chan struct{}),
	}
}
```

**Step 3: Apply auth middleware to WebSocket route**

Modify `internal/server/server.go` in the `New` function where routes are registered:

```go
// Create auth middleware (pass config values)
authMW := server.NewAuthMiddleware(
	config.Auth.URL,
	config.Auth.Secret,
	nil, // TODO: Redis cache
	5*time.Minute,
)

// Apply auth middleware to WebSocket route
var wsHandler http.Handler = wsHandler.HandleWebSocket
wsHandler = authMW.Authenticate(wsHandler)

// Register route
mux.Handle("/ws", wsHandler)
```

**Step 4: Modify WebSocket handler to use user_id from context**

Modify `internal/websocket/handler.go` in `HandleWebSocket` around line 193:

```go
import (
	"github.com/nevaria-ai/gateway/internal/server" // for GetUserIDFromContext
)

// In HandleWebSocket, replace the sessionID generation:
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// ... existing validation code ...

	// Get user ID from context (set by auth middleware)
	userID := server.GetUserIDFromContext(r.Context())
	if userID == "" {
		// This shouldn't happen if middleware is working
		h.logger.Error().Msg("user ID not found in context after auth")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Generate connection ID (session ID is now user ID)
	connID := uuid.New().String()
	sessionID := userID // Use user ID instead of random UUID

	logCtx := h.logger.With().
		Str("conn_id", connID).
		Str("user_id", userID).  // ADD THIS
		Str("remote_addr", r.RemoteAddr).
		Int("active_conns", int(atomic.LoadInt32(&h.activeConns))).
		Logger()

	// ... rest of function ...
```

**Step 5: Update WebSocket handler tests to account for auth**

Existing WebSocket tests will need to mock auth or skip. Update `internal/websocket/handler_test.go` to add a helper for authenticated requests:

```go
// Helper to create request with auth context
func authenticatedRequest(t *testing.T, userID string) *http.Request {
	req := httptest.NewRequest("GET", "/ws", nil)
	ctx := server.WithUserID(req.Context(), userID)
	return req.WithContext(ctx)
}
```

**Step 6: Run WebSocket tests**

Run: `go test ./internal/websocket -v`
Expected: Some tests fail (need auth context), update them to use authenticated requests

**Step 7: Commit**

```bash
git add internal/server/server.go internal/websocket/handler.go
git commit -m "feat(server): wire auth middleware into WebSocket handler"
```

---

## Phase 5: Internal Router with Bearer Auth Middleware

> **Design Change:** Instead of individual Bearer auth per endpoint, create a dedicated internal router at `/_internal/*` with middleware that automatically protects ALL internal endpoints. This is cleaner and more secure long-term.

### Task 5.1: Create internal auth middleware

**Files:**
- Create: `internal/server/internal.go`
- Create: `tests/unit/internal_test.go`

**Step 1: Write the failing test**

Create `tests/unit/internal_test.go`:

```go
//go:build !integration
// +build !integration

package unit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInternalMiddleware_ValidSecret_AllowsRequest(t *testing.T) {
	mw := NewInternalMiddleware("test-secret")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	handler := mw.RequireInternalAuth(next)

	req := httptest.NewRequest("POST", "/_internal/cache/delete", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "OK", w.Body.String())
}

func TestInternalMiddleware_MissingSecret_401(t *testing.T) {
	mw := NewInternalMiddleware("test-secret")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := mw.RequireInternalAuth(next)

	req := httptest.NewRequest("POST", "/_internal/cache/delete", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInternalMiddleware_InvalidSecret_403(t *testing.T) {
	mw := NewInternalMiddleware("test-secret")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := mw.RequireInternalAuth(next)

	req := httptest.NewRequest("POST", "/_internal/cache/delete", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong-secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestInternalMiddleware_MalformedAuthHeader_401(t *testing.T) {
	mw := NewInternalMiddleware("test-secret")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.RequireInternalAuth(next)

	testCases := []struct {
		name   string
		header string
	}{
		{"no bearer", "test-secret"},
		{"empty bearer", "Bearer "},
		{"missing space", "Bearertest-secret"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/_internal/test", nil)
			req.Header.Set("Authorization", tc.header)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./tests/unit -run TestInternal -v`
Expected: FAIL with "undefined: NewInternalMiddleware"

**Step 3: Write implementation**

Create `internal/server/internal.go`:

```go
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
		if token != m.secret {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestInternal -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/internal.go tests/unit/internal_test.go
git commit -m "feat(server): add internal auth middleware for /_internal/* endpoints"
```

### Task 5.2: Create internal router and cache invalidation handler

**Files:**
- Create: `internal/server/invalidation.go`
- Modify: `internal/server/server.go`

**Step 1: Write the failing test**

Add to `tests/unit/internal_test.go`:

```go
func TestInternalRouter_CacheInvalidation_ValidRequest(t *testing.T) {
	var deletedKey string
	mockCache := &mockCache{
		del: func(key string) error {
			deletedKey = key
			return nil
		},
	}

	internalRouter := NewInternalRouter("test-secret", mockCache)

	// Register the cache invalidation handler
	internalRouter.HandleFunc("/cache/delete", func(w http.ResponseWriter, r *http.Request) {
		// Handler implementation here
		HandleCacheInvalidation(mockCache)(w, r)
	})

	body := `{"user_id": "user-123", "session_id": "abc123"}`
	req := httptest.NewRequest("POST", "/_internal/cache/delete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	internalRouter.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "session:abc123", deletedKey)
}

func TestInternalRouter_UnprotectedPath_Rejects(t *testing.T) {
	mockCache := &mockCache{}
	internalRouter := NewInternalRouter("test-secret", mockCache)

	req := httptest.NewRequest("GET", "/_internal/test", nil)
	// No Authorization header
	w := httptest.NewRecorder()

	internalRouter.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./tests/unit -run TestInternalRouter -v`
Expected: FAIL with "undefined: NewInternalRouter"

**Step 3: Write implementation**

Add to `internal/server/internal.go`:

```go
import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// NewInternalRouter creates a router for internal endpoints.
// All routes registered on this router are automatically protected by Bearer auth.
func NewInternalRouter(secret string, cache Cache) *mux.Router {
	internalMW := NewInternalMiddleware(secret)

	router := mux.NewRouter()
	router.Use(internalMW.RequireInternalAuth)

	// Register internal endpoints here
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

		// Delete from cache
		if cache != nil && reqBody.SessionID != "" {
			cacheKey := "session:" + reqBody.SessionID
			_ = cache.Delete(r.Context(), cacheKey)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// HandleCacheInvalidation returns the handler function (for testing)
func HandleCacheInvalidation(cache Cache) http.HandlerFunc {
	return handleCacheInvalidation(cache)
}
```

**Step 4: Update server.go to mount internal router**

Modify `internal/server/server.go`:

```go
// Add field to Server struct
type Server struct {
	httpServer     *http.Server
	logger         zerolog.Logger
	wsHandler      *websocket.Handler
	authMiddleware *server.AuthMiddleware
	internalRouter *mux.Router  // ADD THIS
	shutdownCh     chan struct{}
}

// Update New function
func New(addr string, logger zerolog.Logger, wsHandler *websocket.Handler, authMW *server.AuthMiddleware, internalRouter *mux.Router) *Server {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// WebSocket endpoint with auth
	var wsHandler http.Handler = wsHandler.HandleWebSocket
	wsHandler = authMW.Authenticate(wsHandler)
	mux.Handle("/ws", wsHandler)

	// Internal router - all endpoints protected by Bearer auth
	mux.Handle("/_internal/", http.StripPrefix("/_internal", internalRouter))

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger:         logger,
		wsHandler:      wsHandler,
		authMiddleware: authMW,
		internalRouter: internalRouter,
		shutdownCh:     make(chan struct{}),
	}
}
```

**Step 5: Update cmd/gateway/main.go to create internal router**

Modify `cmd/gateway/main.go`:

```go
// Create internal router
internalRouter := server.NewInternalRouter(cfg.Auth.Secret, redisClient)

// Pass to server
srv := server.New(cfg.GatewayAddr, logger, wsHandler, authMW, internalRouter)
```

**Step 6: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestInternal -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/server/internal.go internal/server/server.go cmd/gateway/main.go tests/unit/internal_test.go
git commit -m "feat(server): add internal router with automatic Bearer auth protection"
```
```

**Step 4: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestHandleCacheInvalidation -v`
Expected: PASS

**Step 5: Add more tests**

Add to `tests/unit/invalidation_test.go`:

```go
func TestHandleCacheInvalidation_SessionIDOnly(t *testing.T) {
	called := false
	mockCache := &mockCache{
		del: func(key string) error {
			called = true
			assert.Equal(t, "session:session-xyz", key)
			return nil
		},
	}

	handler := NewCacheInvalidationHandler("test-secret", mockCache)

	body := map[string]string{"session_id": "session-xyz"}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/_internal/cache/delete", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleCacheInvalidation_NilCache(t *testing.T) {
	handler := NewCacheInvalidationHandler("test-secret", nil)

	body := map[string]string{"user_id": "user-123"}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/_internal/cache/delete", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()

	handler(w, req) // Should not panic

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleCacheInvalidation_InvalidJSON(t *testing.T) {
	handler := NewCacheInvalidationHandler("test-secret", &mockCache{})

	req := httptest.NewRequest("POST", "/_internal/cache/delete", strings.NewReader("invalid json"))
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCacheInvalidation_WrongMethod(t *testing.T) {
	handler := NewCacheInvalidationHandler("test-secret", &mockCache{})

	req := httptest.NewRequest("GET", "/_internal/cache/delete", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()

	handler(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./tests/unit -run TestHandleCacheInvalidation -v`
Expected: PASS

**Step 7: Wire into server**

Modify `internal/server/server.go` to register the invalidation handler:

```go
// In New function, after other routes:
invalidationHandler := server.NewCacheInvalidationHandler(config.Auth.Secret, redisClient)
mux.Handle("/_internal/cache/delete", invalidationHandler)
```

**Step 8: Commit**

```bash
git add internal/server/invalidation.go tests/unit/invalidation_test.go
git commit -m "feat(server): add cache invalidation webhook endpoint"
```

---

## Phase 6: Redis Cache Integration

### Task 6.1: Create Redis cache adapter

**Files:**
- Create: `internal/server/redis_cache.go`

**Step 1: Write the failing test**

Add to `tests/unit/middleware_test.go`:

```go
func TestRedisCache_GetSet(t *testing.T) {
	// This requires a real Redis or mock
	// For now, we'll test the interface compliance
	t.Skip("TODO: implement Redis cache adapter")
}
```

**Step 2: Create Redis cache adapter**

Create `internal/server/redis_cache.go`:

```go
package server

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements the Cache interface using Redis.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache creates a new Redis cache.
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

// Get retrieves a value from Redis.
func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", fmt.Errorf("key not found")
		}
		return "", err
	}
	return val, nil
}

// Set stores a value in Redis with TTL.
func (c *RedisCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes a value from Redis.
func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}
```

**Step 3: Wire Redis cache into server startup**

Modify `cmd/gateway/main.go` to create Redis cache and pass to middleware:

```go
// Create Redis client (reuse from quota)
redisClient := redis.NewClient(&redis.Options{
	Addr:     cfg.RedisAddr,
	Password: cfg.RedisPassword,
})

// Create cache
var cache server.Cache
if cfg.RedisAddr != "" {
	cache = server.NewRedisCache(redisClient)
} else {
	log.Warn().Msg("Redis not configured, session caching disabled")
}

// Create auth middleware with cache
authMW := server.NewAuthMiddleware(
	cfg.Auth.URL,
	cfg.Auth.Secret,
	cache,
	5*time.Minute,
)
```

**Step 4: Commit**

```bash
git add internal/server/redis_cache.go cmd/gateway/main.go
git commit -m "feat(server): add Redis cache adapter for session caching"
```

---

## Phase 7: Comprehensive Testing

### Task 7.1: Create test utilities

**Files:**
- Create: `tests/testutil/mock_auth.go`
- Create: `tests/testutil/mock_redis.go`

**Step 1: Create mock auth server**

Create `tests/testutil/mock_auth.go`:

```go
package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// MockAuthServer is a controllable mock auth service for testing.
type MockAuthServer struct {
	Server    *httptest.Server
	mu        sync.Mutex
	responses map[string]MockAuthResponse
}

type MockAuthResponse struct {
	Status      int
	Body        interface{}
	Delay       time.Duration // optional delay before responding
}

func NewMockAuthServer() *MockAuthServer {
	m := &MockAuthServer{
		responses: make(map[string]MockAuthResponse),
	}

	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-secret" {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// Get cookie
		cookie, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		m.mu.Lock()
		resp := m.responses[cookie.Value]
		m.mu.Unlock()

		if resp.Delay > 0 {
			time.Sleep(resp.Delay)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.Status)
		if resp.Body != nil {
			json.NewEncoder(w).Encode(resp.Body)
		}
	}))

	return m
}

func (m *MockAuthServer) SetResponse(token string, status int, body interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[token] = MockAuthResponse{Status: status, Body: body}
}

func (m *MockAuthServer) Close() {
	m.Server.Close()
}
```

**Step 2: Create mock Redis cache**

Create `tests/testutil/mock_redis.go`:

```go
package testutil

import (
	"context"
	"sync"
	"time"
)

// MockRedisCache is an in-memory cache for testing.
type MockRedisCache struct {
	mu    sync.RWMutex
	data  map[string]cacheEntry
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

func NewMockRedisCache() *MockRedisCache {
	return &MockRedisCache{
		data: make(map[string]cacheEntry),
	}
}

func (m *MockRedisCache) Get(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.data[key]
	if !ok {
		return "", errors.New("not found")
	}

	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(m.data, key)
		return "", errors.New("expired")
	}

	return entry.value, nil
}

func (m *MockRedisCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	m.data[key] = cacheEntry{
		value:     value,
		expiresAt: expiresAt,
	}
	return nil
}

func (m *MockRedisCache) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}
```

**Step 3: Commit**

```bash
git add tests/testutil/mock_auth.go tests/testutil/mock_redis.go
git commit -m "test: add mock auth server and Redis cache for testing"
```

### Task 7.2: Integration tests with compose.test.yaml

**Files:**
- Create: `tests/compose.test.yaml`

**Step 1: Create docker compose for test services**

Create `tests/compose.test.yaml`:

```yaml
version: "3.8"

services:
  test-redis:
    image: redis:7-alpine
    ports:
      - "6380:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 1s
      timeout: 1s
      retries: 5

  test-auth:
    image: nevaria/auth:test
    build:
      context: ../../auth
      dockerfile: Dockerfile
    environment:
      - DATABASE_URL=postgres://postgres:test@test-postgres:5432/test_auth
      - AUTH_ADDR=:8081
      - INTERNAL_SECRET=test-secret
      - SESSION_EXPIRE_SECONDS=3600
    ports:
      - "8082:8081"
    depends_on:
      test-postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8081/health"]
      interval: 1s
      timeout: 1s
      retries: 5

  test-postgres:
    image: postgres:16-alpine
    environment:
      - POSTGRES_DB=test_auth
      - POSTGRES_PASSWORD=test
      - POSTGRES_USER=postgres
    ports:
      - "5433:5432"
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "postgres"]
      interval: 1s
      timeout: 1s
      retries: 5
```

**Step 2: Add test helper for starting services**

Add to `tests/integration/common_test.go`:

```go
//go:build integration
// +build integration

package integration_test

import (
	"os"
	"testing"
	"time"
)

// StartTestServices starts docker compose services for integration tests.
// Skip if COMPOSE_TEST_SERVICES is not "true".
func StartTestServices(t *testing.T) {
	if os.Getenv("COMPOSE_TEST_SERVICES") != "true" {
		t.Skip("Set COMPOSE_TEST_SERVICES=true to run with Docker services")
	}

	// Run docker compose up
	// This would typically use exec.Command
	t.Log("Starting test services via docker compose...")
}
```

**Step 3: Commit**

```bash
git add tests/compose.test.yaml tests/integration/common_test.go
git commit -m "test: add docker compose for integration test services"
```

---

## Final Steps

### Task 8.1: Update documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/README.md` (if exists)

**Step 1: Update main README**

Add to `README.md`:

```markdown
## Authentication

The gateway requires authenticated sessions for all WebSocket connections.

### Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `AUTH_SERVICE_URL` | Auth service base URL | `http://localhost:8081` |
| `AUTH_TIMEOUT` | Auth request timeout | `2s` |
| `INTERNAL_SECRET` | Shared secret for service-to-service auth | (required) |

### Flow

1. Client authenticates directly with auth service (OAuth2/WebAuthn)
2. Client receives session cookie
3. Client connects to gateway WebSocket with session cookie
4. Gateway validates session with auth service (cached in Redis)
5. On success: WebSocket connection established
6. On failure: 401 returned, client redirects to login
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add authentication configuration to README"
```

### Task 8.2: Final verification

**Step 1: Run all tests**

```bash
# Unit tests
go test ./tests/unit/... -v

# Integration tests (if services available)
go test ./tests/integration/... -tags=integration -v

# Package tests
go test ./internal/... -v
```

**Step 2: Build verification**

```bash
make build
```

**Step 3: Check test coverage**

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

Target: >85% coverage for new auth code.

---

## Summary

**Files Created:**
- `internal/auth/client.go`
- `internal/server/middleware.go`
- `internal/server/invalidation.go`
- `internal/server/redis_cache.go`
- `config/auth.go`
- `tests/unit/auth_client_test.go`
- `tests/unit/middleware_test.go`
- `tests/unit/invalidation_test.go`
- `tests/testutil/mock_auth.go`
- `tests/testutil/mock_redis.go`
- `tests/compose.test.yaml`

**Files Modified:**
- `config/config.go`
- `internal/server/server.go`
- `internal/websocket/handler.go`
- `cmd/gateway/main.go`
- `README.md`

**Total Tasks:** ~50 bite-sized steps following TDD.

**Estimated Implementation Time:** 3-4 hours with subagent-driven development.

---

**After implementation:**
1. Regenerate docs using project-workflow skill
2. Run full test suite
3. Create PR for code review
