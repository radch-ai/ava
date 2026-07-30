package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newForTest(userHandler http.Handler, agentConfigHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/user/agents", userHandler)
	mux.Handle("/user/thread", userHandler)
	mux.Handle("/user/agent/", agentConfigHandler)
	return mux
}

func TestRouter_MountsUserAndAgentRoutes(t *testing.T) {
	userHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/agents", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	agentConfigHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/agent/ava", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})

	router := newForTest(userHandler, agentConfigHandler)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/user/agents", nil))
	assert.Equal(t, http.StatusOK, res.Code)
}

func TestRouter_MountsAgentRoute(t *testing.T) {
	userHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	agentConfigHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/agent/ava", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})

	router := newForTest(userHandler, agentConfigHandler)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/user/agent/ava", nil))
	assert.Equal(t, http.StatusOK, res.Code)
}

func TestRouter_MountsAgentActivityRoute(t *testing.T) {
	userHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	agentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Activity route now goes through /user/agent/{name}/activity
		assert.Equal(t, "/user/agent/ava/activity", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})

	router := newForTest(userHandler, agentHandler)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/user/agent/ava/activity", nil))
	assert.Equal(t, http.StatusOK, res.Code)
}
