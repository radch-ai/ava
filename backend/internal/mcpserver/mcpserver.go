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
	"slices"
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
// server. Headers are the only attachment the connection applies.
type Secrets struct {
	Headers map[string]string `json:"headers"`
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
			if _, _, err := Discover(discoverCtx, sqlDB, queries, manager, vaultStore, server); err != nil {
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
func Discover(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, vaultStore *vault.Store, server db.McpServer) ([]mcp.ListedTool, []mcp.Skipped, error) {
	defer pubLocks.Lock(server.ID)()
	listed, skipped, err := discover(ctx, sqlDB, queries, manager, vaultStore, server)
	if err != nil {
		return nil, nil, RecordPublishFailure(ctx, queries, server.ID, err)
	}
	return listed, skipped, SetDiagnostics(ctx, queries, server.ID, nil)
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
	listed, skipped, err := Publish(ctx, sqlDB, queries, server, client)
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
	// Replace retires the dead client without aborting its dispatched
	// work (Adopt would keep it: the config is equivalent); publication
	// re-fences the row before anything is written.
	return Publish(ctx, sqlDB, queries, server, manager.Replace(cfg, replacement))
}

// Publish writes one already-connected client's listing into the catalog
// as a coherent generation: the definition, the implementation and
// the authority are accepted together. A staged listing from verification
// is reused rather than listed twice; the client's live cache only
// advances after the catalog write succeeds, so a failed or overlapping
// publication never exposes a replacement implementation under an older
// approved definition. The client's config is the authority the write
// fence re-checks.
func Publish(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, server db.McpServer, client *mcp.Client) ([]mcp.ListedTool, []mcp.Skipped, error) {
	listed, skipped, staged := client.StagedListing()
	if !staged {
		var err error
		listed, skipped, err = client.SyncStaged(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", errStaging, err)
		}
	}
	if err := UpsertListing(ctx, sqlDB, server.ID, client.Config(), listed); err != nil {
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

// PublishVerified installs a verification client and publishes the
// listing it staged as one critical section: the client the manager keeps
// and the catalog generation the database accepts come from the same
// pass, and the pass lists the catalog exactly once.
//
// The verified client wins over a cached one even for an equivalent
// config: Adopt would keep the cache, whose committed generation may be
// older than the listing verification just validated, and publishing the
// fresh listing under the older implementation would split definition
// from implementation. Replace retires the previous client by draining
// it, so work already on the wire finishes. Ownership of client
// transfers either way.
func PublishVerified(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, manager *mcp.Manager, server db.McpServer, cfg mcp.ServerConfig, client *mcp.Client) ([]mcp.ListedTool, []mcp.Skipped, error) {
	defer pubLocks.Lock(server.ID)()
	return Publish(ctx, sqlDB, queries, server, manager.Replace(cfg, client))
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
func UpsertListing(ctx context.Context, sqlDB *sql.DB, serverID string, cfg mcp.ServerConfig, listed []mcp.ListedTool) error {
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

	others, err := queries.ListOtherServerToolNames(ctx, sql.NullString{String: serverID, Valid: true})
	if err != nil {
		return fmt.Errorf("list sibling tool names: %w", err)
	}
	taken := make(map[string]string, len(others))
	for _, other := range others {
		server, ok := other.ServerID.(string)
		if !ok {
			continue
		}
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

	kept := make([]string, 0, len(listed))
	for _, tool := range listed {
		id := mcp.CatalogID(serverID, tool.Remote)
		if err := queries.UpsertMcpTool(ctx, db.UpsertMcpToolParams{
			ID:             id,
			Name:           tool.Remote,
			Description:    tool.Description,
			ServerID:       serverID,
			DefinitionJson: tool.DefinitionJSON,
		}); err != nil {
			return fmt.Errorf("upsert tool %q: %w", id, err)
		}
		kept = append(kept, id)
	}
	keptJSON, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("encode kept ids: %w", err)
	}
	if _, err := queries.DeleteVanishedMcpTools(ctx, db.DeleteVanishedMcpToolsParams{
		ServerID: serverID,
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

// sameAuthority reports whether the row still describes the endpoint the
// discovery pass actually talked to: destination, approvals, credential
// key set and effective timeout.
func sameAuthority(row db.McpServer, cfg mcp.ServerConfig) bool {
	if row.Url.String != cfg.URL || row.Url.Valid != (cfg.URL != "") {
		return false
	}
	if (row.AllowPrivateNetwork != 0) != cfg.AllowPrivateNetwork {
		return false
	}
	if (row.AllowPlaintext != 0) != cfg.AllowPlaintext {
		return false
	}
	if row.HeaderKeys != headerKeysJSON(cfg.Headers) {
		return false
	}
	var timeout time.Duration
	if row.TimeoutMs.Valid && row.TimeoutMs.Int64 > 0 {
		timeout = time.Duration(row.TimeoutMs.Int64) * time.Millisecond
	}
	return timeout == cfg.Timeout
}

// headerKeysJSON renders a credential map's key names the way the row
// stores them: canonically sorted.
func headerKeysJSON(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	encoded, _ := json.Marshal(keys)
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
		AllowPrivateNetwork: server.AllowPrivateNetwork != 0,
		AllowPlaintext:      server.AllowPlaintext != 0,
	}
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
	return cfg, nil
}

// maxDiagnosticDetail bounds the sanitized human-readable detail stored
// beside a reason code: enough to act on, never a payload.
const maxDiagnosticDetail = 1 << 10

// SetDiagnostics records the post-discovery state: a static reason code
// plus a sanitized, bounded detail (NULL = healthy) and when the sync
// attempt ran. The message never carries a raw error body or a secret.
func SetDiagnostics(ctx context.Context, queries *db.Queries, id string, lastErr error) error {
	params := db.SetMcpServerDiagnosticsParams{
		ID:          id,
		RefreshedAt: sql.NullInt64{Int64: time.Now().UnixMicro(), Valid: true},
	}
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
