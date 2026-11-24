package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/mcptest"
	"github.com/radch-ai/ava/backend/internal/security"
)

// connectFixture dials one fixture through the manager, with the policy
// flags the test needs.
func connectFixture(t *testing.T, srv *mcptest.Server, cfg ServerConfig) (*Manager, *Client, error) {
	t.Helper()
	m := NewManager()
	t.Cleanup(m.CloseAll)
	cfg.ID = "fixture"
	if cfg.URL == "" {
		cfg.URL = srv.URL
	}
	cfg.Transport = TransportHTTP
	cfg.Timeout = 5 * time.Second
	// The fixture listens on loopback: approve the local destination and
	// the plaintext hop the way an operator would. Denial paths dial
	// directly instead of going through this helper.
	cfg.AllowPrivateNetwork = true
	cfg.AllowPlaintext = true
	c, err := m.Connect(t.Context(), cfg)
	return m, c, err
}

// TestHTTPRoundTrip drives the streamable-HTTP transport against an
// in-process server and proves configured headers reach the wire.
func TestHTTPRoundTrip(t *testing.T) {
	srv := mcptest.Start(t)
	_, c, err := connectFixture(t, srv, ServerConfig{
		Headers: map[string]string{"X-Ava-Probe": "on"},
	}) // loopback + plaintext approval comes from the helper
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if _, _, err := c.SyncStaged(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	c.Commit()
	inv, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after sync")
	}
	out, err := inv.InvokableRun(t.Context(), `{"text":"hello http"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// A single text block arrives as text, not JSON-enveloped.
	if out != "hello http" {
		t.Fatalf("result = %q, want the bare text", out)
	}
	if got := srv.Header("X-Ava-Probe"); got != "on" {
		t.Fatalf("configured header on the wire = %q, want on", got)
	}
}

// TestBoundedDialCapsConfiguredTimeout pins the install-time budget: a
// user-configured timeout larger than the verify cap must not stretch a
// connection check past the cap (the HTTP write deadline cannot wait).
func TestBoundedDialCapsConfiguredTimeout(t *testing.T) {
	srv := mcptest.Start(t, mcptest.WithDelay(20*time.Second))
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := BoundedDial(context.Background(), ServerConfig{
			ID: "slow", URL: srv.URL, Timeout: 60 * time.Second,
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dial of a never-answering server succeeded")
		}
		if elapsed := time.Since(start); elapsed > VerifyTimeout+5*time.Second {
			t.Fatalf("bounded dial took %v, want <= %v", elapsed, VerifyTimeout)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("bounded dial ignored the verify cap")
	}
}

// A redirect is a denial, not a hop: an approved endpoint's authority
// cannot be widened by a 3xx.
func TestRedirectDenied(t *testing.T) {
	target := mcptest.Start(t)
	redirector := mcptest.Start(t, mcptest.WithRedirect(target.URL))
	_, _, err := connectFixture(t, redirector, ServerConfig{})
	if err == nil {
		t.Fatal("redirecting endpoint connected")
	}
	if target.Calls() != 0 {
		t.Fatalf("redirect target received %d requests, want 0", target.Calls())
	}
}

// Loopback destinations are denied by default and admitted only with the
// server's approved exception: the fixture listens on 127.0.0.1,
// so every local connect exercises that path.
func TestLoopbackRequiresApproval(t *testing.T) {
	srv := mcptest.Start(t)
	m := NewManager()
	t.Cleanup(m.CloseAll)

	_, err := m.Connect(t.Context(), ServerConfig{
		ID: "loopback", Transport: TransportHTTP, URL: srv.URL, Timeout: 5 * time.Second,
	})
	if !errors.Is(err, ErrDestinationDenied) {
		t.Fatalf("connect without approval err = %v, want ErrDestinationDenied", err)
	}
	if _, ok := m.Get("loopback"); ok {
		t.Fatal("denied destination installed a client")
	}

	_, err = m.Connect(t.Context(), ServerConfig{
		ID: "loopback", Transport: TransportHTTP, URL: srv.URL, Timeout: 5 * time.Second,
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatalf("connect with approval: %v", err)
	}
}

// Link-local and metadata destinations are denied even with the private
// exception.
func TestMetadataDestinationAlwaysDenied(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)
	_, err := m.Connect(t.Context(), ServerConfig{
		ID:                  "metadata",
		Transport:           TransportHTTP,
		URL:                 "http://169.254.169.254/mcp",
		Timeout:             time.Second,
		AllowPrivateNetwork: true,
	})
	if !errors.Is(err, ErrLinkLocalDenied) {
		t.Fatalf("metadata connect err = %v, want ErrLinkLocalDenied", err)
	}
}

// Plaintext HTTP with credentials needs its own approval; a public
// destination stays denied even when that approval is set.
func TestPlaintextCredentialsPolicy(t *testing.T) {
	srv := mcptest.Start(t)
	headers := map[string]string{"Authorization": "Bearer canary-token"}

	m := NewManager()
	t.Cleanup(m.CloseAll)
	_, err := m.Connect(t.Context(), ServerConfig{
		ID: "plain", Transport: TransportHTTP, URL: srv.URL, Timeout: 5 * time.Second,
		Headers: headers, AllowPrivateNetwork: true,
	})
	if !errors.Is(err, ErrInsecureCredentials) {
		t.Fatalf("plaintext-with-creds connect err = %v, want ErrInsecureCredentials", err)
	}

	// Approved loopback plaintext carries its credentials.
	_, err = m.Connect(t.Context(), ServerConfig{
		ID: "plain", Transport: TransportHTTP, URL: srv.URL, Timeout: 5 * time.Second,
		Headers: headers, AllowPrivateNetwork: true, AllowPlaintext: true,
	})
	if err != nil {
		t.Fatalf("approved loopback plaintext connect: %v", err)
	}

	// A public destination is never credential-bearing over plaintext,
	// approval or not: the class check runs before the dial.
	m2 := NewManager()
	t.Cleanup(m2.CloseAll)
	_, err = m2.Connect(t.Context(), ServerConfig{
		ID:        "public",
		Transport: TransportHTTP,
		URL:       "http://93.184.216.34/mcp",
		Timeout:   time.Second, Headers: headers,
		AllowPrivateNetwork: true, AllowPlaintext: true,
	})
	if !errors.Is(err, ErrInsecureCredentials) {
		t.Fatalf("public plaintext connect err = %v, want ErrInsecureCredentials", err)
	}
}

// An oversized decoded response aborts with the typed error and never
// surfaces a partial body.
func TestResponseCeilingAborts(t *testing.T) {
	srv := mcptest.Start(t, mcptest.WithToolResult(MaxResponseBytes+1024))
	_, c, err := connectFixture(t, srv, ServerConfig{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, _, err := c.SyncStaged(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	c.Commit()
	inv, ok := c.InvokableByRemote("big")
	if !ok {
		t.Fatal("big not cached after sync")
	}
	out, err := inv.InvokableRun(t.Context(), `{}`)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversized call err = %v, want ErrResponseTooLarge", err)
	}
	if strings.Contains(out, "xxxx") || strings.Contains(err.Error(), "xxxx") {
		t.Fatalf("partial body surfaced: out=%q err=%v", out, err)
	}
}

// A non-success response body becomes an error string inside mcp-go: it
// is bounded and scrubbed before that can happen.
func TestErrorBodyScrubbed(t *testing.T) {
	srv := mcptest.Start(t, mcptest.WithStatus(500))
	_, _, err := connectFixture(t, srv, ServerConfig{
		Headers: map[string]string{"Authorization": "Bearer canary-token"},
	})
	if err == nil {
		t.Fatal("500 endpoint connected")
	}
	if !strings.Contains(err.Error(), "[REDACTED:secret]") {
		t.Fatalf("error %q carries no redaction for the reflected credential", err)
	}
	for _, leaked := range []string{"canary-token", mcptest.PatternCanary} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("raw %q surfaced in error %q", leaked, err)
		}
	}
}

// The transport's security posture is pinned structurally: no proxy, no
// redirect following, no whole-client timeout, connect cap set.
func TestTransportPosture(t *testing.T) {
	c := newHTTPClient(ServerConfig{
		ID: "posture", URL: "https://example.test/mcp",
		Headers: map[string]string{"Authorization": "Bearer x"},
	}, security.NewPatternScrubber())
	rt, ok := c.Transport.(*limitRT)
	if !ok {
		t.Fatalf("client transport = %T, want *limitRT", c.Transport)
	}
	if rt.rt.Proxy != nil {
		t.Fatal("proxy set: a proxy would re-resolve the destination")
	}
	if rt.rt.TLSHandshakeTimeout != ConnectTimeout {
		t.Fatalf("TLS handshake timeout = %v, want %v", rt.rt.TLSHandshakeTimeout, ConnectTimeout)
	}
	if c.CheckRedirect == nil {
		t.Fatal("redirects are followed: CheckRedirect is nil")
	}
	if err := c.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy err = %v, want ErrUseLastResponse", err)
	}
	if c.Timeout != 0 {
		t.Fatalf("client timeout = %v, want 0 (the invocation context owns it)", c.Timeout)
	}
	if !rt.hasCreds {
		t.Fatal("https-with-headers client does not register its credentials")
	}
	if rt.plaintext {
		t.Fatal("https client flagged plaintext")
	}
	plain := newHTTPClient(ServerConfig{
		ID: "plain", URL: "http://example.test/mcp",
		Headers: map[string]string{"X-Api-Key": "x"},
	}, security.NewPatternScrubber())
	if prt := plain.Transport.(*limitRT); !prt.plaintext {
		t.Fatal("http client not flagged plaintext")
	}
}

// Numeric tokens survive decoding, fingerprinting and storage — two
// schemas differing only past float64's integer range are different
// definitions.
func TestSchemaNumbersPreserved(t *testing.T) {
	withBound := func(bound string) *mcptest.Server {
		return mcptest.Start(t, mcptest.WithEchoSchema(
			`{"type":"object","properties":{"n":{"type":"integer","minimum":`+bound+`}}}`))
	}

	c := newInProcess(t, "helper", 5*time.Second,
		mcpsdk.NewToolWithRawSchema("echo", "Echoes its input text.",
			[]byte(`{"type":"object","properties":{"n":{"type":"integer","minimum":9007199254740993}}}`)))
	listed, _ := syncOnce(t, c)
	if len(listed) != 1 {
		t.Fatalf("listing = %+v, want one tool", listed)
	}
	if !strings.Contains(listed[0].DefinitionJSON, "9007199254740993") {
		t.Fatalf("definition = %s, want the exact numeric literal", listed[0].DefinitionJSON)
	}

	// The two bounds differ only beyond float64 precision; the stored
	// definition must still tell them apart.
	definitionOf := func(srv *mcptest.Server) string {
		_, c, err := connectFixture(t, srv, ServerConfig{})
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		listed, _ := syncOnce(t, c)
		if len(listed) != 1 {
			t.Fatalf("listing = %+v, want one tool", listed)
		}
		if !strings.Contains(listed[0].DefinitionJSON, `"minimum"`) {
			t.Fatalf("definition = %s, want the schema bound", listed[0].DefinitionJSON)
		}
		return listed[0].DefinitionJSON
	}
	low, high := definitionOf(withBound("9007199254740992")), definitionOf(withBound("9007199254740993"))
	if low == high {
		t.Fatal("two distinct schema bounds produced the same definition")
	}
}

// Malformed or non-object annotations reject the generation instead
// of being silently dropped from what the model is told about a tool.
func TestMalformedAnnotationsRejectCatalog(t *testing.T) {
	c := &Client{id: "helper", cfg: ServerConfig{ID: "helper"}, scrub: security.NewPatternScrubber()}
	for _, annotations := range []string{`"x"$(invalid`, `["read-only"]`} {
		_, _, err := c.buildListing([]rawTool{{
			Name: "echo", Annotations: json.RawMessage(annotations),
		}})
		var catalog *CatalogError
		if !errors.As(err, &catalog) {
			t.Fatalf("annotations %q: err = %v, want a CatalogError", annotations, err)
		}
		if catalog.Code != CodeCatalogInvalidSchema {
			t.Fatalf("annotations %q: code = %q, want %q", annotations, catalog.Code, CodeCatalogInvalidSchema)
		}
		if !strings.Contains(err.Error(), "annotations") {
			t.Fatalf("annotations %q: err = %v, want the annotations cause", annotations, err)
		}
	}
}

// An HTTP-200 JSON-RPC listing error is scrubbed before it becomes a
// diagnostic or an API answer.
func TestListProtocolErrorReflectedSecretScrubbed(t *testing.T) {
	const secret = "canary-listing-token"
	srv := mcptest.Start(t, mcptest.WithListError("upstream rejected "+secret))
	_, c, err := connectFixture(t, srv, ServerConfig{
		Headers: map[string]string{"Authorization": "Bearer " + secret},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, _, err = c.SyncStaged(t.Context())
	if err == nil {
		t.Fatal("listing error swallowed")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error %q leaked the credential", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED:secret]") {
		t.Fatalf("error %q carries no redaction", err)
	}
}

// A schema reflecting the server's own credential rejects the
// generation — the definition is authority, never a rewritten copy.
func TestSchemaReflectedSecretRejectsCatalog(t *testing.T) {
	const secret = "canary-schema-token"
	srv := mcptest.Start(t, mcptest.WithEchoSchema(
		`{"type":"object","properties":{"k":{"type":"string","default":"`+secret+`"}}}`))
	_, c, err := connectFixture(t, srv, ServerConfig{
		Headers: map[string]string{"Authorization": "Bearer " + secret},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, _, err = c.SyncStaged(t.Context())
	var catalog *CatalogError
	if !errors.As(err, &catalog) || catalog.Code != CodeCatalogInvalidSchema {
		t.Fatalf("sync err = %v, want a catalog_invalid_schema rejection", err)
	}
}

// A staged generation is not live until the catalog write commits.
func TestStagedCacheNotLiveUntilCommit(t *testing.T) {
	srv := mcptest.Start(t)
	_, c, err := connectFixture(t, srv, ServerConfig{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, _, err := c.SyncStaged(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	c.Commit()

	srv.SetEcho("changed description", `{"type":"object","properties":{"text":{"type":"string"}}}`)
	first, _, err := c.SyncStaged(t.Context())
	if err != nil {
		t.Fatalf("staged sync: %v", err)
	}
	if staged, _, ok := c.StagedListing(); !ok || staged[0].Remote != "echo" {
		t.Fatalf("staged listing = %+v ok=%v, want the new generation", staged, ok)
	}
	// The live cache still serves the previous generation.
	inv, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo vanished from the live cache while staged")
	}
	info, err := inv.Info(t.Context())
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.Desc != "Echoes its input text." {
		t.Fatalf("live description = %q, want the previous generation", info.Desc)
	}

	c.Commit()
	committed, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("committed generation lost the tool")
	}
	if info, err = committed.Info(t.Context()); err != nil || info.Desc != first[0].Description {
		t.Fatalf("committed description = %v, err %v; want %q", info, err, first[0].Description)
	}
	if _, _, ok := c.StagedListing(); ok {
		t.Fatal("staged listing survived its commit")
	}
}

// Discard leaves the committed generation serving.
func TestDiscardKeepsPreviousGeneration(t *testing.T) {
	srv := mcptest.Start(t)
	_, c, err := connectFixture(t, srv, ServerConfig{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, _, err := c.SyncStaged(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	c.Commit()
	srv.SetEcho("changed description", `{"type":"object","properties":{"text":{"type":"string"}}}`)
	if _, _, err := c.SyncStaged(t.Context()); err != nil {
		t.Fatalf("staged sync: %v", err)
	}
	c.Discard()
	if _, _, ok := c.StagedListing(); ok {
		t.Fatal("staged listing survived its discard")
	}
	inv, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("discard dropped the committed tool")
	}
	if info, err := inv.Info(t.Context()); err != nil || info.Desc != "Echoes its input text." {
		t.Fatalf("live description = %v, err %v; want the previous generation", info, err)
	}
}

// A body of exactly the ceiling is a clean end, not an overflow —
// and one byte more still aborts without surfacing the partial body.
func TestResponseExactlyAtLimitAccepted(t *testing.T) {
	for _, tc := range []struct {
		size int
		ok   bool
	}{
		{MaxResponseBytes, true},
		{MaxResponseBytes + 1, false},
	} {
		var served atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			served.Add(1)
			_, _ = w.Write(bytes.Repeat([]byte("x"), tc.size))
		}))
		t.Cleanup(srv.Close)

		// The bounded body rides the real hardened client, so the request
		// path (dial policy, limit filter) is the production one.
		client := newHTTPClient(ServerConfig{
			ID: "ceiling", URL: srv.URL,
			AllowPrivateNetwork: true, AllowPlaintext: true,
		}, security.NewPatternScrubber())
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("size %d: request: %v", tc.size, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if tc.ok {
			if err != nil || len(body) != MaxResponseBytes {
				t.Fatalf("exactly-at-limit body: %d bytes, err %v; want %d clean bytes",
					len(body), err, MaxResponseBytes)
			}
			continue
		}
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("over-limit body err = %v, want ErrResponseTooLarge", err)
		}
		if len(body) > MaxResponseBytes {
			t.Fatalf("over-limit read surfaced %d bytes", len(body))
		}
	}
}

// A stored URL carrying credentials in its userinfo is rejected at
// runtime instead of injecting unapproved Basic auth.
func TestUserinfoRejectedAtRuntime(t *testing.T) {
	srv := mcptest.Start(t)
	m := NewManager()
	t.Cleanup(m.CloseAll)
	//nolint:lll // one-line URL userinfo is the point.
	cfgURL := strings.Replace(srv.URL, "http://", "http://user:pass@", 1)
	_, err := m.Connect(t.Context(), ServerConfig{
		ID: "userinfo", URL: cfgURL, Timeout: time.Second,
		AllowPrivateNetwork: true, AllowPlaintext: true,
	})
	if err == nil || !strings.Contains(err.Error(), "userinfo") {
		t.Fatalf("connect err = %v, want the userinfo rejection", err)
	}
	if srv.Calls() != 0 {
		t.Fatalf("userinfo endpoint received %d requests, want 0", srv.Calls())
	}
	if _, ok := m.Get("userinfo"); ok {
		t.Fatal("rejected userinfo row installed a client")
	}
}

// A hostile initialize handshake answers HTTP 200 with a JSON-RPC
// error; its server-controlled text is scrubbed with the server's known
// values before it can become a diagnostic or an API answer.
func TestInitializeErrorScrubbed(t *testing.T) {
	const secret = "canary-init-token"
	srv := mcptest.Start(t, mcptest.WithInitializeError("handshake rejected "+secret))
	_, _, err := connectFixture(t, srv, ServerConfig{
		Headers: map[string]string{"Authorization": "Bearer " + secret},
	})
	if err == nil {
		t.Fatal("hostile handshake connected")
	}
	if !strings.Contains(err.Error(), "[REDACTED:secret]") {
		t.Fatalf("error %q carries no redaction", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error %q leaked the credential", err)
	}
}

// A description reflecting the server's own credential is scrubbed
// in the listing — the model-visible instruction, sanitized not rejected.
func TestDescriptionReflectedSecretScrubbed(t *testing.T) {
	const secret = "canary-description-token"
	srv := mcptest.Start(t, mcptest.WithEchoDescription("notes about "+secret))
	_, c, err := connectFixture(t, srv, ServerConfig{
		Headers: map[string]string{"Authorization": "Bearer " + secret},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	listed, _, err := c.SyncStaged(t.Context())
	if err != nil {
		t.Fatalf("staged sync: %v", err)
	}
	c.Commit()
	if len(listed) != 1 || listed[0].Remote != "echo" {
		t.Fatalf("listing = %+v, want the echo tool", listed)
	}
	if strings.Contains(listed[0].Description, secret) {
		t.Fatalf("description %q leaked the credential", listed[0].Description)
	}
	if !strings.Contains(listed[0].Description, "[REDACTED:secret]") {
		t.Fatalf("description %q carries no redaction", listed[0].Description)
	}
	if strings.Contains(listed[0].DefinitionJSON, secret) {
		t.Fatalf("definition %s leaked the credential", listed[0].DefinitionJSON)
	}
}

// Annotations reflecting the server's own credential reject the
// generation — authority-relevant metadata is never rewritten under the
// server's name.
func TestAnnotationsReflectedSecretRejectCatalog(t *testing.T) {
	const secret = "canary-annotation-token"
	srv := mcptest.Start(t, mcptest.WithEchoAnnotations(mcpsdk.ToolAnnotation{Title: secret}))
	_, c, err := connectFixture(t, srv, ServerConfig{
		Headers: map[string]string{"Authorization": "Bearer " + secret},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, _, err = c.SyncStaged(t.Context())
	var catalog *CatalogError
	if !errors.As(err, &catalog) || catalog.Code != CodeCatalogInvalidSchema {
		t.Fatalf("staged sync err = %v, want a catalog_invalid_schema rejection", err)
	}
}

// The advertised schema keeps its exact numeric literals — a default
// past float64's integer range survives into the wrapped tool's Info(),
// the model-visible contract, not just the stored definition.
func TestAdvertisedSchemaPreservesNumbers(t *testing.T) {
	srv := mcptest.Start(t, mcptest.WithEchoSchema(
		`{"type":"object","properties":{"n":{"type":"integer","default":9007199254740993}}}`))
	_, c, err := connectFixture(t, srv, ServerConfig{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, _, err := c.SyncStaged(t.Context()); err != nil {
		t.Fatalf("staged sync: %v", err)
	}
	c.Commit()
	inv, ok := c.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after commit")
	}
	info, err := inv.Info(t.Context())
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	if !strings.Contains(string(encoded), "9007199254740993") {
		t.Fatalf("advertised schema = %s, want the exact numeric literal", encoded)
	}
}

// A server that re-issues its first pagination cursor forever is a
// malformed listing — the generation pauses instead of looping.
func TestRepeatedCursorPauses(t *testing.T) {
	srv := mcptest.Start(t,
		mcptest.WithPageSize(1), mcptest.WithToolCount(1), mcptest.WithRepeatedCursor())
	_, c, err := connectFixture(t, srv, ServerConfig{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, _, err = c.SyncStaged(t.Context())
	var catalog *CatalogError
	if !errors.As(err, &catalog) || catalog.Code != CodeCatalogInvalidSchema {
		t.Fatalf("staged sync err = %v, want a catalog_invalid_schema rejection", err)
	}
}

// A listing with more real pages than the page cap is bounded and
// rejected as oversized, never read to the end. Real pagination (one
// tool per page, all distinct) isolates the page cap from the
// duplicate/cursor rejection paths.
func TestPageCapRejects(t *testing.T) {
	srv := mcptest.Start(t, mcptest.WithPageSize(1), mcptest.WithToolCount(60))
	_, c, err := connectFixture(t, srv, ServerConfig{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, _, err = c.SyncStaged(t.Context())
	var catalog *CatalogError
	if !errors.As(err, &catalog) || catalog.Code != CodeCatalogTooLarge {
		t.Fatalf("staged sync err = %v, want a catalog_too_large rejection", err)
	}
}

// The decoded-response ceiling applies after transparent gzip
// decompression — a small wire body cannot smuggle an oversized payload.
func TestGzipOversizedAborts(t *testing.T) {
	payload := append([]byte(`{"jsonrpc":"2.0","id":1,"result":{"padding":"`),
		bytes.Repeat([]byte("x"), MaxResponseBytes+1024)...)
	payload = append(payload, '"', '}', '}')
	var wire bytes.Buffer
	gz := gzip.NewWriter(&wire)
	if _, err := gz.Write(payload); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(wire.Bytes())
	}))
	t.Cleanup(srv.Close)

	// The bounded body rides the real hardened client; Go's transport
	// adds Accept-Encoding and decompresses before the ceiling counts.
	client := newHTTPClient(ServerConfig{
		ID: "gzip", URL: srv.URL,
		AllowPrivateNetwork: true, AllowPlaintext: true,
	}, security.NewPatternScrubber())
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("read err = %v, want ErrResponseTooLarge", err)
	}
	if len(body) > MaxResponseBytes {
		t.Fatalf("over-limit read surfaced %d bytes", len(body))
	}
}

// stuckReader yields neither bytes nor an error: a bounded body over it
// must answer ErrNoProgress instead of letting the consumer spin.
type stuckReader struct{}

func (stuckReader) Read([]byte) (int, error) { return 0, nil }

func TestBoundedBodyStuckReaderErrors(t *testing.T) {
	b := &boundedBody{inner: io.NopCloser(stuckReader{}), remaining: 0}
	if _, err := io.ReadAll(b); !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("err = %v, want ErrNoProgress", err)
	}
}

// The protocol logger actually reaches a sink: mcp-go's error lines
// arrive at error level, informational lines at info, and the server's
// known values are scrubbed before the write.
func TestScrubLogger(t *testing.T) {
	var buf bytes.Buffer
	l := scrubLogger{scrub: security.NewScrubber([]string{"canary-token"}), log: zerolog.New(&buf)}

	l.Errorf("sse decode failed: Bearer %s", "canary-token")
	errLine := buf.String()
	if !strings.Contains(errLine, `"level":"error"`) {
		t.Fatalf("Errorf did not reach the sink at error level: %s", errLine)
	}
	if strings.Contains(errLine, "canary-token") {
		t.Fatalf("raw credential reached the sink: %s", errLine)
	}
	if !strings.Contains(errLine, "[REDACTED:secret]") {
		t.Fatalf("no redaction marker in %s", errLine)
	}

	buf.Reset()
	l.Infof("connected to %s", "example.test")
	infoLine := buf.String()
	if !strings.Contains(infoLine, `"level":"info"`) || !strings.Contains(infoLine, "example.test") {
		t.Fatalf("Infof did not reach the sink at info level: %s", infoLine)
	}
}

// The hardened transport keeps a bounded idle pool: concurrent calls on
// one server reuse connections instead of re-dialing (and re-fencing)
// per call.
func TestTransportKeepsIdlePool(t *testing.T) {
	c := newHTTPClient(ServerConfig{ID: "pool"}, security.NewPatternScrubber())
	lrt, ok := c.Transport.(*limitRT)
	if !ok {
		t.Fatalf("transport = %T, want *limitRT", c.Transport)
	}
	rt := lrt.rt
	if rt.MaxIdleConns != 16 || rt.MaxIdleConnsPerHost != 16 {
		t.Fatalf("idle pool = %d/%d, want 16/16", rt.MaxIdleConns, rt.MaxIdleConnsPerHost)
	}
	if rt.IdleConnTimeout != 90*time.Second {
		t.Fatalf("idle timeout = %s, want 90s", rt.IdleConnTimeout)
	}
}
