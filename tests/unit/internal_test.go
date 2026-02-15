//go:build !integration
// +build !integration

package unit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nevaria-ai/gateway/internal/server"
	"github.com/stretchr/testify/assert"
)

func TestInternalMiddleware_ValidSecret_AllowsRequest(t *testing.T) {
	mw := server.NewInternalMiddleware("test-secret")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
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
	mw := server.NewInternalMiddleware("test-secret")

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
	mw := server.NewInternalMiddleware("test-secret")

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
	mw := server.NewInternalMiddleware("test-secret")

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

func TestInternalRouter_CacheInvalidation_ValidRequest(t *testing.T) {
	var deletedKey string
	mockCache := &server.MockCache{
		DeleteFunc: func(key string) error {
			deletedKey = key
			return nil
		},
	}

	internalRouter := server.NewInternalRouter("test-secret", mockCache)

	body := `{"user_id": "user-123", "session_id": "abc123"}`
	req := httptest.NewRequest("POST", "/cache/delete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	internalRouter.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "session:abc123", deletedKey)
}

func TestInternalRouter_UnprotectedPath_Rejects(t *testing.T) {
	mockCache := &server.MockCache{}
	internalRouter := server.NewInternalRouter("test-secret", mockCache)

	req := httptest.NewRequest("GET", "/cache/delete", nil)
	w := httptest.NewRecorder()

	internalRouter.ServeHTTP(w, req)

	// gorilla/mux returns 405 Method Not Allowed for wrong method on defined route
	// The auth middleware runs before route matching, so we get 401
	// But actually mux checks method before middleware runs
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestInternalRouter_CacheInvalidation_MissingUserID(t *testing.T) {
	mockCache := &server.MockCache{}
	internalRouter := server.NewInternalRouter("test-secret", mockCache)

	body := `{"session_id": "abc123"}`
	req := httptest.NewRequest("POST", "/cache/delete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	internalRouter.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "user_id is required")
}

func TestInternalRouter_CacheInvalidation_InvalidBody(t *testing.T) {
	mockCache := &server.MockCache{}
	internalRouter := server.NewInternalRouter("test-secret", mockCache)

	body := `invalid json`
	req := httptest.NewRequest("POST", "/cache/delete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	internalRouter.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInternalRouter_CacheInvalidation_Unauthorized(t *testing.T) {
	mockCache := &server.MockCache{}
	internalRouter := server.NewInternalRouter("test-secret", mockCache)

	body := `{"user_id": "user-123", "session_id": "abc123"}`
	req := httptest.NewRequest("POST", "/cache/delete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	internalRouter.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
