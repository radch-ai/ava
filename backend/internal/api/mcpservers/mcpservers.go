// Package mcpservers serves the MCP server endpoints under
// /api/mcp/servers: CRUD plus refresh. Creation and every
// connection-affecting patch verify the connection before anything is
// written (the provider template); secret values are write-only —
// sealed into the runtime vault on save, never echoed back.
package mcpservers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

// configProblem returns the 400 message for a server config that cannot be
// dialed as described; "" when it is usable. The rule itself is mcp's one
// validator — the transport matrix included.
func configProblem(cfg mcp.ServerConfig) string {
	if err := cfg.Validate(); err != nil {
		return err.Error()
	}
	return ""
}

// ackRequired rejects a stdio install or process edit made without the
// acknowledgment: running a process with the administrator's privileges is
// never granted silently.
const ackRequired = "stdio servers run a process with your privileges: acknowledge required"

// ackNotApplicable rejects the stdio acknowledgment on an http row: the
// flag grants process execution, which http servers never do.
const ackNotApplicable = "acknowledgeProcessExecution applies to stdio servers only"

// processAck fingerprints a stdio server's process shape — command plus
// args — as stored in process_ack: any change to either clears the
// acknowledgment until the next save acknowledges again. The JSON array
// encoding keeps the fingerprint injective: JSON strings escape every
// separator, so no two distinct shapes serialize alike.
func processAck(command string, args []string) string {
	if args == nil {
		args = []string{}
	}
	shape, _ := json.Marshal([]any{command, args})
	sum := sha256.Sum256(shape)
	return hex.EncodeToString(sum[:])
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
		Transport:           row.Transport,
		Enabled:             row.Enabled != 0,
		ToolCount:           toolCount,
		CatalogState:        row.CatalogState,
		InheritEnv:          row.InheritEnv != 0,
		AllowPrivateNetwork: row.AllowPrivateNetwork != 0,
		AllowPlaintext:      row.AllowPlaintext != 0,
	}
	if row.Command.Valid {
		out.Command = row.Command.String
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
	if out.HeaderKeys, err = mcpserver.DecodeStrings(row.HeaderKeys); err != nil {
		return apitypes.McpServer{}, fmt.Errorf("decode headerKeys for %q: %w", row.ID, err)
	}
	if out.Args, err = mcpserver.DecodeStrings(row.Args); err != nil {
		return apitypes.McpServer{}, fmt.Errorf("decode args for %q: %w", row.ID, err)
	}
	if out.EnvKeys, err = mcpserver.DecodeStrings(row.EnvKeys); err != nil {
		return apitypes.McpServer{}, fmt.Errorf("decode envKeys for %q: %w", row.ID, err)
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
