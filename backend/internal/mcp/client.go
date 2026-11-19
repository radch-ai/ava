package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	mcpext "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// Client is one live MCP server connection.
type Client struct {
	id      string
	cfg     ServerConfig
	timeout time.Duration
	tr      transport.Interface
	cli     *client.Client
	cancel  context.CancelFunc
	proc    *stdioProc // nil over HTTP

	// cache is the last Sync's wrapped tools keyed by remote name —
	// the per-turn lookup path, no network.
	mu    sync.Mutex
	cache map[string]tool.InvokableTool

	closeOnce sync.Once
	closeErr  error
}

// Dial builds the transport for cfg and starts a client without caching
// it: the caller owns the result until Manager.Adopt (or Close).
func Dial(ctx context.Context, cfg ServerConfig) (*Client, error) {
	tr, proc, err := newTransport(cfg)
	if err != nil {
		return nil, err
	}
	return newClient(ctx, cfg, tr, proc)
}

// newTransport builds the transport and, for stdio, the process-group
// tracker wired into the spawned command.
func newTransport(cfg ServerConfig) (transport.Interface, *stdioProc, error) {
	switch cfg.Transport {
	case TransportStdio:
		env := make([]string, 0, len(cfg.Env))
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}
		proc := &stdioProc{}
		tr := transport.NewStdioWithOptions(cfg.Command, env, cfg.Args,
			transport.WithCommandFunc(func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
				cmd := exec.CommandContext(ctx, command, args...)
				// mcp-go merges os.Environ() only on its default spawn
				// path; the factory branch must do it itself
				// (stdio.go:172-175).
				cmd.Env = append(os.Environ(), env...)
				setPgid(cmd)
				proc.track(cmd)
				return cmd, nil
			}))
		return tr, proc, nil
	case TransportHTTP:
		opts := make([]transport.StreamableHTTPCOption, 0, 1)
		if len(cfg.Headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
		}
		tr, err := transport.NewStreamableHTTP(cfg.URL, opts...)
		if err != nil {
			return nil, nil, fmt.Errorf("http transport: %w", err)
		}
		return tr, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

// newClient starts a client over an already-built transport and completes
// the MCP initialize handshake. The transport's lifetime context is
// manager-owned (not the caller's request context): cancelling it on close
// is what makes exec.CommandContext kill a stdio server's direct child.
func newClient(ctx context.Context, cfg ServerConfig, tr transport.Interface, proc *stdioProc) (*Client, error) {
	lifetime, cancel := context.WithCancel(context.Background())
	c := &Client{
		id:      cfg.ID,
		cfg:     cfg,
		timeout: cfg.timeout(),
		tr:      tr,
		cli:     client.NewClient(tr),
		cancel:  cancel,
		proc:    proc,
	}
	if err := c.cli.Start(lifetime); err != nil {
		_ = c.teardown()
		return nil, fmt.Errorf("start: %w", err)
	}
	if proc != nil {
		// The pipe exists once the transport started the process.
		drainStderr(tr)
	}

	// The handshake is the only network-shaped step before use: bound it by
	// both the caller's deadline and the per-call timeout. A stdio process
	// that dies mid-handshake cancels it immediately: mcp-go leaves the
	// pending Initialize waiting forever, and the API cannot afford the
	// full deadline.
	initCtx, initCancel := context.WithTimeout(ctx, c.timeout)
	defer initCancel()
	if proc != nil {
		if exited := proc.watchExit(); exited != nil {
			stop := make(chan struct{})
			defer close(stop)
			go func() {
				select {
				case <-exited:
					initCancel()
				case <-stop:
				}
			}()
		}
	}
	if _, err := c.cli.Initialize(initCtx, mcpsdk.InitializeRequest{
		Params: mcpsdk.InitializeParams{
			ProtocolVersion: mcpsdk.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcpsdk.Implementation{Name: "ava", Version: "dev"},
		},
	}); err != nil {
		_ = c.teardown()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	return c, nil
}

// Tools lists the server's tools as eino base tools — the full list, no
// name filter.
func (c *Client) Tools(ctx context.Context) ([]tool.BaseTool, error) {
	tools, err := mcpext.GetTools(ctx, &mcpext.Config{Cli: c.cli})
	if err != nil {
		return nil, fmt.Errorf("mcp server %s: list tools: %w", c.id, err)
	}
	return tools, nil
}

// errUnrepresentable marks a tool whose composite model name violates
// the provider wire contract: install rejects it by name, the runtime
// path skips it — inert, never a failed turn.
var errUnrepresentable = errors.New("unrepresentable model tool name")

// wrap adapts one listed tool under the composite model name: Info
// reports `{slug}_{remote}`, InvokableRun carries the per-call timeout.
// The one wrapping path — Invokable and Sync both call it, so the name
// rules cannot drift apart.
func (c *Client) wrap(base tool.BaseTool, info *schema.ToolInfo) (tool.InvokableTool, error) {
	if info.Name == "" {
		return nil, fmt.Errorf("%w: mcp server %s: tool has no name", errUnrepresentable, c.id)
	}
	model := ModelName(c.id, info.Name)
	if len(model) > maxModelNameLen {
		return nil, fmt.Errorf("%w: mcp server %s: %q exceeds %d characters",
			errUnrepresentable, c.id, model, maxModelNameLen)
	}
	for _, r := range model {
		ok := r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return nil, fmt.Errorf("%w: mcp server %s: %q contains %q, outside [a-zA-Z0-9_-]",
				errUnrepresentable, c.id, model, r)
		}
	}
	return &wrappedTool{id: c.id, base: base, model: model, timeout: c.timeout}, nil
}

// Invokable wraps one listed tool for the registry (tests and callers
// that hold a BaseTool); Sync is the production path.
func (c *Client) Invokable(t tool.BaseTool) (tool.InvokableTool, error) {
	info, err := t.Info(context.Background())
	if err != nil {
		return nil, fmt.Errorf("mcp server %s: tool info: %w", c.id, err)
	}
	return c.wrap(t, info)
}

// ListedTool is one of a server's tools after Sync: the remote name and
// description the caller upserts into the catalog.
type ListedTool struct {
	Remote      string
	Description string
}

// Sync refreshes the client's cached tool set: it lists the server's
// tools, wraps the representable ones, and returns the listing for the
// catalog upsert plus the remote names whose composite model name
// violates the provider wire contract. Creation rejects those names;
// the runtime path drops them.
func (c *Client) Sync(ctx context.Context) ([]ListedTool, []string, error) {
	base, err := c.Tools(ctx)
	if err != nil {
		return nil, nil, err
	}
	wrapped := make(map[string]tool.InvokableTool, len(base))
	listed := make([]ListedTool, 0, len(base))
	var rejected []string
	for _, t := range base {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("mcp server %s: tool info: %w", c.id, err)
		}
		impl, err := c.wrap(t, info)
		if errors.Is(err, errUnrepresentable) {
			rejected = append(rejected, info.Name)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		wrapped[info.Name] = impl
		listed = append(listed, ListedTool{Remote: info.Name, Description: info.Desc})
	}
	c.mu.Lock()
	c.cache = wrapped
	c.mu.Unlock()
	return listed, rejected, nil
}

// InvokableByRemote answers from the last Sync's cache — the per-turn
// path; no network.
func (c *Client) InvokableByRemote(remote string) (tool.InvokableTool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	impl, ok := c.cache[remote]
	return impl, ok
}

// Close tears the connection down. Safe to call more than once.
func (c *Client) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.teardown() })
	return c.closeErr
}

// teardown kills in two passes: cancelling the lifetime context SIGKILLs the
// direct stdio child (exec.CommandContext), then the process group is
// signalled so grandchildren die too, and HTTP/in-process transports just
// close. Transport Close runs under closeGrace because mcp-go's stdio Close
// blocks in Wait when the server ignores stdin EOF (stdio.go:208-231).
func (c *Client) teardown() error {
	c.cancel()

	// The reaper (and only it) owns Wait while the process runs; join it
	// before mcp-go's Close-Wait so the two never race. The cancel above
	// SIGKILLs the child, so the join is bounded by process teardown.
	if c.proc != nil {
		c.proc.waitExited(closeGrace + 2*time.Second)
	}

	var closeErr error
	if c.tr != nil {
		done := make(chan error, 1)
		go func() { done <- c.tr.Close() }()
		select {
		case closeErr = <-done:
		case <-time.After(closeGrace):
			// Kill below reaps the process; the goroutine exits then.
		}
	}
	if c.proc != nil {
		killGroup(c.proc.pid())
	}
	return closeErr
}

// stdioProc tracks the spawned command so teardown can kill its process
// group: mcp-go's Stdio.Close only closes pipes and waits, so a server that
// ignores stdin EOF stays alive otherwise (stdio.go:208-231). It also
// reaps the process so a death during the handshake fails fast instead of
// waiting out the deadline.
type stdioProc struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	exited chan struct{}
	once   sync.Once
}

func (p *stdioProc) track(cmd *exec.Cmd) {
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()
}

// waitExited blocks until the reaper observed the exit (bounded): teardown
// joins it before mcp-go's Close-Wait runs, because os/exec.Cmd forbids
// concurrent Wait calls. Never started = nothing to join.
func (p *stdioProc) waitExited(timeout time.Duration) {
	exited := p.exited
	if exited == nil {
		return
	}
	select {
	case <-exited:
	case <-time.After(timeout):
	}
}

// watchExit starts the reaper and returns the channel closed on process
// exit. Call after the transport started the command (exec.Cmd.Wait before
// Start is an error, and mcp-go starts it inside Transport.Start).
// Idempotent. Reaping here makes mcp-go's own Close-Wait return "wait was
// already called"; teardown joins this reaper first so the two Waits never
// run concurrently (os/exec.Cmd is not safe for that).
func (p *stdioProc) watchExit() <-chan struct{} {
	p.once.Do(func() {
		p.mu.Lock()
		cmd := p.cmd
		p.mu.Unlock()
		if cmd == nil {
			return
		}
		exited := make(chan struct{})
		p.exited = exited
		go func() {
			_ = cmd.Wait()
			close(exited)
		}()
	})
	return p.exited
}

func (p *stdioProc) pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
