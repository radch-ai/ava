// Package mcptest serves an in-process streamable-HTTP MCP server for
// tests. It is a normal package (not a _test.go) so every package that
// needs a live MCP server — mcp, mcpserver, the API, gateway smoke tests —
// drives the same fixture and the same behaviors.
package mcptest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server is one running fixture endpoint.
type Server struct {
	// URL is the streamable-HTTP endpoint the client dials.
	URL string

	srv     *httptest.Server
	mcp     *mcpserver.MCPServer
	config  config
	mu      sync.Mutex
	headers http.Header
	calls   atomic.Int64
	inits   atomic.Int64
	lists   atomic.Int64
	// firstCursor remembers the first page's cursor for the
	// repeated-cursor forgery.
	firstCursor atomic.Value

	// gate releases a blocked echo call; started counts the calls that
	// reached it.
	gate      chan struct{}
	gateOnce  sync.Once
	echoCalls atomic.Int64
	// deadSession holds the session id a restart forgot: requests on that
	// connection are rejected until a new initialize creates another.
	deadSession atomic.Value // string
	lastSession atomic.Value // string
}

// config is the fixture's behavior: every zero value is a plain server
// with one echo tool.
type config struct {
	toolCount   int
	resultBytes int
	echoSecrets bool
	pageSize    int
	schemaDepth int
	redirect    string
	delay       time.Duration
	statusCode  int
	toolNames   []string
	// listErrorMsg answers tools/list with an HTTP-200 JSON-RPC error.
	listErrorMsg string
	// initErrorMsg answers the initialize handshake with an HTTP-200
	// JSON-RPC error.
	initErrorMsg string
	// listPageDelay holds each tools/list request open.
	listPageDelay time.Duration
	echoDesc      string
	echoSchema    json.RawMessage
	// echoAnnotations replaces the echo tool's annotations.
	echoAnnotations *mcpsdk.ToolAnnotation
	// forgeRepeatedCursor re-issues the first page's cursor forever.
	forgeRepeatedCursor bool
	described           [2]int
	// blockEcho holds every echo call open until ReleaseEcho runs: a
	// call that is provably mid-flight while the test acts.
	blockEcho bool
}

// Option customizes the fixture.
type Option func(*config)

// WithToolCount registers n trivial tools ("tool-0"…), for catalog caps.
func WithToolCount(n int) Option {
	return func(c *config) { c.toolCount = n }
}

// WithToolResult adds a "big" tool returning n bytes of plain text, for
// the decoded-response ceiling.
func WithToolResult(n int) Option {
	return func(c *config) { c.resultBytes = n }
}

// WithEchoSecrets adds an "echo-secrets" tool that reflects the request
// headers it received — the reflected-credential leak path.
func WithEchoSecrets() Option {
	return func(c *config) { c.echoSecrets = true }
}

// WithPageSize paginates the tool listing.
func WithPageSize(n int) Option {
	return func(c *config) { c.pageSize = n }
}

// WithSchemaDepth registers a "deep" tool whose input schema nests that
// many arrays, for the schema-complexity bounds.
func WithSchemaDepth(depth int) Option {
	return func(c *config) { c.schemaDepth = depth }
}

// WithRedirect answers every request with a redirect to target, for the
// redirect-denial rule.
func WithRedirect(target string) Option {
	return func(c *config) { c.redirect = target }
}

// WithDelay holds every request open for d, for deadline tests.
func WithDelay(d time.Duration) Option {
	return func(c *config) { c.delay = d }
}

// WithToolNames registers extra tools under the given (arbitrary) names,
// for name-contract and collision cases.
func WithToolNames(names ...string) Option {
	return func(c *config) { c.toolNames = append(c.toolNames, names...) }
}

// WithListError answers every tools/list request with an HTTP-200
// JSON-RPC error carrying message — a hostile protocol-error path, not a
// transport failure.
func WithListError(message string) Option {
	return func(c *config) { c.listErrorMsg = message }
}

// WithListPageDelay holds each tools/list request open for d: slow later
// pages, for the listing budget case.
func WithListPageDelay(d time.Duration) Option {
	return func(c *config) { c.listPageDelay = d }
}

// WithEchoDescription overrides the echo tool's description — the
// model-visible instruction a definition carries.
func WithEchoDescription(text string) Option {
	return func(c *config) { c.echoDesc = text }
}

// WithEchoSchema overrides the echo tool's raw input schema, for the
// canonicalization cases (key order, structural change).
func WithEchoSchema(schema string) Option {
	return func(c *config) { c.echoSchema = json.RawMessage(schema) }
}

// WithDescribedTools registers count tools whose descriptions are each
// size bytes — the advertised-definition budget case.
func WithDescribedTools(count, size int) Option {
	return func(c *config) { c.described = [2]int{count, size} }
}

// WithStatus answers every request with code, for transport-error tests.
func WithStatus(code int) Option {
	return func(c *config) { c.statusCode = code }
}

// WithInitializeError answers the initialize handshake with an HTTP-200
// JSON-RPC error carrying message — a hostile handshake whose text must
// be scrubbed before it reaches an API answer or a log.
func WithInitializeError(message string) Option {
	return func(c *config) { c.initErrorMsg = message }
}

// WithEchoAnnotations overrides the echo tool's annotations — the
// model-visible metadata a definition carries.
func WithEchoAnnotations(annotation mcpsdk.ToolAnnotation) Option {
	return func(c *config) { c.echoAnnotations = &annotation }
}

// WithRepeatedCursor forges the listing's pagination into a cycle: the
// first page's cursor is re-issued forever, the client's repeated-cursor
// rejection path.
func WithRepeatedCursor() Option {
	return func(c *config) { c.forgeRepeatedCursor = true }
}

// WithBlockingEcho holds every echo call open until the test calls
// ReleaseEcho: discovery stays fast, so a refresh can run while one call
// is provably mid-flight.
func WithBlockingEcho() Option {
	return func(c *config) { c.blockEcho = true }
}

// ExpireSession makes the current connection's session dead, the way a
// restarted server forgets it: requests carrying that id are rejected
// until a fresh initialize creates another session.
func (s *Server) ExpireSession() {
	if sid, ok := s.lastSession.Load().(string); ok && sid != "" {
		s.deadSession.Store(sid)
	}
}

// Start runs the fixture until the test ends.
func Start(tb testing.TB, opts ...Option) *Server {
	tb.Helper()
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	server := &Server{config: cfg, gate: make(chan struct{})}
	mcpServer := mcpserver.NewMCPServer("ava-test-helper", "1.0.0")
	if cfg.pageSize > 0 {
		mcpServer = mcpserver.NewMCPServer("ava-test-helper", "1.0.0",
			mcpserver.WithPaginationLimit(cfg.pageSize))
	}
	server.mcp = mcpServer
	registerTools(mcpServer, server)
	handler := mcpserver.NewStreamableHTTPServer(mcpServer)
	server.srv = httptest.NewServer(http.HandlerFunc(server.serve(handler)))
	tb.Cleanup(server.srv.Close)
	server.URL = server.srv.URL
	return server
}

// serve applies the fixture's behaviors, records the request headers a
// tool may reflect, then delegates to the MCP server.
func (s *Server) serve(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
			s.lastSession.Store(sid)
		}
		s.mu.Lock()
		s.headers = r.Header.Clone()
		s.mu.Unlock()
		if s.config.delay > 0 {
			select {
			case <-time.After(s.config.delay):
			case <-r.Context().Done():
				return
			}
		}
		if s.config.redirect != "" {
			http.Redirect(w, r, s.config.redirect, http.StatusTemporaryRedirect)
			return
		}
		var listRequest bool
		if body, err := io.ReadAll(r.Body); err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			listRequest = bytes.Contains(body, []byte(`"tools/list"`))
			if listRequest {
				s.lists.Add(1)
				dead, _ := s.deadSession.Load().(string)
				sessionID := r.Header.Get("Mcp-Session-Id")
				if sessionID == "" {
					sessionID = r.Header.Get("mcp-session-id")
				}
				if dead != "" && sessionID == dead {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Invalid session ID"},"id":null}`))
					return
				}
			}
			if bytes.Contains(body, []byte(`"initialize"`)) {
				s.inits.Add(1)
				if s.config.initErrorMsg != "" {
					var req struct {
						ID json.RawMessage `json:"id"`
					}
					_ = json.Unmarshal(body, &req)
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":%s}}`,
						req.ID, mustJSON(s.config.initErrorMsg))
					return
				}
			}
			if listRequest && s.config.listErrorMsg != "" {
				var req struct {
					ID json.RawMessage `json:"id"`
				}
				_ = json.Unmarshal(body, &req)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":%s}}`,
					req.ID, mustJSON(s.config.listErrorMsg))
				return
			}
		}
		if listRequest && s.config.listPageDelay > 0 {
			select {
			case <-time.After(s.config.listPageDelay):
			case <-r.Context().Done():
				return
			}
		}
		if s.config.statusCode != 0 {
			// Reflect the request's credential and a pattern-shaped key:
			// a hostile server's error body, which must be scrubbed
			// before mcp-go can fold it into a message.
			s.mu.Lock()
			auth := s.headers.Get("Authorization")
			s.mu.Unlock()
			w.WriteHeader(s.config.statusCode)
			_, _ = fmt.Fprintf(w, "upstream failure; authorization=%s mock=sk-abcdefghijklmnop1234", auth)
			return
		}
		if listRequest && s.config.forgeRepeatedCursor {
			s.serveForgedList(w, r, next)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// mustJSON renders one string as a JSON literal.
func mustJSON(v string) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

// serveForgedList answers tools/list through the real MCP server, then
// forges the pagination cursor of the JSON response: the first page's
// cursor re-issued forever (a repeated-cursor cycle), or a fresh cursor
// on every page (endless pagination). A non-JSON answer passes through
// untouched.
func (s *Server) serveForgedList(w http.ResponseWriter, r *http.Request, next http.Handler) {
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, r)
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		copyHeader(w.Header(), rec.Header())
		w.WriteHeader(rec.Code)
		_, _ = rec.Body.WriteTo(w)
		return
	}
	var doc map[string]any
	body := rec.Body.Bytes()
	if err := json.Unmarshal(body, &doc); err != nil {
		copyHeader(w.Header(), rec.Header())
		w.WriteHeader(rec.Code)
		_, _ = w.Write(body)
		return
	}
	result, _ := doc["result"].(map[string]any)
	if result != nil {
		switch {
		case s.config.forgeRepeatedCursor:
			if stored, ok := s.firstCursor.Load().(string); ok && stored != "" {
				result["nextCursor"] = stored
			} else if cursor, _ := result["nextCursor"].(string); cursor != "" {
				s.firstCursor.Store(cursor)
			}
		}
	}
	rewritten, err := json.Marshal(doc)
	if err != nil {
		copyHeader(w.Header(), rec.Header())
		w.WriteHeader(rec.Code)
		_, _ = w.Write(body)
		return
	}
	copyHeader(w.Header(), rec.Header())
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rewritten)
}

// copyHeader copies every response header value to dst.
func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// SetEcho re-registers the echo tool with a new description and/or raw
// input schema, so one endpoint can advertise a changed (or merely
// reordered) definition between two discovery passes.
func (s *Server) SetEcho(description, schemaJSON string) {
	s.mu.Lock()
	s.config.echoDesc = description
	s.config.echoSchema = json.RawMessage(schemaJSON)
	s.mu.Unlock()
	s.registerEcho()
}

// registerEcho installs the current echo definition, replacing any
// previous one under the same name.
func (s *Server) registerEcho() {
	s.mu.Lock()
	description, schema := s.config.echoDesc, s.config.echoSchema
	annotations := s.config.echoAnnotations
	s.mu.Unlock()
	echo := echoTool()
	if description != "" {
		echo.Description = description
	}
	if len(schema) > 0 {
		echo = mcpsdk.NewToolWithRawSchema("echo", echo.Description, schema)
	}
	if annotations != nil {
		echo.Annotations = *annotations
	}
	s.mcp.AddTool(echo, s.echoHandler)
}

// echoHandler answers an echo call, holding it while the fixture is in
// blocking mode.
func (s *Server) echoHandler(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	s.echoCalls.Add(1)
	if s.config.blockEcho {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return echoHandler(ctx, req)
}

// ReleaseEcho lets every blocked echo call proceed. Idempotent.
func (s *Server) ReleaseEcho() { s.gateOnce.Do(func() { close(s.gate) }) }

// EchoCalls reports how many echo calls reached the fixture.
func (s *Server) EchoCalls() int64 { return s.echoCalls.Load() }

// Close stops the fixture: later requests are refused. Idempotent.
func (s *Server) Close() { s.srv.Close() }

// Calls reports how many HTTP requests the fixture served.
func (s *Server) Calls() int64 { return s.calls.Load() }

// Initializes reports how many MCP sessions were initialized: the dial
// count, since each dial initializes once.
func (s *Server) Initializes() int64 { return s.inits.Load() }

// Lists reports how many tools/list requests the fixture answered.
func (s *Server) Lists() int64 { return s.lists.Load() }

// Header returns the last value of one request header the fixture saw.
func (s *Server) Header(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.headers == nil {
		return s.headers.Get(key)
	}
	return s.headers.Get(key)
}

// registerTools installs the fixture's tool set.
func registerTools(server *mcpserver.MCPServer, s *Server) {
	s.registerEcho()
	for i := range s.config.toolCount {
		name := fmt.Sprintf("tool-%d", i)
		server.AddTool(mcpsdk.NewTool(name, mcpsdk.WithDescription("Fixture tool "+name)), echoHandler)
	}
	for _, name := range s.config.toolNames {
		server.AddTool(mcpsdk.NewTool(name, mcpsdk.WithDescription("Fixture tool "+name)), echoHandler)
	}
	if s.config.resultBytes > 0 {
		server.AddTool(
			mcpsdk.NewTool("big", mcpsdk.WithDescription("Returns a large text result.")),
			bigHandler(s.config.resultBytes))
	}
	if s.config.echoSecrets {
		server.AddTool(
			mcpsdk.NewTool("echo-secrets", mcpsdk.WithDescription("Reflects the request headers it received.")),
			func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				var b strings.Builder
				s.mu.Lock()
				for key, values := range s.headers {
					for _, value := range values {
						fmt.Fprintf(&b, "%s: %s\n", key, value)
					}
				}
				s.mu.Unlock()
				return mcpsdk.NewToolResultText(b.String()), nil
			})
	}
	for i := range s.config.described[0] {
		name := fmt.Sprintf("described-%d", i)
		server.AddTool(mcpsdk.NewTool(name,
			mcpsdk.WithDescription(name+" "+strings.Repeat("d", s.config.described[1]))), echoHandler)
	}
	if s.config.schemaDepth > 0 {
		server.AddTool(mcpsdk.NewToolWithRawSchema(
			"deep", "Carries a deeply nested input schema.", deepSchema(s.config.schemaDepth)), echoHandler)
	}
}

// echoTool is the fixture's baseline tool.
func echoTool() mcpsdk.Tool {
	return mcpsdk.NewTool("echo",
		mcpsdk.WithDescription("Echoes its input text."),
		mcpsdk.WithString("text", mcpsdk.Required()))
}

// echoHandler answers an echo call.
func echoHandler(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	text, _ := req.GetArguments()["text"].(string)
	return mcpsdk.NewToolResultText(text), nil
}

// bigHandler answers an oversized-result call.
func bigHandler(size int) mcpserver.ToolHandlerFunc {
	return func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return mcpsdk.NewToolResultText(strings.Repeat("x", size)), nil
	}
}

// deepSchema builds an input schema nested depth arrays deep.
func deepSchema(depth int) json.RawMessage {
	schema := `{"type":"string"}`
	for range depth {
		schema = `{"type":"array","items":` + schema + `}`
	}
	return json.RawMessage(schema)
}
