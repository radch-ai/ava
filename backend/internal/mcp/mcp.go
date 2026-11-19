// Package mcp is the seam between ava's tool catalog and MCP servers: it
// turns a server config into live eino invokables and owns their lifetime.
//
// The model sees {slug}_{remoteName}; the catalog id {slug}:{remoteName}
// never leaves the database; secrets arrive already unsealed and are
// handed only to the transport; a server that is down is an error the
// caller records — never a boot failure and never a retry loop owned by
// this package.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

const (
	// DefaultTimeout bounds one tool call when the server config has none.
	DefaultTimeout = 30 * time.Second
	// VerifyTimeout bounds install-time connection checks — create,
	// patch re-verify, refresh and enable-reconnect. Those run inside an
	// API request whose http.Server WriteTimeout is 15s, so the check
	// must answer before the connection dies; runtime tool calls still
	// honor the configured timeout.
	VerifyTimeout = 10 * time.Second
	// closeGrace bounds how long transport Close may block before the
	// process group is killed. mcp-go's stdio Close closes pipes and then
	// waits, which a server ignoring stdin EOF never ends (stdio.go:208).
	closeGrace = 3 * time.Second
	// maxModelNameLen is OpenAI's stricter tool-name cap.
	maxModelNameLen = 64
)

// Transports accepted for v1 (SSE is legacy and rejected).
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

// ServerConfig is one mcp_servers row resolved for connection. Env and
// Header values are already unsealed by the caller; this package never
// touches the vault.
type ServerConfig struct {
	ID        string            // slug (immutable)
	Transport string            // TransportStdio | TransportHTTP
	Command   string            // stdio: executable
	Args      []string          // stdio: arguments
	URL       string            // http: streamable HTTP endpoint
	Env       map[string]string // stdio: merged over the process env
	Headers   map[string]string // http: request headers
	Timeout   time.Duration     // per-call deadline; 0 => DefaultTimeout
}

// CatalogID is the DB-only catalog id: "{slug}:{remoteName}".
func CatalogID(slug, remote string) string { return slug + ":" + remote }

// ModelName is the eino-visible tool name: "{slug}_{remoteName}".
// Providers ban the colon, so the composite model name uses an underscore.
func ModelName(slug, remote string) string { return slug + "_" + remote }

func (c ServerConfig) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

func (c ServerConfig) validate() error {
	switch c.Transport {
	case TransportStdio:
		if c.Command == "" {
			return errors.New("stdio transport needs a command")
		}
	case TransportHTTP:
		if c.URL == "" {
			return errors.New("http transport needs a url")
		}
	default:
		return fmt.Errorf("unsupported transport %q", c.Transport)
	}
	return nil
}

// sameConfig reports whether a cached client already serves cfg. Args and
// secret maps are compared by value; Timeout compares the effective value.
func sameConfig(a, b ServerConfig) bool {
	return a.Transport == b.Transport &&
		a.Command == b.Command &&
		a.URL == b.URL &&
		slices.Equal(a.Args, b.Args) &&
		maps.Equal(a.Env, b.Env) &&
		maps.Equal(a.Headers, b.Headers) &&
		a.timeout() == b.timeout()
}

// Manager owns the live clients, one per server id.
type Manager struct {
	mu      sync.Mutex
	clients map[string]*Client
}

// NewManager returns an empty manager.
func NewManager() *Manager {
	return &Manager{clients: make(map[string]*Client)}
}

// BoundedDial dials under the install-time budget: min(ctx deadline,
// cfg timeout, VerifyTimeout). Create, patch re-verify, refresh and
// enable-reconnect go through it so a slow or dead server answers the
// request instead of outliving the http.Server write deadline. Cancelling
// the returned client's context is safe: the transport lifetime is
// manager-owned, not derived from the dial context.
func BoundedDial(ctx context.Context, cfg ServerConfig) (*Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, min(cfg.timeout(), VerifyTimeout))
	defer cancel()
	return Dial(dialCtx, cfg)
}

// Connect returns the live client for cfg.ID, dialing when needed.
//
// A config that differs from the cached one (any connection-affecting field)
// replaces the cached client — process group included — only after the new
// dial succeeds; a failed dial leaves the old client serving. Dialing runs
// outside the manager lock: one slow handshake must not stall `Get` (the
// per-turn lookup) or connects for other servers. A race between two dials
// for the same id resolves in Adopt: the first installed client wins and
// the loser is closed.
func (m *Manager) Connect(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("mcp server %s: %w", cfg.ID, err)
	}
	if cached, ok := m.Get(cfg.ID); ok && sameConfig(cached.cfg, cfg) {
		return cached, nil
	}
	client, err := Dial(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("mcp server %s: %w", cfg.ID, err)
	}
	return m.Adopt(cfg, client), nil
}

// Adopt installs an already-connected client under cfg.ID and returns
// the client now live for it: the given one, unless a concurrent caller
// installed an equivalent config first — then the given client is closed
// and the existing one returned. A cached client for a different config
// is replaced (closed) here. Adopt takes ownership of c.
func (m *Manager) Adopt(cfg ServerConfig, c *Client) *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.clients[cfg.ID]; ok {
		if sameConfig(existing.cfg, cfg) {
			_ = c.Close()
			return existing
		}
		_ = existing.Close()
	}
	m.clients[cfg.ID] = c
	return c
}

// Get returns the cached client for id without dialing.
func (m *Manager) Get(id string) (*Client, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	client, ok := m.clients[id]
	return client, ok
}

// Close drops and tears down the client for id. Unknown ids are a no-op.
func (m *Manager) Close(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client, ok := m.clients[id]; ok {
		_ = client.Close()
		delete(m.clients, id)
	}
}

// CloseAll tears down every cached client (boot shutdown path).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, client := range m.clients {
		_ = client.Close()
		delete(m.clients, id)
	}
}
