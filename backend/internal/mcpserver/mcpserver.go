// Package mcpserver syncs MCP server rows into the tool catalog:
// connect, discover, upsert, prune, diagnostics. Boot and the API's
// create/patch/refresh paths both write through it, so catalog state
// is identical wherever it starts. A server that is down degrades to
// an inert listing.
package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/security"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// Kind is the vault kind of an MCP server's secrets entry.
const Kind = "mcp-server"

// Secrets is the sealed blob under runtime/{EntryName}: one entry per
// server. Headers ride http connections, Env values reach a stdio child;
// one blob carries whichever the server uses.
type Secrets struct {
	Headers map[string]string `json:"headers"`
	Env     map[string]string `json:"env"`
}

// EntryName is the runtime-vault entry holding a server's secrets.
func EntryName(slug string) string { return "mcp-server/" + slug }

// bootDiscoveryParallel bounds concurrent boot dials: one hung or slow
// server costs its own handshake timeout, never a serial queue of them.
const bootDiscoveryParallel = 4

// SyncEnabled runs discovery for every enabled server at boot, bounded
// parallel. Only a broken catalog read fails the boot; per-server
// failures land in the row's diagnostics.
func SyncEnabled(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, vaultStore *vault.Store, logger zerolog.Logger) error {
	servers, err := queries.ListMcpServers(ctx)
	if err != nil {
		return fmt.Errorf("list mcp servers: %w", err)
	}
	var group errgroup.Group
	group.SetLimit(bootDiscoveryParallel)
	for _, server := range servers {
		if server.Enabled == 0 {
			// Kill switch: tools stay listed but inert, and this boot
			// does not touch the row's diagnostics.
			continue
		}
		group.Go(func() error {
			// One bounded pass per server: initialization, pagination and
			// every protocol exchange share the discovery budget.
			discoverCtx, cancel := context.WithTimeout(ctx, mcp.DiscoveryTimeout)
			defer cancel()
			if _, _, err := Discover(discoverCtx, sqlDB, queries, manager, vaultStore, logger, server); err != nil {
				logger.Warn().Err(err).Str("server", server.ID).Msg("mcp discovery failed")
			}
			return nil
		})
	}
	return group.Wait()
}

// Discover connects one server (cached when unchanged), publishes its
// listing into the catalog, prunes rows it no longer reports — agent
// opt-outs cascade away with them — and records diagnostics. Errors are returned
// for the caller to answer or log; the row always explains itself.
//
// A security-rejected or over-limit generation pauses the catalog: the
// previous tool rows stay inspectable, new calls resolve nothing, and a
// valid refresh unpauses. A transient failure leaves the
// last validated generation active.
func Discover(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, vaultStore *vault.Store, logger zerolog.Logger, server db.McpServer) ([]mcp.ListedTool, []mcp.Skipped, error) {
	defer pubLocks.Lock(server.ID)()
	listed, skipped, err := discover(ctx, sqlDB, queries, manager, vaultStore, server)
	if err != nil {
		return nil, nil, RecordPublishFailure(ctx, queries, server.ID, err)
	}
	// The listing is committed and serving: a diagnostics write that
	// fails after it degrades the row's explanation, not the publish.
	// Only pre-publish failures reach the caller as errors.
	if diagErr := SetDiagnostics(ctx, queries, server.ID, nil); diagErr != nil {
		logger.Warn().Err(diagErr).Str("server", server.ID).
			Msg("mcp diagnostics write failed after a committed publish")
	}
	return listed, skipped, nil
}

// isCatalogRejection reports whether err rejects the generation itself
// (as opposed to a transient network failure): the catalog must pause.
func isCatalogRejection(err error) bool {
	var catalog *mcp.CatalogError
	return errors.As(err, &catalog)
}

// RecordPublishFailure leaves the row explaining a failed publication:
// a rejected generation pauses the catalog — the previous tool rows stay
// inspectable, new calls resolve nothing, and a valid listing unpauses —
// and every failure records its diagnostics. The error is returned for
// the caller to answer.
func RecordPublishFailure(ctx context.Context, queries *db.Queries, id string, err error) error {
	if isCatalogRejection(err) {
		if stateErr := SetCatalogState(ctx, queries, id, CatalogPaused); stateErr != nil {
			err = errors.Join(err, stateErr)
		}
	}
	if diagErr := SetDiagnostics(ctx, queries, id, err); diagErr != nil {
		return errors.Join(err, diagErr)
	}
	return err
}

// errStaging marks a failure to read a server's listing: the connection
// is at fault (a dropped session, a restarted or unreachable server), not
// the catalog generation or the database.
var errStaging = errors.New("mcp listing unavailable")

// discover is one discovery pass: connect, then publish. A cached session
// the remote no longer knows is replaced by one freshly validated
// connection and published: a restarted server must recover on the next
// refresh rather than explain itself forever.
func discover(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, vaultStore *vault.Store, server db.McpServer) ([]mcp.ListedTool, []mcp.Skipped, error) {
	cfg, err := Config(ctx, server, vaultStore)
	if err != nil {
		return nil, nil, err
	}
	client, err := manager.Connect(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	listed, skipped, err := Publish(ctx, sqlDB, vaultStore, server, client)
	if err == nil {
		return listed, skipped, nil
	}
	// A rejected generation is not a connection fault: the remote answered
	// with a listing the catalog refuses, so reconnecting would only list
	// the same rejection again. The healthy client stays installed and its
	// previous generation keeps serving inspection.
	var catalogErr *mcp.CatalogError
	if errors.As(err, &catalogErr) {
		return nil, nil, err
	}
	if !errors.Is(err, errStaging) || ctx.Err() != nil {
		return nil, nil, err
	}
	replacement, dialErr := mcp.BoundedDial(ctx, cfg)
	if dialErr != nil {
		return nil, nil, errors.Join(err, fmt.Errorf("reconnect mcp server %s: %w", server.ID, dialErr))
	}
	// The catalog write commits before the replacement is installed —
	// the same order PublishVerified holds: a failed re-publish keeps
	// the previous client answering from its committed generation (dead
	// session, live cache) instead of stranding an empty replacement.
	// Discover holds this server's pubLock across the whole pass, so
	// the publish and the Replace below are serialized as one critical
	// section exactly like PublishVerified's.
	listed, skipped, pubErr := Publish(ctx, sqlDB, vaultStore, server, replacement)
	if pubErr != nil {
		_ = replacement.Close()
		return nil, nil, pubErr
	}
	// Replace retires the dead client without aborting its dispatched
	// work (Adopt would keep it: the config is equivalent).
	manager.Replace(cfg, replacement)
	return listed, skipped, nil
}

// Publish writes one already-connected client's listing into the catalog
// as a coherent generation: the definition, the implementation and
// the authority are accepted together. A staged listing from verification
// is reused rather than listed twice; the client's live cache only
// advances after the catalog write succeeds, so a failed or overlapping
// publication never exposes a replacement implementation under an older
// approved definition. The client's config is the authority the write
// fence re-checks, values included (vaultStore unseals the stored set).
func Publish(ctx context.Context, sqlDB *sql.DB, vaultStore *vault.Store, server db.McpServer, client *mcp.Client) ([]mcp.ListedTool, []mcp.Skipped, error) {
	return publishStaged(ctx, sqlDB, vaultStore, server.ID, client)
}

// publishStaged stages the client's listing when it carries none, commits
// the catalog generation, and only then makes it live. A rejected
// generation discards the staging and leaves the previous one serving:
// nothing is live that the catalog did not accept. The client's own
// config is the single authority the fence validates.
func publishStaged(ctx context.Context, sqlDB *sql.DB, vaultStore *vault.Store, serverID string, client *mcp.Client) ([]mcp.ListedTool, []mcp.Skipped, error) {
	listed, skipped, staged := client.StagedListing()
	if !staged {
		var err error
		listed, skipped, err = client.SyncStaged(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", errStaging, err)
		}
	}
	if err := UpsertListing(ctx, sqlDB, vaultStore, serverID, client.Config(), listed); err != nil {
		// Nothing was committed: the live generation keeps serving.
		client.Discard()
		return nil, nil, err
	}
	client.Commit()
	return listed, skipped, nil
}

// pubLocks serializes catalog publication per server id: staging, the
// catalog transaction, and the manager install advance as one critical
// section, so the published definition and the live implementation
// never come from different generations. Entries persist for the
// process lifetime, bounded by distinct server slugs (same trade as
// api.Throttle's key map).
var pubLocks = keyedMutex{keys: map[string]*sync.Mutex{}}

// keyedMutex hands out one mutex per key.
type keyedMutex struct {
	mu   sync.Mutex
	keys map[string]*sync.Mutex
}

// Lock acquires key's mutex and returns its release.
func (k *keyedMutex) Lock(key string) func() {
	k.mu.Lock()
	lock, ok := k.keys[key]
	if !ok {
		lock = &sync.Mutex{}
		k.keys[key] = lock
	}
	k.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// PublishVerified publishes the listing a verification client staged and
// installs that client as one critical section: the catalog generation
// the database accepts and the client the manager keeps come from the
// same pass, and the pass lists the catalog exactly once. The catalog
// write commits first: a rejected publication leaves the manager
// untouched — the previous generation keeps serving — and ownership of
// the client stays with the caller, who must close it. Only a committed
// generation transfers ownership, and Replace then retires the previous
// client by draining it, so work already on the wire finishes.
func PublishVerified(ctx context.Context, sqlDB *sql.DB, vaultStore *vault.Store, manager *mcp.Manager, server db.McpServer, client *mcp.Client) ([]mcp.ListedTool, []mcp.Skipped, error) {
	defer pubLocks.Lock(server.ID)()
	listed, skipped, err := publishStaged(ctx, sqlDB, vaultStore, server.ID, client)
	if err != nil {
		// Nothing committed, nothing installed: the candidate's staged
		// generation is dropped and the live one keeps serving.
		return nil, nil, err
	}
	manager.Replace(client.Config(), client)
	return listed, skipped, nil
}

// Catalog states.
const (
	CatalogActive = "active"
	CatalogPaused = "paused"
)

// UpsertListing publishes one server's listing: it revalidates the
// server's current configuration and authority inside the write
// transaction (the discovery fence), rejects model-name collisions with
// another server, then upserts every tool with its definition, prunes what
// the server no longer reports, and marks the
// catalog active — one transaction, so readers (the catalog page, a
// turn's tool resolution) never see a torn or half-published listing.
func UpsertListing(ctx context.Context, sqlDB *sql.DB, vaultStore *vault.Store, serverID string, cfg mcp.ServerConfig, listed []mcp.ListedTool) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin listing sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	queries := db.New(tx)

	row, err := queries.GetMcpServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("re-read server %q for publish: %w", serverID, err)
	}
	if row.Enabled == 0 {
		return errStaleGeneration{serverID: serverID, reason: "disabled during discovery"}
	}
	if !sameAuthority(row, cfg) {
		return errStaleGeneration{serverID: serverID, reason: "configuration changed during discovery"}
	}
	// Credential values, not just key names: a rotation between
	// discovery and this write must reject the generation. The blob is
	// unsealed through this transaction — same connection, same
	// snapshot as the row read above.
	var storedHeaders, storedEnv map[string]string
	blob, _, err := vaultStore.GetThrough(ctx, queries, vault.NameRuntime, EntryName(serverID))
	switch {
	case errors.Is(err, vault.ErrNotFound):
	case err != nil:
		return fmt.Errorf("unseal stored credentials to fence %q: %w", serverID, err)
	default:
		var secrets Secrets
		if err := json.Unmarshal(blob, &secrets); err != nil {
			return fmt.Errorf("decode stored credentials to fence %q: %w", serverID, err)
		}
		storedHeaders = secrets.Headers
		storedEnv = secrets.Env
	}
	if !maps.Equal(storedHeaders, cfg.Headers) || !maps.Equal(storedEnv, cfg.Env) {
		return errStaleGeneration{serverID: serverID, reason: "credentials changed during discovery"}
	}

	others, err := queries.ListOtherServerToolNames(ctx, sql.NullString{String: serverID, Valid: true})
	if err != nil {
		return fmt.Errorf("list sibling tool names: %w", err)
	}
	taken := make(map[string]string, len(others))
	for _, other := range others {
		if !other.ServerID.Valid {
			continue
		}
		server := other.ServerID.String
		taken[mcp.ModelName(server, other.Name)] = server
	}
	for _, tool := range listed {
		if owner, clash := taken[mcp.ModelName(serverID, tool.Remote)]; clash {
			return &mcp.CatalogError{
				Code: mcp.CodeCatalogInvalidSchema,
				Err: fmt.Errorf("model tool name %q already belongs to server %q",
					mcp.ModelName(serverID, tool.Remote), owner),
			}
		}
	}

	// Chunked multi-row upsert: 1,024 single inserts cost ~50ms of
	// round-trips per refresh; 100-row statements (500 bind params,
	// under SQLite's 999 limit) cut that by an order of magnitude.
	// sqlc cannot express variable-arity VALUES, so the statement is
	// built here — every value is a bound parameter, never spliced.
	kept := make([]string, 0, len(listed))
	const chunkRows = 100
	for start := 0; start < len(listed); start += chunkRows {
		end := min(start+chunkRows, len(listed))
		rows := listed[start:end]
		var stmt strings.Builder
		stmt.WriteString("INSERT INTO tools (id, name, description, enabled, kind, server_id, definition_json) VALUES ")
		args := make([]any, 0, len(rows)*5)
		for i, tool := range rows {
			if i > 0 {
				stmt.WriteByte(',')
			}
			stmt.WriteString("(?, ?, ?, 0, 'mcp', ?, ?)")
			id := mcp.CatalogID(serverID, tool.Remote)
			args = append(args, id, tool.Remote, tool.Description, serverID, tool.DefinitionJSON)
			kept = append(kept, id)
		}
		stmt.WriteString(" ON CONFLICT(id) DO UPDATE SET name = excluded.name, description = excluded.description, definition_json = excluded.definition_json")
		if _, err := tx.ExecContext(ctx, stmt.String(), args...); err != nil {
			return fmt.Errorf("upsert tools %d..%d for %q: %w", start, end-1, serverID, err)
		}
	}
	keptJSON, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("encode kept ids: %w", err)
	}
	if _, err := queries.DeleteVanishedMcpTools(ctx, db.DeleteVanishedMcpToolsParams{
		ServerID: sql.NullString{String: serverID, Valid: true},
		KeptIds:  string(keptJSON),
	}); err != nil {
		return fmt.Errorf("prune vanished tools: %w", err)
	}
	if _, err := queries.SetMcpServerState(ctx, db.SetMcpServerStateParams{
		CatalogState: CatalogActive, ID: serverID,
	}); err != nil {
		return fmt.Errorf("activate catalog %q: %w", serverID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit listing sync: %w", err)
	}
	return nil
}

// errStaleGeneration is the fence's rejection: the row moved under the
// discovery pass, so nothing is published.
type errStaleGeneration struct {
	serverID string
	reason   string
}

// Error explains which fence arm rejected the publish.
func (e errStaleGeneration) Error() string {
	return "mcp server " + e.serverID + ": stale discovery generation (" + e.reason + ")"
}

// sameAuthority reports whether the row still describes the connection the
// discovery pass actually talked to: transport, endpoint or process shape,
// approvals, environment inheritance and effective timeout. Credential
// values are fenced separately, inside the write transaction, against the
// unsealed stored set.
func sameAuthority(row db.McpServer, cfg mcp.ServerConfig) bool {
	if row.Transport != cfg.Transport {
		return false
	}
	if row.Url.String != cfg.URL || row.Url.Valid != (cfg.URL != "") {
		return false
	}
	if row.Command.String != cfg.Command || row.Command.Valid != (cfg.Command != "") {
		return false
	}
	args, err := DecodeStrings(row.Args)
	if err != nil || !slices.Equal(args, cfg.Args) {
		return false
	}
	if (row.InheritEnv != 0) != cfg.InheritEnv {
		return false
	}
	if (row.AllowPrivateNetwork != 0) != cfg.AllowPrivateNetwork {
		return false
	}
	if (row.AllowPlaintext != 0) != cfg.AllowPlaintext {
		return false
	}
	var timeout time.Duration
	if row.TimeoutMs.Valid && row.TimeoutMs.Int64 > 0 {
		timeout = time.Duration(row.TimeoutMs.Int64) * time.Millisecond
	}
	return timeout == cfg.Timeout
}

// HeaderKeys renders a secrets map's key names the way the row stores
// them: canonically sorted. EnvKeys is the same rendering for a stdio
// server's sealed environment.
func HeaderKeys(headers map[string]string) string { return keysJSON(headers) }

// EnvKeys is HeaderKeys for the sealed environment.
func EnvKeys(env map[string]string) string { return keysJSON(env) }

// keysJSON encodes a secrets map's key names, sorted and always an array.
func keysJSON(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return EncodeStrings(keys)
}

// DecodeStrings parses a JSON string-array column into a non-nil slice:
// the one decoder for args, env_keys and header_keys.
func DecodeStrings(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// EncodeStrings is DecodeStrings' inverse: the column form of a string
// list, always a JSON array (nil encodes as "[]").
func EncodeStrings(values []string) string {
	if values == nil {
		values = []string{}
	}
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

// SetCatalogState writes the catalog admission state alone.
func SetCatalogState(ctx context.Context, queries *db.Queries, id, state string) error {
	if _, err := queries.SetMcpServerState(ctx, db.SetMcpServerStateParams{
		CatalogState: state, ID: id,
	}); err != nil {
		return fmt.Errorf("set catalog state %q for %q: %w", state, id, err)
	}
	return nil
}

// Config resolves one server row into a connection config, unsealing
// the secrets blob. A missing blob is an honestly empty config, not an
// error: servers without secrets never seal one.
func Config(ctx context.Context, server db.McpServer, vaultStore *vault.Store) (mcp.ServerConfig, error) {
	cfg := mcp.ServerConfig{
		ID:                  server.ID,
		Transport:           server.Transport,
		InheritEnv:          server.InheritEnv != 0,
		AllowPrivateNetwork: server.AllowPrivateNetwork != 0,
		AllowPlaintext:      server.AllowPlaintext != 0,
	}
	if server.Command.Valid {
		cfg.Command = server.Command.String
	}
	args, err := DecodeStrings(server.Args)
	if err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("decode args: %w", err)
	}
	cfg.Args = args
	if server.Url.Valid {
		cfg.URL = server.Url.String
	}
	if server.TimeoutMs.Valid && server.TimeoutMs.Int64 > 0 {
		cfg.Timeout = time.Duration(server.TimeoutMs.Int64) * time.Millisecond
	}

	blob, _, err := vaultStore.Get(ctx, vault.NameRuntime, EntryName(server.ID))
	if errors.Is(err, vault.ErrNotFound) {
		return cfg, nil
	}
	if err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("unseal secrets: %w", err)
	}
	var secrets Secrets
	if err := json.Unmarshal(blob, &secrets); err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("decode secrets: %w", err)
	}
	cfg.Headers = secrets.Headers
	cfg.Env = secrets.Env
	return cfg, nil
}

// maxDiagnosticDetail bounds the sanitized human-readable detail stored
// beside a reason code: enough to act on, never a payload.
const maxDiagnosticDetail = 1 << 10

// SetDiagnostics records the post-discovery state: a static reason code
// plus a sanitized, bounded detail (NULL = healthy). The message never
// carries a raw error body or a secret.
func SetDiagnostics(ctx context.Context, queries *db.Queries, id string, lastErr error) error {
	params := db.SetMcpServerDiagnosticsParams{ID: id}
	if lastErr != nil {
		params.LastError = sql.NullString{String: diagnosticMessage(lastErr), Valid: true}
	}
	return queries.SetMcpServerDiagnostics(ctx, params)
}

// diagnosticMessage renders "code:detail" for one failure: the code is a
// static class, the detail is scrubbed and truncated.
func diagnosticMessage(err error) string {
	code, detail := Classify(err)
	detail, _ = security.NewPatternScrubber().ScrubString(detail)
	detail = truncateUTF8(detail, maxDiagnosticDetail)
	if detail == "" {
		return code
	}
	return code + ": " + detail
}

// Classify maps a discovery or connect failure to its static reason code
// and the underlying detail text.
func Classify(err error) (code, detail string) {
	var catalog *mcp.CatalogError
	var stale errStaleGeneration
	switch {
	case errors.As(err, &catalog):
		return catalog.Code, err.Error()
	case errors.As(err, &stale):
		return mcp.CodeTransient, err.Error()
	case errors.Is(err, mcp.ErrInsecureCredentials):
		return mcp.CodeInsecureCredentials, err.Error()
	case errors.Is(err, mcp.ErrResponseTooLarge):
		return mcp.CodeResponseTooLarge, err.Error()
	case errors.Is(err, security.ErrLinkLocalDenied),
		errors.Is(err, security.ErrReservedDenied),
		errors.Is(err, security.ErrUnspecifiedDenied),
		errors.Is(err, security.ErrPrivateDenied),
		errors.Is(err, security.ErrUnresolved):
		return mcp.CodeDestinationDenied, err.Error()
	default:
		return mcp.CodeTransient, err.Error()
	}
}

// truncateUTF8 clips s to at most n bytes on a rune boundary.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
