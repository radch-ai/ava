package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/radch-ai/ava/backend/internal/security"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// mcpListRequestBase offsets the discovery listing's request ids into a
// range mcp-go's own counter never reaches.
const mcpListRequestBase = 1 << 32

// errClientClosed refuses a dispatch on a connection that is already
// torn down: a live one is resolved from the manager, never resurrected.
var errClientClosed = errors.New("mcp server connection is closed")

// Client is one live MCP server connection.
type Client struct {
	id      string
	cfg     ServerConfig
	timeout time.Duration
	tr      transport.Interface
	cli     *client.Client
	cancel  context.CancelFunc
	// mgr is the manager that installed this client (nil until Adopt):
	// tools resolve their live connection through it instead of pinning
	// the client they were listed on.
	mgr *Manager

	// calls counts borrowed dispatches so a replaced client is closed only
	// after the work already on its wire returns. closed refuses new
	// borrows once teardown has begun.
	callsMu sync.Mutex
	calls   int
	closed  bool
	drained chan struct{}

	// scrub redacts this server's known credential values plus the
	// pattern battery from everything model- or log-visible.
	scrub *security.Scrubber

	// cache is the last committed generation's wrapped tools keyed by
	// remote name — the per-turn lookup path, no network.
	mu    sync.Mutex
	cache map[string]tool.InvokableTool

	// syncMu serializes staged publication: one generation is staged,
	// published by the caller's write, then committed or discarded, so a
	// replacement implementation is never live under an older approved
	// definition.
	syncMu         sync.Mutex
	staged         map[string]tool.InvokableTool
	stagedListing  []ListedTool
	stagedSkipped  []Skipped
	stagedComplete bool

	// listIDs numbers the discovery listing's own JSON-RPC requests.
	listIDs atomic.Uint64

	closeOnce sync.Once
	closeErr  error
}

// Dial builds the transport for cfg and starts a client without caching
// it: the caller owns the result until Manager.Adopt (or Close).
func Dial(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("mcp server %s: %w", cfg.ID, err)
	}
	tr, err := newTransport(cfg)
	if err != nil {
		return nil, err
	}
	return newClient(ctx, cfg, tr)
}

// newTransport builds the streamable-HTTP transport over the hardened
// client: no proxy, no redirects, pinned dial, bounded body, scrubbed
// protocol logging.
func newTransport(cfg ServerConfig) (transport.Interface, error) {
	opts := []transport.StreamableHTTPCOption{
		transport.WithHTTPBasicClient(newHTTPClient(cfg)),
		transport.WithHTTPLogger(scrubLogger{scrub: security.NewScrubber(cfg.KnownSecrets())}),
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
	}
	tr, err := transport.NewStreamableHTTP(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("streamable HTTP connection: %w", err)
	}
	return tr, nil
}

// newClient starts a client over an already-built transport and completes
// the MCP initialize handshake. The transport's lifetime context is
// manager-owned (not the caller's request context).
func newClient(ctx context.Context, cfg ServerConfig, tr transport.Interface) (*Client, error) {
	lifetime, cancel := context.WithCancel(context.Background())
	c := &Client{
		id:      cfg.ID,
		cfg:     cfg,
		timeout: cfg.timeout(),
		tr:      tr,
		cli:     client.NewClient(tr),
		cancel:  cancel,
		scrub:   security.NewScrubber(cfg.KnownSecrets()),
	}
	if err := c.cli.Start(lifetime); err != nil {
		_ = c.teardown()
		return nil, c.wrapStartupError("start", err)
	}

	// The handshake is the only network-shaped step before use: bound it by
	// both the caller's deadline and the per-call timeout.
	initCtx, initCancel := context.WithTimeout(ctx, c.timeout)
	defer initCancel()
	if _, err := c.cli.Initialize(initCtx, mcpsdk.InitializeRequest{
		Params: mcpsdk.InitializeParams{
			ProtocolVersion: mcpsdk.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcpsdk.Implementation{Name: "ava", Version: "dev"},
		},
	}); err != nil {
		_ = c.teardown()
		return nil, c.wrapStartupError("initialize", err)
	}
	return c, nil
}

// wrapStartupError scrubs server-controlled startup text with this
// server's known values while keeping the host's own destination
// and credential denials identifiable: those carry no server text, so
// their error chains survive for errors.Is callers.
func (c *Client) wrapStartupError(stage string, err error) error {
	if errors.Is(err, ErrDestinationDenied) || errors.Is(err, ErrLinkLocalDenied) ||
		errors.Is(err, security.ErrUnspecifiedDenied) || errors.Is(err, ErrInsecureCredentials) {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return fmt.Errorf("%s: %s", stage, c.scrubText(err))
}

// Catalog bounds: one discovery pass admits at most this many
// pages, tools and decoded catalog bytes, and one definition this many
// bytes. Exceeding any of them rejects the generation.
const (
	MaxCatalogPages    = 50
	MaxCatalogTools    = 1024
	MaxCatalogBytes    = 2 << 20
	MaxDefinitionBytes = 64 << 10
)

// errRepeatedCursor marks a server that hands back the same cursor
// forever: a malformed listing, never a stream to keep reading.
var errRepeatedCursor = errors.New("listing repeated its pagination cursor")

// Skipped is one tool a discovery pass left out for a cosmetic reason —
// a name the provider wire contract cannot carry. The entry
// is visible as a count/reason, never silently renamed or truncated.
type Skipped struct {
	Remote string
	Reason string
}

// ListedTool is one of a server's tools after discovery: everything the
// catalog stores and every consumer needs — the identity and the
// sanitized definition.
type ListedTool struct {
	Remote      string
	Description string
	// DefinitionJSON is the stored, sanitized definition the UI renders.
	DefinitionJSON string
}

// rawTool is one entry of a raw tools/list page: the schema and
// annotation bytes exactly as the server advertised them. mcp-go's typed
// Tool drops schema keywords it does not model ($ref, items, anyOf …), so
// the listing is parsed here — the stored definition must be the
// advertised one, not a lossy re-encoding.
type rawTool struct {
	Name         string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	Annotations  json.RawMessage
}

// listingPage is one decoded tools/list response.
type listingPage struct {
	Tools []struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		InputSchema  json.RawMessage `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema"`
		Annotations  json.RawMessage `json:"annotations"`
	} `json:"tools"`
	NextCursor string `json:"nextCursor"`
}

// listTools reads the server's complete listing under the catalog
// bounds: pages, tools and cumulative bytes are counted as they arrive,
// so an oversized catalog aborts before it is accumulated whole.
func (c *Client) listTools(ctx context.Context) ([]rawTool, error) {
	var (
		out    []rawTool
		cursor string
		bytes  int
		seen   = make(map[string]struct{})
	)
	for page := 1; ; page++ {
		if page > MaxCatalogPages {
			return nil, fmt.Errorf("%w: more than %d pages", errCatalogTooLarge, MaxCatalogPages)
		}
		res, err := c.listPage(ctx, cursor)
		if err != nil {
			return nil, err
		}
		for _, tool := range res.Tools {
			encoded, err := json.Marshal(tool)
			if err != nil {
				return nil, fmt.Errorf("encode tool %q: %w", tool.Name, err)
			}
			bytes += len(encoded)
			if bytes > MaxCatalogBytes {
				return nil, fmt.Errorf("%w: more than %d bytes", errCatalogTooLarge, MaxCatalogBytes)
			}
			out = append(out, rawTool{
				Name:         tool.Name,
				Description:  tool.Description,
				InputSchema:  tool.InputSchema,
				OutputSchema: tool.OutputSchema,
				Annotations:  tool.Annotations,
			})
			if len(out) > MaxCatalogTools {
				return nil, fmt.Errorf("%w: more than %d tools", errCatalogTooLarge, MaxCatalogTools)
			}
		}
		if res.NextCursor == "" {
			return out, nil
		}
		if _, repeat := seen[res.NextCursor]; repeat {
			return nil, errRepeatedCursor
		}
		seen[res.NextCursor] = struct{}{}
		cursor = res.NextCursor
	}
}

// listPage issues one raw tools/list request over the live transport.
// The request id comes from a range mcp-go's own counter never reaches,
// so the two cannot pair a response wrongly.
func (c *Client) listPage(ctx context.Context, cursor string) (*listingPage, error) {
	params := map[string]any{}
	if cursor != "" {
		params["cursor"] = cursor
	}
	resp, err := c.tr.SendRequest(ctx, transport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcpsdk.NewRequestId(mcpListRequestBase + c.listIDs.Add(1)),
		Method:  "tools/list",
		Params:  params,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		// An HTTP-200 JSON-RPC error is server-controlled text: it is
		// scrubbed before it can become a diagnostic or an API answer.
		message, _ := c.scrub.ScrubString(resp.Error.Message)
		return nil, fmt.Errorf("tools/list: %s", message)
	}
	var page listingPage
	if err := json.Unmarshal(resp.Result, &page); err != nil {
		return nil, fmt.Errorf("decode tools/list: %w", err)
	}
	return &page, nil
}

// CatalogError is a rejected discovery generation: a security-invalid or
// over-limit catalog pauses the server's new calls until a valid refresh
// succeeds. Code is a static reason code, safe to store and
// show.
type CatalogError struct {
	Code string
	Err  error
}

// Error renders the code plus the underlying cause.
func (e *CatalogError) Error() string { return e.Code + ": " + e.Err.Error() }

// Unwrap exposes the cause to errors.Is/As (the bounds sentinels).
func (e *CatalogError) Unwrap() error { return e.Err }

// errCatalogTooLarge is the shared cause of the page/tool/byte bounds;
// the CatalogError code names which bound tripped.
var errCatalogTooLarge = errors.New("catalog exceeds its bounds")

// buildListing turns one raw listing into catalog entries under the
// validation rules, or rejects the whole generation. A tool whose name the
// provider cannot carry is skipped with a reason; a tool whose schema,
// definition or identity is unsafe rejects the generation.
func (c *Client) buildListing(raw []rawTool) ([]ListedTool, []Skipped, error) {
	scrub := c.scrub
	listed := make([]ListedTool, 0, len(raw))
	skipped := make([]Skipped, 0)
	seen := make(map[string]struct{}, len(raw))
	for _, tool := range raw {
		if _, dup := seen[tool.Name]; dup {
			return nil, nil, &CatalogError{
				Code: CodeCatalogInvalidSchema,
				Err:  fmt.Errorf("duplicate tool name %q in one listing", tool.Name),
			}
		}
		seen[tool.Name] = struct{}{}

		if reason := c.nameProblem(tool.Name); reason != "" {
			skipped = append(skipped, Skipped{Remote: tool.Name, Reason: reason})
			continue
		}

		description, _ := scrub.ScrubString(tool.Description)
		inputSchema, err := schemaValue(tool.InputSchema)
		if err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
				Err: fmt.Errorf("tool %q input schema: %w", tool.Name, err)}
		}
		outputSchema, err := schemaValue(tool.OutputSchema)
		if err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
				Err: fmt.Errorf("tool %q output schema: %w", tool.Name, err)}
		}
		if err := security.ValidateSchema(inputSchema); err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
				Err: fmt.Errorf("tool %q input schema: %w", tool.Name, err)}
		}
		if err := security.ValidateSchema(outputSchema); err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
				Err: fmt.Errorf("tool %q output schema: %w", tool.Name, err)}
		}
		// A definition is authority, not a copy — credential material
		// reflected into a schema or annotation tree rejects the whole
		// generation instead of being rewritten under the server's name.
		for _, tree := range []struct {
			what  string
			value any
		}{
			{"input schema", inputSchema},
			{"output schema", outputSchema},
		} {
			if len(scrub.ScrubFindingsIn(tree.value)) > 0 {
				return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
					Err: fmt.Errorf("tool %q %s contains credential material", tool.Name, tree.what)}
			}
		}

		definition := map[string]any{
			"name":        tool.Name,
			"description": description,
			"inputSchema": inputSchema,
		}
		if outputSchema != nil {
			definition["outputSchema"] = outputSchema
		}
		annotations, err := annotationsValue(tool.Annotations)
		if err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
				Err: fmt.Errorf("tool %q: malformed annotations: %w", tool.Name, err)}
		}
		if annotations != nil {
			if len(scrub.ScrubFindingsIn(annotations)) > 0 {
				return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
					Err: fmt.Errorf("tool %q annotations contain credential material", tool.Name)}
			}
			definition["annotations"] = annotations
		}
		definitionJSON, err := security.CanonicalJSON(definition)
		if err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema,
				Err: fmt.Errorf("tool %q definition: %w", tool.Name, err)}
		}
		if len(definitionJSON) > MaxDefinitionBytes {
			return nil, nil, &CatalogError{Code: CodeCatalogTooLarge,
				Err: fmt.Errorf("tool %q definition is %d bytes, over %d",
					tool.Name, len(definitionJSON), MaxDefinitionBytes)}
		}

		listed = append(listed, ListedTool{
			Remote:         tool.Name,
			Description:    description,
			DefinitionJSON: string(definitionJSON),
		})
	}
	return listed, skipped, nil
}

// nameProblem reports why a remote name cannot be a model tool: the
// composite name's charset and length. An empty string means OK.
func (c *Client) nameProblem(remote string) string {
	if remote == "" {
		return "tool has no name"
	}
	model := ModelName(c.id, remote)
	if len(model) > maxModelNameLen {
		return fmt.Sprintf("model name %q exceeds %d characters", model, maxModelNameLen)
	}
	for _, r := range model {
		ok := r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return fmt.Sprintf("model name %q contains %q, outside [a-zA-Z0-9_-]", model, r)
		}
	}
	return ""
}

// schemaValue decodes advertised schema bytes into a plain JSON value.
// An absent schema is the empty object: a tool that takes no arguments.
func schemaValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	return decodeJSONValue(raw)
}

// annotationsValue decodes advertised annotations into a plain JSON value,
// or nil when the server declared none. Malformed or non-object
// annotations are an error: annotation data is authority-relevant (a
// read-only or destructive hint changes what the model is told), so it is
// never silently discarded.
func annotationsValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	value, err := decodeJSONValue(raw)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("annotations must be a JSON object")
	}
	if len(object) == 0 {
		return nil, nil
	}
	return value, nil
}

// decodeJSONValue decodes JSON into any for canonicalization. Numbers keep
// their literal form (json.Number), so a definition's numeric tokens — a
// schema bound, a default — survive decoding, fingerprinting and storage
// exactly as advertised.
func decodeJSONValue(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// errUnrepresentable marks a tool whose composite model name violates
// the provider wire contract: discovery skips it with a reason —
// cosmetic, never a rename — so it never reaches the runtime.
var errUnrepresentable = errors.New("unrepresentable model tool name")

// wrap adapts one listed tool under the composite model name: Info
// reports `{slug}_{remote}`, and the invokable carries the catalog
// identity it was discovered under.
func (c *Client) wrap(base tool.BaseTool, entry ListedTool) (tool.InvokableTool, error) {
	if reason := c.nameProblem(entry.Remote); reason != "" {
		return nil, fmt.Errorf("%w: mcp server %s: %s", errUnrepresentable, c.id, reason)
	}
	return &wrappedTool{
		id:     c.id,
		remote: entry.Remote,
		base:   base,
		model:  ModelName(c.id, entry.Remote),
	}, nil
}

// Config reports the connection config this client was built from.
func (c *Client) Config() ServerConfig { return c.cfg }

// SyncStaged reads the bounded listing, validates and fingerprints it, and
// wraps the representable tools into a staged generation — without touching
// the live cache. A security-invalid or over-limit listing fails the whole
// generation and leaves the live one serving.
func (c *Client) SyncStaged(ctx context.Context) ([]ListedTool, []Skipped, error) {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	return c.stage(ctx)
}

// stage is SyncStaged's body under syncMu.
func (c *Client) stage(ctx context.Context) ([]ListedTool, []Skipped, error) {
	raw, err := c.listTools(ctx)
	if err != nil {
		if errors.Is(err, errCatalogTooLarge) {
			return nil, nil, &CatalogError{Code: CodeCatalogTooLarge, Err: err}
		}
		if errors.Is(err, errRepeatedCursor) {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema, Err: err}
		}
		// A transport-level failure can carry server text too.
		return nil, nil, fmt.Errorf("mcp server %s: list tools: %s", c.id, c.scrubText(err))
	}
	listed, skipped, err := c.buildListing(raw)
	if err != nil {
		return nil, nil, err
	}

	byName := make(map[string]rawTool, len(raw))
	for _, tool := range raw {
		byName[tool.Name] = tool
	}
	wrapped := make(map[string]tool.InvokableTool, len(listed))
	for _, entry := range listed {
		base, err := newRemoteTool(c, byName[entry.Remote], entry.Description)
		if err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema, Err: err}
		}
		impl, err := c.wrap(base, entry)
		if err != nil {
			return nil, nil, &CatalogError{Code: CodeCatalogInvalidSchema, Err: err}
		}
		wrapped[entry.Remote] = impl
	}
	c.staged = wrapped
	c.stagedListing = listed
	c.stagedSkipped = skipped
	c.stagedComplete = true
	return listed, skipped, nil
}

// Commit publishes the staged generation: the live cache becomes the one
// the catalog writer accepted.
func (c *Client) Commit() {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if !c.stagedComplete {
		return
	}
	c.mu.Lock()
	c.cache = c.staged
	c.mu.Unlock()
	c.staged, c.stagedListing, c.stagedSkipped, c.stagedComplete = nil, nil, nil, false
}

// Discard drops the staged generation, leaving the live one serving.
func (c *Client) Discard() {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	c.staged, c.stagedListing, c.stagedSkipped, c.stagedComplete = nil, nil, nil, false
}

// StagedListing reports the staged generation when one is waiting: the
// installer publishes what verification already listed instead of listing
// twice.
func (c *Client) StagedListing() ([]ListedTool, []Skipped, bool) {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if !c.stagedComplete {
		return nil, nil, false
	}
	return c.stagedListing, c.stagedSkipped, true
}

// scrubText sanitizes free-form error text with this server's known values.
func (c *Client) scrubText(err error) string {
	sanitized, _ := c.scrub.ScrubString(err.Error())
	return sanitized
}

// InvokableByRemote answers from the last Sync's cache — the per-turn
// path; no network.
func (c *Client) InvokableByRemote(remote string) (tool.InvokableTool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	impl, ok := c.cache[remote]
	return impl, ok
}

// borrow admits one dispatch on this connection, counting it so teardown
// can wait for the work already on the wire.
func (c *Client) borrow() error {
	c.callsMu.Lock()
	defer c.callsMu.Unlock()
	if c.closed {
		return errClientClosed
	}
	c.calls++
	return nil
}

// release pairs one borrow.
func (c *Client) release() {
	c.callsMu.Lock()
	defer c.callsMu.Unlock()
	c.calls--
	if c.calls == 0 && c.drained != nil {
		close(c.drained)
		c.drained = nil
	}
}

// waitDrained blocks until every borrowed call on this connection returns,
// bounded by the administrator's call ceiling: every dispatch is bounded by
// it, so a retired connection cannot be pinned forever.
func (c *Client) waitDrained() {
	c.callsMu.Lock()
	if c.calls == 0 {
		c.callsMu.Unlock()
		return
	}
	if c.drained == nil {
		c.drained = make(chan struct{})
	}
	drained := c.drained
	c.callsMu.Unlock()

	timer := time.NewTimer(tooladmission.MaxCallTimeout)
	defer timer.Stop()
	select {
	case <-drained:
	case <-timer.C:
	}
}

// Close tears the connection down. Safe to call more than once.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.callsMu.Lock()
		c.closed = true
		c.callsMu.Unlock()
		c.closeErr = c.teardown()
	})
	return c.closeErr
}

// teardown cancels the lifetime context and closes the transport, bounded
// by closeGrace so a stuck transport cannot hang shutdown.
func (c *Client) teardown() error {
	c.cancel()
	if c.tr == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- c.tr.Close() }()
	timer := time.NewTimer(closeGrace)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return nil
	}
}
