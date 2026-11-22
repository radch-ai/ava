package mcp

// Hardened HTTP transport: the host, not the model or the server, owns
// egress, plaintext-credential policy and the decoded response ceiling.
// Everything mcp-go would otherwise decide is decided here: no proxy, no
// redirects, no independent DNS re-resolution, no unbounded body.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/radch-ai/ava/backend/internal/security"
)

// Destination policy errors are re-exported so callers (the API's 400
// mapping, diagnostics codes) can name a denial class without importing
// the primitives package.
var (
	// ErrDestinationDenied is the non-public-destination denial: a
	// loopback or LAN endpoint without its approved exception.
	ErrDestinationDenied = security.ErrPrivateDenied
	// ErrLinkLocalDenied is the always-denied link-local/metadata class.
	ErrLinkLocalDenied = security.ErrLinkLocalDenied
	// ErrUnresolvedHost is a name that produced no address to validate.
	ErrUnresolvedHost = security.ErrUnresolved
)

var (
	// ErrInsecureCredentials denies credential injection over plaintext
	// HTTP to a destination that is not an approved loopback/LAN
	// endpoint.
	ErrInsecureCredentials = errors.New(
		"insecure transport denied: plaintext HTTP with credentials requires an approved loopback/LAN endpoint")
	// ErrResponseTooLarge aborts a response that passes the decoded-byte
	// ceiling; the partial buffer is never surfaced.
	ErrResponseTooLarge = fmt.Errorf("response too large: over %d bytes", MaxResponseBytes)
)

// guardedDial resolves, classifies, validates and then dials the address
// it validated — the same pass runs at connect time, so a DNS answer
// cannot drift between policy check and TCP connect.
func guardedDial(cfg ServerConfig) func(ctx context.Context, network, addr string) (net.Conn, error) {
	plaintextCreds := endpointPlaintext(cfg) && len(cfg.Headers) > 0
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("dial %q: %w", addr, err)
		}
		// One budget covers resolution and the dial loop: a slow DNS
		// answer cannot restart the clock.
		dialCtx, cancel := context.WithTimeout(ctx, ConnectTimeout)
		defer cancel()
		addrs, classes, err := security.ResolveClass(dialCtx, host)
		if err != nil {
			return nil, err
		}
		if err := security.DestinationAllowed(classes, cfg.AllowPrivateNetwork); err != nil {
			return nil, err
		}
		if plaintextCreds {
			for _, class := range classes {
				if class != security.ClassLoopback && class != security.ClassPrivate {
					return nil, ErrInsecureCredentials
				}
			}
		}
		dialer := &net.Dialer{}
		var lastErr error
		for _, ip := range addrs {
			conn, dialErr := dialer.DialContext(dialCtx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, fmt.Errorf("dial %s: %w", host, lastErr)
	}
}

// newHTTPClient builds the hardened client every MCP request rides: no
// proxy, a pinning dial, a bounded body, and no redirects.
func newHTTPClient(cfg ServerConfig) *http.Client {
	scrub := security.NewScrubber(cfg.KnownSecrets())
	rt := &http.Transport{
		// A proxy re-resolves the destination: that would defeat the
		// address pinning below.
		Proxy: nil,
		// Resolution, classification and dial happen in one pass.
		DialContext:         guardedDial(cfg),
		TLSHandshakeTimeout: ConnectTimeout,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{
		Transport: &limitRT{
			rt:             rt,
			allowPlaintext: cfg.AllowPlaintext,
			hasCreds:       len(cfg.Headers) > 0,
			plaintext:      endpointPlaintext(cfg),
			scrub:          scrub,
		},
		// Redirects are denied outright: an approved endpoint's
		// authority cannot be widened by a 3xx, and credentials are
		// never forwarded to another host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		// The whole-call deadline belongs to the invocation context,
		// not to the HTTP client.
		Timeout: 0,
	}
}

// endpointPlaintext reports whether the configured endpoint is plaintext
// HTTP (as opposed to HTTPS).
func endpointPlaintext(cfg ServerConfig) bool {
	u, err := url.Parse(cfg.URL)
	return err == nil && u.Scheme == "http"
}

// limitRT is the HTTP round tripper every MCP request rides: it denies
// credential-bearing plaintext requests outright and bounds both the
// success body and the error body mcp-go folds into messages.
type limitRT struct {
	rt             *http.Transport
	allowPlaintext bool
	hasCreds       bool
	plaintext      bool
	scrub          *security.Scrubber
}

// RoundTrip applies the transport policy to one request.
func (l *limitRT) RoundTrip(req *http.Request) (*http.Response, error) {
	// The request is the authority on whether credentials ride it —
	// net/http injects Basic auth from URL userinfo before RoundTrip.
	hasCreds := l.hasCreds || req.URL.User != nil
	if l.plaintext && hasCreds && !l.allowPlaintext {
		return nil, ErrInsecureCredentials
	}
	resp, err := l.rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		// mcp-go reads this body into an error string; a bounded,
		// scrubbed copy is all it may see.
		resp.Body = sanitizedErrorBody(resp.Body, l.scrub)
		return resp, nil
	}
	resp.Body = &boundedBody{inner: resp.Body, remaining: MaxResponseBytes}
	return resp, nil
}

// sanitizedErrorBody replaces a non-success body with its bounded,
// sanitized text. Anything over the error-body ceiling becomes a static
// marker: a partial buffer never leaves this function.
func sanitizedErrorBody(body io.ReadCloser, scrub *security.Scrubber) io.ReadCloser {
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes+1))
	if err != nil || len(raw) > maxErrorBodyBytes {
		return io.NopCloser(strings.NewReader("[response body withheld: over the error-body limit]"))
	}
	text, _ := scrub.ScrubString(string(raw))
	return io.NopCloser(strings.NewReader(text))
}

// boundedBody fails the read that would cross the decoded-byte ceiling,
// so accumulation stops incrementally instead of after the fact.
type boundedBody struct {
	inner     io.ReadCloser
	remaining int64
	over      bool
}

// Read passes through until the ceiling, then refuses. A body of exactly
// the ceiling is a clean end, not an overflow: reaching zero is
// settled by a one-byte probe rather than by remembering the reader said
// nil.
func (b *boundedBody) Read(p []byte) (int, error) {
	if b.over {
		return 0, ErrResponseTooLarge
	}
	if b.remaining <= 0 {
		var probe [1]byte
		n, err := b.inner.Read(probe[:])
		if n > 0 {
			b.over = true
			return 0, ErrResponseTooLarge
		}
		if errors.Is(err, io.EOF) {
			return 0, io.EOF
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.inner.Read(p)
	b.remaining -= int64(n)
	if b.remaining <= 0 && err == nil {
		// The next read settles end-of-stream versus overflow.
		return n, nil
	}
	return n, err
}

// Close closes the underlying body.
func (b *boundedBody) Close() error { return b.inner.Close() }

// scrubLogger sanitizes everything mcp-go logs: its SSE reader logs raw
// event data on a decode failure, which must never reach a log sink
// unsanitized.
type scrubLogger struct {
	scrub *security.Scrubber
}

// Infof logs one scrubbed informational line.
func (l scrubLogger) Infof(format string, v ...any) { l.write("INFO", format, v...) }

// Errorf logs one scrubbed error line.
func (l scrubLogger) Errorf(format string, v ...any) { l.write("ERROR", format, v...) }

// write formats, scrubs, then logs — the order that keeps a value from
// ever being observable.
func (l scrubLogger) write(level, format string, v ...any) {
	line := fmt.Sprintf(format, v...)
	if l.scrub != nil {
		line, _ = l.scrub.ScrubString(line)
	}
	log.Print(level + ": mcp: " + line)
}
