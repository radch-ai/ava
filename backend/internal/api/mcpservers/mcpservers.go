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
	"slices"
	"strings"
	"time"

	"modernc.org/sqlite"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/vault"
)

const (
	maxTimeoutMs = 10 * 60 * 1000
	maxModelName = 64 // OpenAI's stricter tool-name cap
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

// msDuration converts a timeoutMs column value to a duration.
func msDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

// configProblem returns the 400 message for a malformed transport
// config; "" when well formed.
func configProblem(transport, command, rawURL string) string {
	switch transport {
	case mcp.TransportStdio:
		if strings.TrimSpace(command) == "" {
			return "stdio transport needs a command"
		}
	case mcp.TransportHTTP:
		if !isValidHTTPURL(rawURL) {
			return "http transport needs an http(s) url"
		}
	default:
		return `transport must be "stdio" or "http"`
	}
	return ""
}

func isValidHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func timeoutProblem(ms *int64) string {
	if ms == nil {
		return ""
	}
	if *ms <= 0 || *ms > maxTimeoutMs {
		return "timeoutMs must be between 1 and 600000"
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

// verify dials cfg directly — no manager, no cache — and syncs its
// listing: the candidate must connect, initialize, and report only tools
// whose composite model names fit the provider wire contract (a server
// exposing a name the model cannot call is rejected by name, never
// installed minus that tool). On success it returns the connected
// client and the caller adopts it (Manager.Adopt), so the same process
// serves discovery — no second dial; on any failure nothing is left
// running.
func verify(ctx context.Context, cfg mcp.ServerConfig) (*mcp.Client, []mcp.ListedTool, error) {
	client, err := mcp.BoundedDial(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	listed, rejected, err := client.Sync(ctx)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	if len(rejected) > 0 {
		_ = client.Close()
		return nil, nil, fmt.Errorf(
			"tool %q cannot be represented as a model tool name (max %d chars, [a-zA-Z0-9_-])",
			rejected[0], maxModelName)
	}
	return client, listed, nil
}

// buildServer renders one server row with its tool count; secret
// values never appear, only the key names the row stores.
func buildServer(ctx context.Context, queries *db.Queries, row db.McpServer) (apitypes.McpServer, error) {
	count, err := queries.CountMcpServerTools(ctx, row.ID)
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
		ID:        row.ID,
		Name:      row.Name,
		Transport: row.Transport,
		Enabled:   row.Enabled != 0,
		ToolCount: toolCount,
	}
	if row.Command.Valid {
		out.Command = row.Command.String
	}
	if row.Url.Valid {
		out.URL = row.Url.String
	}
	if row.TimeoutMs.Valid {
		ms := row.TimeoutMs.Int64
		out.TimeoutMs = &ms
	}
	if row.LastError.Valid {
		msg := row.LastError.String
		out.LastError = &msg
	}
	if row.RefreshedAt.Valid {
		at := row.RefreshedAt.Int64
		out.RefreshedAt = &at
	}
	var err error
	if out.Args, err = decodeStrings(row.Args); err != nil {
		return apitypes.McpServer{}, fmt.Errorf("decode args for %q: %w", row.ID, err)
	}
	if out.EnvKeys, err = decodeStrings(row.EnvKeys); err != nil {
		return apitypes.McpServer{}, fmt.Errorf("decode envKeys for %q: %w", row.ID, err)
	}
	if out.HeaderKeys, err = decodeStrings(row.HeaderKeys); err != nil {
		return apitypes.McpServer{}, fmt.Errorf("decode headerKeys for %q: %w", row.ID, err)
	}
	return out, nil
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
// with the full effective secrets: a patch that replaces only env
// merges the stored headers in.
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

// encodeKeyNames writes a secrets map's key names, canonically
// sorted, as the column shape.
func encodeKeyNames(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	b, _ := json.Marshal(keys)
	return string(b)
}

// readServer maps the row read to 404 or a server fault.
func readServer(ctx context.Context, queries *db.Queries, w http.ResponseWriter, id string) (db.McpServer, error) {
	row, err := queries.GetMcpServer(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.McpServer{}, api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("MCP server %q not found", id))
		}
		_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
		return db.McpServer{}, fmt.Errorf("read mcp server %q: %w", id, err)
	}
	return row, nil
}
