// Package mcpservers serves the MCP server endpoints under
// /api/mcp/servers: CRUD plus refresh. Creation and every
// connection-affecting patch verify the connection before anything is
// written (the provider template); secret values are write-only —
// sealed into the runtime vault on save, never echoed back.
package mcpservers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"modernc.org/sqlite"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
	"github.com/radch-ai/ava/backend/internal/vault"
)

const (
	// maxTimeoutMs is the administrator's call ceiling in
	// milliseconds — one source for the bound.
	maxTimeoutMs = int64(tooladmission.MaxCallTimeout / time.Millisecond)
	// sqliteConstraintPrimaryKey / sqliteConstraintUnique are the two
	// SQLITE_CONSTRAINT extended codes a duplicate slug insert can raise
	// (mcp_servers.id is the primary key; TEXT keys report either).
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// isDuplicateSlug reports whether err is the insert's slug conflict — a
// concurrent create must answer the same 409 as the pre-check, never 500.
func isDuplicateSlug(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code()
	return code == sqliteConstraintPrimaryKey || code == sqliteConstraintUnique
}

// boolInt maps a policy flag to its 0/1 column form.
func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// msDuration converts a timeoutMs column value to a duration.
func msDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

// configProblem returns the 400 message for a malformed server config;
// "" when well formed. The rule itself is mcp's one validator.
func configProblem(rawURL string) string {
	if err := mcp.ValidateURL(rawURL); err != nil {
		return "url must be an http(s) URL without embedded credentials"
	}
	return ""
}

func timeoutProblem(ms *int64) string {
	if ms == nil {
		return ""
	}
	if *ms <= 0 || *ms > maxTimeoutMs {
		return fmt.Sprintf("timeoutMs must be between 1 and %d", maxTimeoutMs)
	}
	return ""
}

// requestContext bounds one request-bound dial or discovery. The
// install-time budget keeps the answer inside the http.Server write
// deadline, so a slow or dead server receives a 400/502 instead of a
// killed connection (mcp.VerifyTimeout's doc).
func requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, mcp.VerifyTimeout)
}

// verify dials cfg directly — no manager, no cache — and stages the
// candidate's catalog under the admission bounds: a server that cannot
// connect, initialize or list a security-valid generation is rejected
// before anything is written. A tool whose name the provider cannot carry
// is reported as skipped, not rejected. On success it returns
// the connected client with its staged listing, and the caller publishes
// that listing instead of listing twice; on any failure nothing is
// left running.
//
// The whole check shares one 10-second budget — DNS, TLS, initialization
// and every listing page — so a slow server answers the request
// instead of outliving the http.Server write deadline.
func verify(ctx context.Context, cfg mcp.ServerConfig) (*mcp.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, mcp.VerifyTimeout)
	defer cancel()
	client, err := mcp.BoundedDial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Stage the listing here: the caller publishes the staged generation
	// instead of listing twice, and publication re-reads it.
	if _, _, err := client.SyncStaged(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// buildServer renders one server row with its tool count; secret
// values never appear, only the key names the row stores.
func buildServer(ctx context.Context, queries *db.Queries, row db.McpServer) (apitypes.McpServer, error) {
	count, err := queries.CountMcpServerTools(ctx, sql.NullString{String: row.ID, Valid: true})
	if err != nil {
		return apitypes.McpServer{}, fmt.Errorf("count tools for %q: %w", row.ID, err)
	}
	return serverView(row, count)
}

// serverView maps one row plus its tool count to the wire shape. Corrupt
// JSON columns are a server fault, not a silent empty list (one decode
// policy across every reader).
func serverView(row db.McpServer, toolCount int64) (apitypes.McpServer, error) {
	out := apitypes.McpServer{
		ID:                  row.ID,
		Name:                row.Name,
		Enabled:             row.Enabled != 0,
		ToolCount:           toolCount,
		CatalogState:        row.CatalogState,
		AllowPrivateNetwork: row.AllowPrivateNetwork != 0,
		AllowPlaintext:      row.AllowPlaintext != 0,
	}
	if row.Url.Valid {
		// A stored URL may carry credentials in its userinfo. The
		// runtime rejects it; the view never echoes it either.
		out.URL = maskedURL(row.Url.String)
	}
	if row.TimeoutMs.Valid {
		ms := row.TimeoutMs.Int64
		out.TimeoutMs = &ms
	}
	if row.LastError.Valid {
		msg := row.LastError.String
		out.LastError = &msg
	}
	var err error
	if out.HeaderKeys, err = decodeStrings(row.HeaderKeys); err != nil {
		return apitypes.McpServer{}, fmt.Errorf("decode headerKeys for %q: %w", row.ID, err)
	}
	return out, nil
}

// maskedURL hides userinfo in a stored endpoint: credentials ride the
// sealed header channel only, so neither the runtime nor a view may show
// them.
func maskedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.UserPassword("[redacted]", "[redacted]")
	return u.String()
}

// discoveredTools renders a listing for create/refresh responses.
func discoveredTools(slug string, listed []mcp.ListedTool) []apitypes.McpDiscoveredTool {
	out := make([]apitypes.McpDiscoveredTool, 0, len(listed))
	for _, t := range listed {
		out = append(out, apitypes.McpDiscoveredTool{
			ID:          mcp.CatalogID(slug, t.Remote),
			Name:        t.Remote,
			Description: t.Description,
		})
	}
	return out
}

// sealSecrets seals the one runtime blob for a server. Called
// with the full effective secrets: a patch that replaces only the
// header map clears the rest.
func sealSecrets(ctx context.Context, vaultStore *vault.Store, slug string, secrets mcpserver.Secrets) error {
	blob, err := json.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("encode secrets: %w", err)
	}
	return vaultStore.Put(ctx, vault.NameRuntime, mcpserver.EntryName(slug), mcpserver.Kind, blob, nil)
}

// decodeStrings parses a JSON string-array column into a non-nil
// slice.
func decodeStrings(s string) ([]string, error) {
	if s == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// skippedTools renders the cosmetic skips of one discovery pass.
func skippedTools(skipped []mcp.Skipped) []apitypes.McpSkippedTool {
	if len(skipped) == 0 {
		return nil
	}
	out := make([]apitypes.McpSkippedTool, 0, len(skipped))
	for _, s := range skipped {
		out = append(out, apitypes.McpSkippedTool{Name: s.Remote, Reason: s.Reason})
	}
	return out
}

// readServer maps the row read to 404 or a server fault. The bool is the
// "already answered" signal: the caller returns nil instead of running on
// with a zero row, which would append a second body to the same response.
// The error is the server fault the caller must log and report.
func readServer(ctx context.Context, queries *db.Queries, w http.ResponseWriter, id string) (row db.McpServer, ok bool, fault error) {
	row, err := queries.GetMcpServer(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("MCP server %q not found", id))
			return db.McpServer{}, false, nil
		}
		_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
		return db.McpServer{}, false, fmt.Errorf("read mcp server %q: %w", id, err)
	}
	return row, true, nil
}

// respondServer answers one saved server with the listing its pass
// knows: the rebuilt row plus the discovery view (tools and skips).
func respondServer(ctx context.Context, w http.ResponseWriter, status int, queries *db.Queries, id string,
	listed []mcp.ListedTool, skipped []mcp.Skipped) error {
	saved, err := queries.GetMcpServer(ctx, id)
	if err != nil {
		_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
		return fmt.Errorf("read back mcp server %q: %w", id, err)
	}
	server, err := buildServer(ctx, queries, saved)
	if err != nil {
		_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
		return fmt.Errorf("build server %q: %w", id, err)
	}
	return api.WriteJSON(w, status, apitypes.McpServerWithToolsResponse{
		Server:       server,
		Tools:        discoveredTools(id, listed),
		SkippedTools: skippedTools(skipped),
	})
}
