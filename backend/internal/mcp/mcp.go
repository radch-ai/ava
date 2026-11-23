// Package mcp is the seam between ava's tool catalog and MCP servers: it
// turns a server config into live eino invokables and owns their lifetime.
//
// MCP runs over streamable HTTP only: nothing here spawns a process. The
// model sees {slug}_{remoteName}; the catalog id {slug}:{remoteName} never
// leaves the database; secrets arrive already unsealed and are handed only
// to the transport. A server that is down is an error the caller records —
// never a boot failure and never a retry loop owned by this package.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/radch-ai/ava/backend/internal/security"
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
	// ConnectTimeout caps connection establishment and the TLS
	// handshake at a fixed 10 seconds; any shorter remaining request or
	// invocation budget applies through the context.
	ConnectTimeout = 10 * time.Second
	// DiscoveryTimeout bounds one boot-time discovery pass, including
	// every paginated protocol exchange.
	DiscoveryTimeout = 10 * time.Second
	// closeGrace bounds how long transport Close may block.
	closeGrace = 3 * time.Second
	// maxModelNameLen is OpenAI's stricter tool-name cap.
	maxModelNameLen = 64
	// MaxResponseBytes bounds one logical call's decoded response body,
	// protocol framing included.
	MaxResponseBytes = 2 << 20
	// maxErrorBodyBytes bounds a non-success response body, which mcp-go
	// folds into an error string: the partial buffer must never become a
	// model-visible payload.
	maxErrorBodyBytes = 64 << 10
)

// ServerConfig is one mcp_servers row resolved for connection. Header
// values are already unsealed by the caller; this package never
// touches the vault. The two policy flags are administrator approvals
// recorded on the row — nothing model-supplied reaches them.
type ServerConfig struct {
	ID      string            // slug (immutable)
	URL     string            // streamable HTTP endpoint
	Headers map[string]string // request headers, credentials included
	Timeout time.Duration     // per-call deadline; 0 => DefaultTimeout
	// AllowPrivateNetwork approves loopback/LAN destinations for this
	// endpoint. Link-local and metadata destinations stay denied.
	AllowPrivateNetwork bool
	// AllowPlaintext approves HTTP (not HTTPS) to a loopback/LAN
	// endpoint, credentials included. Public HTTP stays
	// credential-free.
	AllowPlaintext bool
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

// KnownSecrets lists the credential values this server's requests carry,
// in every spelling that can reach a wire or a log.
func (c ServerConfig) KnownSecrets() []string {
	values := make([]string, 0, len(c.Headers))
	for _, v := range c.Headers {
		values = append(values, v)
	}
	return security.CredentialForms(values)
}

// validate requires a usable streamable-HTTP endpoint: no process is ever
// spawned from a stored configuration, and credentials never ride a URL —
// they ride the sealed header channel.
func (c ServerConfig) validate() error {
	u, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("streamable HTTP endpoint must be a valid url: %w", err)
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("streamable HTTP endpoint must be an http(s) url")
	}
	if u.User != nil {
		return errURLCredentials
	}
	return nil
}

// ValidateURL applies validate to one raw endpoint string, trimming
// surrounding whitespace first: the single rule every endpoint input
// passes through, whatever its caller answers on failure.
func ValidateURL(raw string) error {
	return ServerConfig{URL: strings.TrimSpace(raw)}.validate()
}

// errURLCredentials rejects stored endpoints that carry userinfo:
// stdlib would turn it into an unapproved Basic-auth header.
var errURLCredentials = errors.New(
	"credential-bearing URL userinfo is not allowed: pass credentials via sealed headers")

// sameConfig reports whether a cached client already serves cfg. Secret
// maps are compared by value; Timeout compares the effective value and the
// policy flags are part of a connection's identity.
func sameConfig(a, b ServerConfig) bool {
	return a.URL == b.URL &&
		maps.Equal(a.Headers, b.Headers) &&
		a.timeout() == b.timeout() &&
		a.AllowPrivateNetwork == b.AllowPrivateNetwork &&
		a.AllowPlaintext == b.AllowPlaintext
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
// replaces the cached client only after the new dial succeeds; a failed dial
// leaves the old client serving. Dialing runs outside the manager lock: one
// slow handshake must not stall `Get` (the per-turn lookup) or connects for
// other servers. A race between two dials for the same id resolves in Adopt:
// the first installed client wins and the loser is closed.
func (m *Manager) Connect(ctx context.Context, cfg ServerConfig) (*Client, error) {
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
// is replaced here; the replaced client is retired, not aborted, so calls
// already dispatched on it finish on the connection they started on.
// Adopt takes ownership of c.
func (m *Manager) Adopt(cfg ServerConfig, c *Client) *Client {
	c.mgr = m
	m.mu.Lock()
	var replaced *Client
	if existing, ok := m.clients[cfg.ID]; ok {
		if sameConfig(existing.cfg, cfg) {
			m.mu.Unlock()
			_ = c.Close()
			return existing
		}
		replaced = existing
	}
	m.clients[cfg.ID] = c
	m.mu.Unlock()
	// Teardown runs outside the map lock: a slow transport close must
	// not stall another server's lookup or dial.
	if replaced != nil {
		go retire(replaced)
	}
	return c
}

// retire waits for a replaced client's borrowed calls — bounded by the
// connection's own call ceiling, since every dispatch is bounded by it —
// and only then tears the connection down. A refresh therefore never
// aborts work already on the wire. Borrows are refused first: no
// dispatch can start between the drain and the teardown.
func retire(c *Client) {
	c.refuseBorrows()
	c.waitDrained()
	_ = c.Close()
}

// Replace installs c under cfg.ID unconditionally and retires whatever
// client served it. Adopt resolves racing dials by equivalence — it
// refuses to replace an equivalent config — so a caller that knows the
// cached connection is dead (a dropped session) uses this instead.
func (m *Manager) Replace(cfg ServerConfig, c *Client) *Client {
	c.mgr = m
	m.mu.Lock()
	replaced := m.clients[cfg.ID]
	m.clients[cfg.ID] = c
	m.mu.Unlock()
	if replaced != nil {
		go retire(replaced)
	}
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
// Unlike the retirement paths, shutdown owns the abort: teardown is
// immediate, without refuse-then-drain.
func (m *Manager) Close(id string) {
	m.mu.Lock()
	client, ok := m.clients[id]
	delete(m.clients, id)
	m.mu.Unlock()
	if ok {
		_ = client.Close()
	}
}

// CloseAll tears down every cached client (boot shutdown path).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	clients := make([]*Client, 0, len(m.clients))
	for id, client := range m.clients {
		clients = append(clients, client)
		delete(m.clients, id)
	}
	m.mu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
}

// Diagnostic reason codes: static, payload-free, and
// shared by the catalog admission and the server row's diagnostics.
const (
	CodeCatalogInvalidSchema = "catalog_invalid_schema"
	CodeCatalogTooLarge      = "catalog_too_large"
	CodeDestinationDenied    = "destination_denied"
	CodeInsecureCredentials  = "insecure_credentials"
	CodeResponseTooLarge     = "response_too_large"
	CodeTransient            = "transient"
)
