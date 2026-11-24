package mcpserver_test

// Catalog admission: bounded listings, cosmetic skips versus security
// rejections, the publish fence, pause/unpause semantics, and discovery
// recovery from a dead session.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/mcptest"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

var masterKey = []byte("0123456789abcdef0123456789abcdef")

// newStore builds the runtime vault over one test database.
func newStore(t *testing.T, sqlDB *sql.DB) *vault.Store {
	t.Helper()
	master, err := vault.NewMasterAEAD(masterKey)
	if err != nil {
		t.Fatalf("master AEAD: %v", err)
	}
	store, err := vault.NewStore(sqlDB, master)
	if err != nil {
		t.Fatalf("vault store: %v", err)
	}
	return store
}

// seedServer inserts one HTTP row pointing at url, approvals included so
// the loopback fixture is reachable.
func seedServer(t *testing.T, sqlDB *sql.DB, id, url string) db.McpServer {
	t.Helper()
	queries := db.New(sqlDB)
	testdb.SeedMcpServer(t, sqlDB, id, url, func(params *db.CreateMcpServerParams) {
		params.TimeoutMs = sql.NullInt64{Int64: 5000, Valid: true}
	})
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read server %q: %v", id, err)
	}
	return row
}

// discover runs one discovery pass for the row's current state.
func discover(t *testing.T, sqlDB *sql.DB, manager *mcp.Manager, store *vault.Store, id string) ([]mcp.ListedTool, []mcp.Skipped, error) {
	t.Helper()
	queries := db.New(sqlDB)
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read server %q: %v", id, err)
	}
	return mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, zerolog.Nop(), row)
}

// catalogState reads one row's admission state.
func catalogState(t *testing.T, queries *db.Queries, id string) string {
	t.Helper()
	row, err := queries.GetMcpServer(t.Context(), id)
	if err != nil {
		t.Fatalf("read server %q: %v", id, err)
	}
	return row.CatalogState
}

// A healthy pass publishes every tool with its definition, globally off,
// and records no diagnostics.
func TestDiscoverPublishesDefinitions(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	seedServer(t, sqlDB, "helper", mcptest.Start(t).URL)

	listed, skipped, err := discover(t, sqlDB, manager, store, "helper")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(skipped) != 0 || len(listed) != 1 {
		t.Fatalf("listed=%d skipped=%v, want one tool and no skips", len(listed), skipped)
	}
	tool, err := queries.GetTool(t.Context(), "helper:echo")
	if err != nil {
		t.Fatalf("read tool: %v", err)
	}
	if tool.Enabled != 0 {
		t.Fatalf("discovered tool enabled = %d, want 0 (the switch is opted in)", tool.Enabled)
	}
	if !strings.Contains(tool.DefinitionJson, `"inputSchema"`) {
		t.Fatalf("stored definition = %s", tool.DefinitionJson)
	}
	server, err := queries.GetMcpServer(t.Context(), "helper")
	if err != nil || server.LastError.Valid {
		t.Fatalf("healthy pass recorded diagnostics: %+v err %v", server.LastError, err)
	}
	if server.CatalogState != mcpserver.CatalogActive {
		t.Fatalf("catalog state = %q, want active", server.CatalogState)
	}
}

// A catalog larger than the 100-row upsert chunk publishes completely:
// both statements run, the prune sees every kept id, and first and last
// rows land in the catalog.
func TestUpsertListingChunksLargeCatalog(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	// 150 fixture tools plus the baseline echo: two upsert chunks.
	seedServer(t, sqlDB, "big", mcptest.Start(t, mcptest.WithToolCount(150)).URL)

	listed, skipped, err := discover(t, sqlDB, manager, store, "big")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(skipped) != 0 || len(listed) != 151 {
		t.Fatalf("listed=%d skipped=%v, want 151 tools and no skips", len(listed), skipped)
	}
	count, err := queries.CountMcpServerTools(t.Context(), sql.NullString{String: "big", Valid: true})
	if err != nil {
		t.Fatalf("count tools: %v", err)
	}
	if count != 151 {
		t.Fatalf("catalog holds %d tools, want 151", count)
	}
	for _, id := range []string{"big:tool-0", "big:tool-149"} {
		tool, err := queries.GetTool(t.Context(), id)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if tool.Kind != "mcp" || tool.Enabled != 0 {
			t.Fatalf("%s = kind %q enabled %d, want mcp and off", id, tool.Kind, tool.Enabled)
		}
	}
}

// Concurrent discovery passes on one server serialize: whichever
// finishes last leaves the catalog definition and the live client on
// the same generation.
func TestConcurrentDiscoverStaysCoherent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	seedServer(t, sqlDB, "helper", srv.URL)

	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("seed discover: %v", err)
	}
	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := queries.GetMcpServer(t.Context(), "helper")
			if err != nil {
				errs <- err
				return
			}
			if _, _, err := mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, zerolog.Nop(), row); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent discover: %v", err)
	}

	tool, err := queries.GetTool(t.Context(), "helper:echo")
	if err != nil {
		t.Fatalf("read tool: %v", err)
	}
	client, ok := manager.Get("helper")
	if !ok {
		t.Fatal("no live client")
	}
	live, ok := client.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not live")
	}
	info, err := live.Info(t.Context())
	if err != nil {
		t.Fatalf("live info: %v", err)
	}
	if tool.Description != info.Desc {
		t.Fatalf("catalog=%q live=%q: generations diverged", tool.Description, info.Desc)
	}
}

// An over-limit catalog is rejected as a generation: the catalog pauses,
// the previous rows stay inspectable, resolution returns nothing, and a
// valid refresh unpauses.
func TestOversizedCatalogPausesAndKeepsPriorRows(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	good := mcptest.Start(t)
	seedServer(t, sqlDB, "helper", good.URL)

	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("first discover: %v", err)
	}

	// Repoint the row at a catalog that exceeds the tool ceiling.
	huge := mcptest.Start(t, mcptest.WithToolCount(mcp.MaxCatalogTools+1))
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:                "helper",
		Url:                 sql.NullString{String: huge.URL, Valid: true},
		HeaderKeys:          "[]",
		TimeoutMs:           sql.NullInt64{Int64: 5000, Valid: true},
		AllowPrivateNetwork: 1, AllowPlaintext: 1, ID: "helper",
	}); err != nil {
		t.Fatalf("repoint row: %v", err)
	}

	_, _, err := discover(t, sqlDB, manager, store, "helper")
	var catalogErr *mcp.CatalogError
	if !errors.As(err, &catalogErr) || catalogErr.Code != mcp.CodeCatalogTooLarge {
		t.Fatalf("oversized discover err = %v, want catalog_too_large", err)
	}
	row, err := queries.GetMcpServer(t.Context(), "helper")
	if err != nil {
		t.Fatalf("read server: %v", err)
	}
	if row.CatalogState != mcpserver.CatalogPaused {
		t.Fatalf("catalog state = %q, want paused", row.CatalogState)
	}
	if !row.LastError.Valid || !strings.HasPrefix(row.LastError.String, mcp.CodeCatalogTooLarge) {
		t.Fatalf("last_error = %v, want the catalog_too_large code", row.LastError)
	}
	if _, err := queries.GetTool(t.Context(), "helper:echo"); err != nil {
		t.Fatalf("paused catalog lost its previous rows: %v", err)
	}
	if rows, err := queries.ResolveAgentToolsDetailed(t.Context(), "a1"); err != nil || len(rows) != 0 {
		t.Fatalf("paused catalog resolved %v (err %v), want nothing", rows, err)
	}

	// A valid refresh publishes and unpauses.
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:                "helper",
		Url:                 sql.NullString{String: good.URL, Valid: true},
		HeaderKeys:          "[]",
		TimeoutMs:           sql.NullInt64{Int64: 5000, Valid: true},
		AllowPrivateNetwork: 1, AllowPlaintext: 1, ID: "helper",
	}); err != nil {
		t.Fatalf("restore row: %v", err)
	}
	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("recover discover: %v", err)
	}
	if state := catalogState(t, queries, "helper"); state != mcpserver.CatalogActive {
		t.Fatalf("catalog state after recovery = %q, want active", state)
	}
}

// A schema bomb is security-invalid, not cosmetic: the whole generation
// is rejected and the catalog pauses.
func TestDeepSchemaRejectsGeneration(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t, mcptest.WithSchemaDepth(64))
	seedServer(t, sqlDB, "bomb", srv.URL)

	_, _, err := discover(t, sqlDB, manager, store, "bomb")
	var catalogErr *mcp.CatalogError
	if !errors.As(err, &catalogErr) || catalogErr.Code != mcp.CodeCatalogInvalidSchema {
		t.Fatalf("deep-schema discover err = %v, want catalog_invalid_schema", err)
	}
	if state := catalogState(t, queries, "bomb"); state != mcpserver.CatalogPaused {
		t.Fatalf("catalog state = %q, want paused", state)
	}
	if tools, err := queries.ListMcpServerTools(t.Context(), sql.NullString{String: "bomb", Valid: true}); err != nil || len(tools) != 0 {
		t.Fatalf("rejected generation published %d tools (err %v)", len(tools), err)
	}
}

// Two servers whose composite model names collide cannot both publish:
// the second generation is rejected, never silently renamed.
func TestModelNameCollisionRejectsGeneration(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	seedServer(t, sqlDB, "a", mcptest.Start(t, mcptest.WithToolNames("b_c")).URL)
	seedServer(t, sqlDB, "a_b", mcptest.Start(t, mcptest.WithToolNames("c")).URL)

	if _, _, err := discover(t, sqlDB, manager, store, "a"); err != nil {
		t.Fatalf("discover first server: %v", err)
	}
	_, _, err := discover(t, sqlDB, manager, store, "a_b")
	var catalogErr *mcp.CatalogError
	if !errors.As(err, &catalogErr) {
		t.Fatalf("colliding discover err = %v, want a catalog rejection", err)
	}
	if state := catalogState(t, queries, "a_b"); state != mcpserver.CatalogPaused {
		t.Fatalf("catalog state = %q, want paused", state)
	}
	if _, err := queries.GetTool(t.Context(), "a_b:c"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("colliding tool published: %v", err)
	}
}

// The publish fence rejects a generation whose row moved underneath it:
// a disabled server publishes nothing, a changed destination publishes
// nothing.
func TestPublishFenceRejectsStaleGeneration(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store := newStore(t, sqlDB)
	srv := mcptest.Start(t)
	row := seedServer(t, sqlDB, "helper", srv.URL)

	cfg, err := mcpserver.Config(t.Context(), row, store)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	listed := []mcp.ListedTool{{Remote: "echo", Description: "d", DefinitionJSON: `{}`}}

	// Disabled mid-flight.
	if _, err := queries.ToggleMcpServerEnabled(t.Context(), db.ToggleMcpServerEnabledParams{
		Enabled: 0, ID: "helper",
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", cfg, listed); err == nil {
		t.Fatal("disabled server published a generation")
	}
	if _, err := queries.GetTool(t.Context(), "helper:echo"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("fenced publish wrote a tool: %v", err)
	}
	if _, err := queries.ToggleMcpServerEnabled(t.Context(), db.ToggleMcpServerEnabledParams{
		Enabled: 1, ID: "helper",
	}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}

	// Destination changed mid-flight.
	stale := cfg
	stale.URL = "http://127.0.0.1:9/mcp"
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", stale, listed); err == nil {
		t.Fatal("changed destination published a generation")
	}
	// Credential authority changed mid-flight.
	stale = cfg
	stale.Headers = map[string]string{"Authorization": "Bearer new"}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", stale, listed); err == nil {
		t.Fatal("changed credential authority published a generation")
	}
	// Credential values rotated mid-flight: same key names, new values —
	// the fence unseals the stored set and compares it whole.
	blob, err := json.Marshal(mcpserver.Secrets{Headers: map[string]string{"Authorization": "Bearer old"}})
	if err != nil {
		t.Fatalf("encode secrets: %v", err)
	}
	if err := store.Put(t.Context(), vault.NameRuntime, mcpserver.EntryName("helper"), mcpserver.Kind, blob, nil); err != nil {
		t.Fatalf("seal secrets: %v", err)
	}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", stale, listed); err == nil {
		t.Fatal("rotated credential values published a generation")
	}
	// The stored values still publish.
	current := cfg
	current.Headers = map[string]string{"Authorization": "Bearer old"}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", current, listed); err != nil {
		t.Fatalf("current generation rejected: %v", err)
	}
}

// The fence's stdio arms: a process shape changed mid-flight — the
// command, the arguments, the env inheritance, or the sealed env values —
// publishes nothing.
func TestPublishFenceRejectsChangedProcess(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store := newStore(t, sqlDB)
	testdb.SeedMcpServer(t, sqlDB, "helper", "", func(params *db.CreateMcpServerParams) {
		params.Transport = "stdio"
		params.Command = sql.NullString{String: "/usr/bin/notes-server", Valid: true}
		params.Args = `["-y"]`
	})
	row, err := queries.GetMcpServer(t.Context(), "helper")
	if err != nil {
		t.Fatalf("read server: %v", err)
	}
	cfg, err := mcpserver.Config(t.Context(), row, store)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	listed := []mcp.ListedTool{{Remote: "echo", Description: "d", DefinitionJSON: `{}`}}

	stale := cfg
	stale.Command = "/usr/bin/other-server"
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", stale, listed); err == nil {
		t.Fatal("changed command published a generation")
	}
	stale = cfg
	stale.Args = []string{"-x"}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", stale, listed); err == nil {
		t.Fatal("changed args published a generation")
	}
	stale = cfg
	stale.InheritEnv = !stale.InheritEnv
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", stale, listed); err == nil {
		t.Fatal("changed env inheritance published a generation")
	}
	// Sealed env values rotated mid-flight: the fence unseals the
	// stored set and compares it whole, exactly as it does for headers.
	blob, err := json.Marshal(mcpserver.Secrets{Env: map[string]string{"NOTES_TOKEN": "old"}})
	if err != nil {
		t.Fatalf("encode secrets: %v", err)
	}
	if err := store.Put(t.Context(), vault.NameRuntime, mcpserver.EntryName("helper"), mcpserver.Kind, blob, nil); err != nil {
		t.Fatalf("seal secrets: %v", err)
	}
	stale = cfg
	stale.Env = map[string]string{"NOTES_TOKEN": "new"}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", stale, listed); err == nil {
		t.Fatal("rotated env values published a generation")
	}
	// The stored values still publish.
	current := cfg
	current.Env = map[string]string{"NOTES_TOKEN": "old"}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, store, "helper", current, listed); err != nil {
		t.Fatalf("current generation rejected: %v", err)
	}
}

// A cached session the remote no longer knows (a server restart) is
// replaced by one freshly validated connection: the next discovery pass
// recovers instead of reporting the dead session forever.
func TestDiscoveryReplacesDeadSession(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	seedServer(t, sqlDB, "helper", srv.URL)

	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	if got := srv.Initializes(); got != 1 {
		t.Fatalf("first discover dialed %d times, want 1", got)
	}

	// The server forgot the session: the cached connection is refused.
	srv.ExpireSession()
	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("recovering discover: %v", err)
	}
	if got := srv.Initializes(); got != 2 {
		t.Fatalf("recovering discover dialed %d times, want a replacement dial", got)
	}
	tool, err := queries.GetTool(t.Context(), "helper:echo")
	if err != nil {
		t.Fatalf("catalog lost its tool: %v", err)
	}
	if tool.Enabled != 0 {
		t.Fatalf("recovered tool enabled = %d, want the saved switch untouched (0)", tool.Enabled)
	}
	if state := catalogState(t, queries, "helper"); state != mcpserver.CatalogActive {
		t.Fatalf("catalog state = %q, want active", state)
	}
}

// A reconnect whose re-publish is rejected keeps the previous client
// serving from its committed generation: the replacement commits the
// catalog before it is installed, exactly like a verified publish.
func TestReconnectFailureKeepsServingClient(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	row := seedServer(t, sqlDB, "helper", srv.URL)

	if _, _, err := mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, zerolog.Nop(), row); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	live, ok := manager.Get("helper")
	if !ok {
		t.Fatal("discover did not install a client")
	}

	// The cached session dies, and the row moves before the replacement
	// publishes: the reconnect arm dials, the fence rejects, and the
	// previous client must survive the rejected replacement.
	srv.ExpireSession()
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:       row.Name,
		Url:        sql.NullString{String: srv.URL + "/moved", Valid: true},
		HeaderKeys: row.HeaderKeys, TimeoutMs: row.TimeoutMs,
		AllowPrivateNetwork: row.AllowPrivateNetwork, AllowPlaintext: row.AllowPlaintext,
		ID: "helper",
	}); err != nil {
		t.Fatalf("move row: %v", err)
	}
	if _, _, err := mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, zerolog.Nop(), row); err == nil {
		t.Fatal("reconnect past a moved row succeeded")
	}
	after, ok := manager.Get("helper")
	if !ok || after != live {
		t.Fatal("rejected re-publish replaced the live client")
	}
	if _, ok := after.InvokableByRemote("echo"); !ok {
		t.Fatal("previous generation stopped serving")
	}
}

// A transient network failure keeps the last validated generation active:
// only an observed security violation pauses the catalog.
func TestTransientFailureKeepsCatalogActive(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	good := mcptest.Start(t)
	seedServer(t, sqlDB, "helper", good.URL)

	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:                "helper",
		Url:                 sql.NullString{String: "http://127.0.0.1:1/mcp", Valid: true},
		HeaderKeys:          "[]",
		TimeoutMs:           sql.NullInt64{Int64: 500, Valid: true},
		AllowPrivateNetwork: 1, AllowPlaintext: 1, ID: "helper",
	}); err != nil {
		t.Fatalf("break row: %v", err)
	}
	manager.Close("helper")

	_, _, err := discover(t, sqlDB, manager, store, "helper")
	var catalogErr *mcp.CatalogError
	if err == nil || errors.As(err, &catalogErr) {
		t.Fatalf("dead endpoint err = %v, want a transient failure", err)
	}
	if state := catalogState(t, queries, "helper"); state != mcpserver.CatalogActive {
		t.Fatalf("catalog state = %q, want active after a transient failure", state)
	}
	if _, err := queries.GetTool(t.Context(), "helper:echo"); err != nil {
		t.Fatalf("transient failure pruned the last generation: %v", err)
	}
}

// Diagnostics carry a static code plus a sanitized, bounded detail —
// never an error body.
func TestDiagnosticsSanitizedAndCoded(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t, mcptest.WithStatus(500))
	seedServer(t, sqlDB, "helper", srv.URL)

	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err == nil {
		t.Fatal("500 endpoint discovered")
	}
	row, err := queries.GetMcpServer(t.Context(), "helper")
	if err != nil || !row.LastError.Valid {
		t.Fatalf("diagnostics missing: %+v err %v", row.LastError, err)
	}
	if !strings.HasPrefix(row.LastError.String, mcp.CodeTransient+":") {
		t.Fatalf("last_error = %q, want the transient code", row.LastError.String)
	}
	for _, leaked := range []string{mcptest.PatternCanary} {
		if strings.Contains(row.LastError.String, leaked) {
			t.Fatalf("diagnostics carry a raw credential: %q", row.LastError.String)
		}
	}
	if len(row.LastError.String) > 1200 {
		t.Fatalf("diagnostics not bounded: %d bytes", len(row.LastError.String))
	}
}

// Publication is staged — a failed catalog write leaves the previous
// committed implementation serving, not a replacement under an older
// approved definition.
func TestFailedUpsertKeepsOldGeneration(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	row := seedServer(t, sqlDB, "helper", srv.URL)

	if _, _, err := mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, zerolog.Nop(), row); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	client, ok := manager.Get("helper")
	if !ok {
		t.Fatal("discover did not install a client")
	}
	before, ok := client.InvokableByRemote("echo")
	if !ok {
		t.Fatal("echo not cached after discovery")
	}
	beforeInfo, err := before.Info(t.Context())
	if err != nil {
		t.Fatalf("info: %v", err)
	}

	// The endpoint advertises a new definition, then the row moves before
	// the publish: the fence rejects the generation.
	srv.SetEcho("changed description", `{"type":"object","properties":{"text":{"type":"string"}}}`)
	if _, _, err := client.SyncStaged(t.Context()); err != nil {
		t.Fatalf("staged sync: %v", err)
	}
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:       row.Name,
		Url:        sql.NullString{String: srv.URL + "/moved", Valid: true},
		HeaderKeys: row.HeaderKeys, TimeoutMs: row.TimeoutMs,
		AllowPrivateNetwork: row.AllowPrivateNetwork, AllowPlaintext: row.AllowPlaintext,
		ID: "helper",
	}); err != nil {
		t.Fatalf("move row: %v", err)
	}
	if _, _, err := mcpserver.Publish(t.Context(), sqlDB, store, row, client); err == nil {
		t.Fatal("publish past a moved row succeeded")
	}
	if _, _, ok := client.StagedListing(); ok {
		t.Fatal("rejected generation stayed staged")
	}
	after, ok := client.InvokableByRemote("echo")
	if !ok {
		t.Fatal("failed publish dropped the live tool")
	}
	afterInfo, err := after.Info(t.Context())
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if afterInfo.Desc != beforeInfo.Desc {
		t.Fatalf("live description = %q, want the previous generation %q",
			afterInfo.Desc, beforeInfo.Desc)
	}
}

// A staged listing is published instead of listing the server twice.
func TestPublishReusesStagedListing(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	store := newStore(t, sqlDB)
	srv := mcptest.Start(t)
	row := seedServer(t, sqlDB, "helper", srv.URL)

	cfg, err := mcpserver.Config(t.Context(), row, store)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	client, err := mcp.BoundedDial(t.Context(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, _, err := client.SyncStaged(t.Context()); err != nil {
		t.Fatalf("staged sync: %v", err)
	}
	if _, _, err := mcpserver.Publish(t.Context(), sqlDB, store, row, client); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := srv.Lists(); got != 1 {
		t.Fatalf("tools/list requests = %d, want 1 (the staged listing)", got)
	}
	if _, ok := client.InvokableByRemote("echo"); !ok {
		t.Fatal("publish did not commit the staged generation")
	}
}

// A verified publish commits the catalog before installing the
// candidate: a rejected generation leaves the manager untouched — the
// previous client keeps serving — and the candidate stays uninstalled.
func TestPublishVerifiedRejectKeepsServingClient(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	row := seedServer(t, sqlDB, "helper", srv.URL)

	if _, _, err := mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, zerolog.Nop(), row); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	live, ok := manager.Get("helper")
	if !ok {
		t.Fatal("discover did not install a client")
	}

	cfg, err := mcpserver.Config(t.Context(), row, store)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	candidate, err := mcp.BoundedDial(t.Context(), cfg)
	if err != nil {
		t.Fatalf("dial candidate: %v", err)
	}
	t.Cleanup(func() { _ = candidate.Close() })
	if _, _, err := candidate.SyncStaged(t.Context()); err != nil {
		t.Fatalf("stage candidate: %v", err)
	}

	// The row moves before the publish: the fence rejects the generation.
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
		Transport: "http", Args: "[]", EnvKeys: "[]",
		Name:       row.Name,
		Url:        sql.NullString{String: srv.URL + "/moved", Valid: true},
		HeaderKeys: row.HeaderKeys, TimeoutMs: row.TimeoutMs,
		AllowPrivateNetwork: row.AllowPrivateNetwork, AllowPlaintext: row.AllowPlaintext,
		ID: "helper",
	}); err != nil {
		t.Fatalf("move row: %v", err)
	}
	if _, _, err := mcpserver.PublishVerified(t.Context(), sqlDB, store, manager, row, candidate); err == nil {
		t.Fatal("verified publish past a moved row succeeded")
	}
	if _, _, ok := candidate.StagedListing(); ok {
		t.Fatal("rejected candidate stayed staged")
	}
	after, ok := manager.Get("helper")
	if !ok || after != live {
		t.Fatal("rejected publish replaced the live client")
	}
	if _, ok := after.InvokableByRemote("echo"); !ok {
		t.Fatal("previous generation stopped serving")
	}
}
