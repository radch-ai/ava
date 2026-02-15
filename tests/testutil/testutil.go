// Package testutil provides test helpers and server setup for gateway tests.
//
// This package enables testing with:
// - HTTP test server helpers
// - WebSocket connection helpers
// - Mock cache implementation
// - JSON response helpers
// - Cookie extraction helpers
package testutil

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// TestServer wraps an HTTP test server with gateway-specific utilities.
type TestServer struct {
	Server  *httptest.Server
	BaseURL string
	Handler http.Handler
	Client  *http.Client
	Cleanup func()
}

// NewTestServer creates a new HTTP test server with the given handler.
func NewTestServer(t *testing.T, handler http.Handler) *TestServer {
	t.Helper()

	server := httptest.NewServer(handler)

	return &TestServer{
		Server:  server,
		BaseURL: server.URL,
		Handler: handler,
		Client:  server.Client(),
		Cleanup: func() {
			server.Close()
		},
	}
}

// NewTestServerWithTLS creates a new HTTPS test server with the given handler.
func NewTestServerWithTLS(t *testing.T, handler http.Handler) *TestServer {
	t.Helper()

	server := httptest.NewTLSServer(handler)

	return &TestServer{
		Server:  server,
		BaseURL: server.URL,
		Handler: handler,
		Client:  server.Client(),
		Cleanup: func() {
			server.Close()
		},
	}
}

// ============ HTTP Helper Methods ============

// Get sends a GET request to the test server.
func (ts *TestServer) Get(path string) *http.Response {
	req, _ := http.NewRequest("GET", ts.BaseURL+path, nil)
	resp, _ := ts.Client.Do(req)
	return resp
}

// GetWithHeaders sends a GET request with custom headers to the test server.
func (ts *TestServer) GetWithHeaders(path string, headers map[string]string) *http.Response {
	req, _ := http.NewRequest("GET", ts.BaseURL+path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, _ := ts.Client.Do(req)
	return resp
}

// PostJSON sends a POST request with JSON body to the test server.
func (ts *TestServer) PostJSON(path string, body string) *http.Response {
	req, _ := http.NewRequest("POST", ts.BaseURL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := ts.Client.Do(req)
	return resp
}

// PostForm sends a POST request with form data to the test server.
func (ts *TestServer) PostForm(path string, data string) *http.Response {
	req, _ := http.NewRequest("POST", ts.BaseURL+path, strings.NewReader(data))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := ts.Client.Do(req)
	return resp
}

// PutJSON sends a PUT request with JSON body to the test server.
func (ts *TestServer) PutJSON(path string, body string) *http.Response {
	req, _ := http.NewRequest("PUT", ts.BaseURL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := ts.Client.Do(req)
	return resp
}

// Delete sends a DELETE request to the test server.
func (ts *TestServer) Delete(path string) *http.Response {
	req, _ := http.NewRequest("DELETE", ts.BaseURL+path, nil)
	resp, _ := ts.Client.Do(req)
	return resp
}

// GetWithCookies sends a GET request with cookies to the test server.
func (ts *TestServer) GetWithCookies(path string, cookies []*http.Cookie) *http.Response {
	req, _ := http.NewRequest("GET", ts.BaseURL+path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, _ := ts.Client.Do(req)
	return resp
}

// PostWithCookies sends a POST request with cookies to the test server.
func (ts *TestServer) PostWithCookies(path string, body string, cookies []*http.Cookie) *http.Response {
	req, _ := http.NewRequest("POST", ts.BaseURL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, _ := ts.Client.Do(req)
	return resp
}

// ============ WebSocket Helper Methods ============

// WebSocketDialer creates a websocket dialer configured for the test server.
// For TLS servers, it skips certificate verification.
func (ts *TestServer) WebSocketDialer() *gorillawebsocket.Dialer {
	dialer := &gorillawebsocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	// Skip TLS cert verification for test servers
	if strings.HasPrefix(ts.BaseURL, "https") {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return dialer
}

// DialWebSocket creates a WebSocket connection to the test server.
// Returns the connection, HTTP response, and any error.
func (ts *TestServer) DialWebSocket(path string) (*gorillawebsocket.Conn, *http.Response, error) {
	wsURL := wsURL(ts.BaseURL, path)
	return ts.WebSocketDialer().Dial(wsURL, nil)
}

// DialWebSocketWithHeaders creates a WebSocket connection with custom headers.
func (ts *TestServer) DialWebSocketWithHeaders(path string, headers http.Header) (*gorillawebsocket.Conn, *http.Response, error) {
	wsURL := wsURL(ts.BaseURL, path)
	return ts.WebSocketDialer().Dial(wsURL, headers)
}

// DialWebSocketWithCookie creates a WebSocket connection with a session cookie.
func (ts *TestServer) DialWebSocketWithCookie(path string, cookie *http.Cookie) (*gorillawebsocket.Conn, *http.Response, error) {
	headers := http.Header{}
	headers.Set("Cookie", cookie.Name+"="+cookie.Value)
	wsURL := wsURL(ts.BaseURL, path)
	return ts.WebSocketDialer().Dial(wsURL, headers)
}

// wsURL converts an HTTP(S) URL to a WebSocket URL.
func wsURL(baseURL, path string) string {
	if strings.HasPrefix(baseURL, "https") {
		return "wss" + baseURL[5:] + path
	}
	return "ws" + baseURL[4:] + path
}

// WebSocketMessage represents a message sent or received via WebSocket.
type WebSocketMessage struct {
	Type int
	Data []byte
}

// ReadWebSocketMessages reads all messages from a WebSocket until the connection closes
// or a timeout occurs. Returns all received messages.
func ReadWebSocketMessages(t *testing.T, conn *gorillawebsocket.Conn, timeout time.Duration) []WebSocketMessage {
	t.Helper()

	var messages []WebSocketMessage
	done := make(chan struct{})

	go func() {
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				close(done)
				return
			}
			messages = append(messages, WebSocketMessage{
				Type: messageType,
				Data: data,
			})
		}
	}()

	select {
	case <-done:
		return messages
	case <-time.After(timeout):
		t.Logf("Timeout after %v waiting for WebSocket messages, returning %d messages", timeout, len(messages))
		return messages
	}
}

// ReadSingleWebSocketMessage reads a single message from WebSocket with timeout.
func ReadSingleWebSocketMessage(t *testing.T, conn *gorillawebsocket.Conn, timeout time.Duration) (int, []byte, error) {
	t.Helper()

	type result struct {
		messageType int
		data        []byte
		err         error
	}

	resultChan := make(chan result, 1)

	go func() {
		messageType, data, err := conn.ReadMessage()
		resultChan <- result{messageType, data, err}
	}()

	select {
	case r := <-resultChan:
		return r.messageType, r.data, r.err
	case <-time.After(timeout):
		return 0, nil, &timeoutError{timeout}
	}
}

// timeoutError is returned when WebSocket read times out.
type timeoutError struct {
	timeout time.Duration
}

func (e *timeoutError) Error() string {
	return "websocket read timed out after " + e.timeout.String()
}

func (e *timeoutError) Timeout() bool {
	return true
}

// WriteWebSocketMessage writes a message to the WebSocket connection.
func WriteWebSocketMessage(t *testing.T, conn *gorillawebsocket.Conn, messageType int, data []byte) {
	t.Helper()
	if err := conn.WriteMessage(messageType, data); err != nil {
		t.Fatalf("Failed to write WebSocket message: %v", err)
	}
}

// WriteJSONWebSocketMessage writes a JSON message to the WebSocket connection.
func WriteJSONWebSocketMessage(t *testing.T, conn *gorillawebsocket.Conn, v interface{}) {
	t.Helper()
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("Failed to write JSON WebSocket message: %v", err)
	}
}

// CloseWebSocket closes a WebSocket connection with a graceful close frame.
func CloseWebSocket(t *testing.T, conn *gorillawebsocket.Conn) {
	t.Helper()

	// Send close frame
	message := gorillawebsocket.FormatCloseMessage(gorillawebsocket.CloseNormalClosure, "")
	if err := conn.WriteMessage(gorillawebsocket.CloseMessage, message); err != nil {
		t.Logf("Failed to send close frame: %v", err)
	}

	// Wait for server to close connection
	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, _ = conn.ReadMessage()

	if err := conn.Close(); err != nil {
		t.Logf("Failed to close connection: %v", err)
	}
}

// ============ Cookie Helper Methods ============

// ExtractCookie extracts a cookie by name from an HTTP response.
// Returns nil if the cookie is not found.
func ExtractCookie(resp *http.Response, name string) *http.Cookie {
	if resp == nil {
		return nil
	}
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ExtractSessionCookie extracts the session cookie from a response.
// Returns nil if no session cookie is found.
func ExtractSessionCookie(resp *http.Response) *http.Cookie {
	return ExtractCookie(resp, "session")
}

// ParseCookieHeader parses a Cookie header string and returns a map of cookie names to values.
func ParseCookieHeader(header string) map[string]string {
	cookies := make(map[string]string)
	parts := strings.Split(header, ";")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			cookies[kv[0]] = kv[1]
		}
	}
	return cookies
}

// ============ JSON Helper Methods ============

// ParseJSONBody parses JSON response body into a map.
// Returns empty map if parsing fails.
func ParseJSONBody(resp *http.Response) map[string]interface{} {
	if resp == nil || resp.Body == nil {
		return map[string]interface{}{}
	}
	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body
}

// ParseJSONBodyTo parses JSON response body into the provided interface.
// Returns error if parsing fails.
func ParseJSONBodyTo(resp *http.Response, v interface{}) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// MustParseJSONBody parses JSON response body into a map and fails the test on error.
func MustParseJSONBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatal("Response or body is nil")
		return nil
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to parse JSON body: %v", err)
		return nil
	}
	return body
}

// MustParseJSONBodyTo parses JSON response body into the provided interface and fails the test on error.
func MustParseJSONBodyTo(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatal("Response or body is nil")
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("Failed to parse JSON body: %v", err)
	}
}

// JSONMarshal marshals a value to JSON bytes, failing the test on error.
func JSONMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
		return nil
	}
	return data
}

// JSONMarshalIndent marshals a value to indented JSON bytes.
func JSONMarshalIndent(t *testing.T, v interface{}) []byte {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
		return nil
	}
	return data
}

// ============ Context Helper Methods ============

// TimeoutContext returns a context with the specified timeout.
// Default is 10 seconds if duration is 0.
func TimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

// WithTestTimeout returns a context with a timeout suitable for tests.
func WithTestTimeout(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx, cancel
}

// ============ Mock Cache Implementation ============

// MockCache is a mock cache implementation for testing.
type MockCache struct {
	GetFunc    func(ctx context.Context, key string) (string, error)
	SetFunc    func(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	DeleteFunc func(ctx context.Context, key string) error
	ExistsFunc func(ctx context.Context, key string) (bool, error)
	data       map[string]mockCacheItem
}

type mockCacheItem struct {
	value      interface{}
	expiration time.Time
}

// NewMockCache creates a new mock cache with default in-memory storage.
func NewMockCache() *MockCache {
	return &MockCache{
		data: make(map[string]mockCacheItem),
	}
}

// Get retrieves a value from the cache.
func (m *MockCache) Get(ctx context.Context, key string) (string, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, key)
	}

	item, ok := m.data[key]
	if !ok {
		return "", &MockCacheKeyNotFoundError{Key: key}
	}

	if !item.expiration.IsZero() && time.Now().After(item.expiration) {
		delete(m.data, key)
		return "", &MockCacheKeyNotFoundError{Key: key}
	}

	return item.value.(string), nil
}

// Set stores a value in the cache.
func (m *MockCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if m.SetFunc != nil {
		return m.SetFunc(ctx, key, value, expiration)
	}

	exp := time.Time{}
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}

	m.data[key] = mockCacheItem{
		value:      value,
		expiration: exp,
	}
	return nil
}

// Delete removes a value from the cache.
func (m *MockCache) Delete(ctx context.Context, key string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, key)
	}
	delete(m.data, key)
	return nil
}

// Exists checks if a key exists in the cache.
func (m *MockCache) Exists(ctx context.Context, key string) (bool, error) {
	if m.ExistsFunc != nil {
		return m.ExistsFunc(ctx, key)
	}

	item, ok := m.data[key]
	if !ok {
		return false, nil
	}

	if !item.expiration.IsZero() && time.Now().After(item.expiration) {
		delete(m.data, key)
		return false, nil
	}

	return true, nil
}

// Clear clears all items from the cache.
func (m *MockCache) Clear() {
	m.data = make(map[string]mockCacheItem)
}

// Count returns the number of items in the cache.
func (m *MockCache) Count() int {
	return len(m.data)
}

// MockCacheKeyNotFoundError is returned when a key is not found in the mock cache.
type MockCacheKeyNotFoundError struct {
	Key string
}

func (e *MockCacheKeyNotFoundError) Error() string {
	return "key not found: " + e.Key
}

// ============ Random Data Helpers ============

// RandomString generates a random string of the specified length.
func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}

// ============ Environment Helpers ============

// GetTestEnv retrieves an environment variable for testing.
// Returns empty string if not set.
func GetTestEnv(key string) string {
	return os.Getenv(key)
}

// SetTestEnv sets an environment variable for testing duration.
// The variable is unset when the test completes.
func SetTestEnv(t *testing.T, key, value string) {
	t.Helper()
	originalValue := os.Getenv(key)
	_ = os.Setenv(key, value)
	t.Cleanup(func() {
		if originalValue == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, originalValue)
		}
	})
}

// ============ Logger Helper ============

// NewTestLogger returns a no-op zerolog logger for testing.
func NewTestLogger() *zerolog.Logger {
	logger := zerolog.Nop()
	return &logger
}

// ============ Assert Helpers ============

// AssertStatusCode asserts that the response has the expected status code.
func AssertStatusCode(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Errorf("Expected status %d, got %d", expected, resp.StatusCode)
	}
}

// AssertContentType asserts that the response has the expected content type.
func AssertContentType(t *testing.T, resp *http.Response, expected string) {
	t.Helper()
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, expected) {
		t.Errorf("Expected Content-Type containing %q, got %q", expected, ct)
	}
}

// AssertJSONContentType asserts that the response has JSON content type.
func AssertJSONContentType(t *testing.T, resp *http.Response) {
	t.Helper()
	AssertContentType(t, resp, "application/json")
}
