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
