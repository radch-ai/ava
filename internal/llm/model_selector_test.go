package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newTestServer(t *testing.T, response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v2/modelRouter/modelSelect", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
}

func newTestServerError(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

func newTestServerDelay(t *testing.T, response string, delay int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Duration(delay) * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
}

func TestModelSelector_SelectModel_Success(t *testing.T) {
	server := newTestServer(t, `{
		"provider": "openai",
		"model": "gpt-4",
		"confidence": 0.95
	}`)
	defer server.Close()

	selector := NewModelSelector(server.URL, "")

	messages := []Message{
		{Role: "user", Content: "test message"},
	}
	providers := []string{"openai", "anthropic"}
	tradeoff := "cost"

	result, err := selector.SelectModel(context.Background(), messages, providers, tradeoff)

	assert.NoError(t, err)
	assert.Equal(t, "openai", result.Provider)
	assert.Equal(t, "gpt-4", result.Model)
	assert.Equal(t, 0.95, result.Confidence)
}

func TestModelSelector_SelectModel_WithAPIKey(t *testing.T) {
	server := newTestServer(t, `{
		"provider": "anthropic",
		"model": "claude-3-sonnet",
		"confidence": 0.88
	}`)
	defer server.Close()

	selector := NewModelSelector(server.URL, "test-api-key")

	messages := []Message{
		{Role: "user", Content: "test"},
	}
	providers := []string{"anthropic"}
	tradeoff := "quality"

	result, err := selector.SelectModel(context.Background(), messages, providers, tradeoff)

	assert.NoError(t, err)
	assert.Equal(t, "anthropic", result.Provider)
	assert.Equal(t, "claude-3-sonnet", result.Model)
}

func TestModelSelector_SelectModel_EmptyMessages(t *testing.T) {
	server := newTestServer(t, `{
		"provider": "openai",
		"model": "gpt-3.5-turbo",
		"confidence": 0.7
	}`)
	defer server.Close()

	selector := NewModelSelector(server.URL, "")

	result, err := selector.SelectModel(context.Background(), []Message{}, []string{"openai"}, "speed")

	assert.NoError(t, err)
	assert.Equal(t, "openai", result.Provider)
}

func TestModelSelector_SelectModel_HTTPError(t *testing.T) {
	server := newTestServerError(t)
	defer server.Close()

	selector := NewModelSelector(server.URL, "")

	messages := []Message{{Role: "user", Content: "test"}}

	_, err := selector.SelectModel(context.Background(), messages, []string{"openai"}, "cost")

	assert.Error(t, err)
}

func TestModelSelector_SelectModel_InvalidJSON(t *testing.T) {
	server := newTestServer(t, `invalid json`)
	defer server.Close()

	selector := NewModelSelector(server.URL, "")

	messages := []Message{{Role: "user", Content: "test"}}

	_, err := selector.SelectModel(context.Background(), messages, []string{"openai"}, "cost")

	assert.Error(t, err)
}

func TestModelSelector_SelectModel_ContextTimeout(t *testing.T) {
	server := newTestServerDelay(t, `{
		"provider": "openai",
		"model": "gpt-4",
		"confidence": 0.95
	}`, 200)
	defer server.Close()

	selector := NewModelSelector(server.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10)
	defer cancel()

	_, err := selector.SelectModel(ctx, []Message{{Role: "user", Content: "test"}}, []string{"openai"}, "cost")

	assert.Error(t, err)
}

func TestNewModelSelector(t *testing.T) {
	selector := NewModelSelector("https://api.example.com", "test-key")

	assert.NotNil(t, selector)
	assert.Equal(t, "https://api.example.com", selector.baseURL)
	assert.Equal(t, "test-key", selector.apiKey)
}
