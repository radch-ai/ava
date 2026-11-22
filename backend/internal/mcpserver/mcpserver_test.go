package mcpserver_test

// Catalog admission: bounded listings, cosmetic skips versus security
// rejections, the publish fence, pause/unpause semantics, and discovery
// recovery from a dead session.

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

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
func seedServer(t *testing.T, queries *db.Queries, id, url string) db.McpServer {
	t.Helper()
	if err := queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID: id, Name: id,
		Url:        sql.NullString{String: url, Valid: true},
		HeaderKeys: "[]",
		TimeoutMs:  sql.NullInt64{Int64: 5000, Valid: true},

		AllowPrivateNetwork: 1,
		AllowPlaintext:      1,
	}); err != nil {
		t.Fatalf("seed server %q: %v", id, err)
	}
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
	return mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, row)
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
	seedServer(t, queries, "helper", mcptest.Start(t).URL)

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

// Concurrent discovery passes on one server serialize: whichever
// finishes last leaves the catalog definition and the live client on
// the same generation.
func TestConcurrentDiscoverStaysCoherent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	srv := mcptest.Start(t)
	seedServer(t, queries, "helper", srv.URL)

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
			if _, _, err := mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, row); err != nil {
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
	seedServer(t, queries, "helper", good.URL)

	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("first discover: %v", err)
	}

	// Repoint the row at a catalog that exceeds the tool ceiling.
	huge := mcptest.Start(t, mcptest.WithToolCount(mcp.MaxCatalogTools+1))
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
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
	if rows, err := queries.ResolveAgentToolsDetailed(t.Context(), db.ResolveAgentToolsDetailedParams{
		AgentID: "a1", AgentID_2: "a1",
	}); err != nil || len(rows) != 0 {
		t.Fatalf("paused catalog resolved %v (err %v), want nothing", rows, err)
	}

	// A valid refresh publishes and unpauses.
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
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
	seedServer(t, queries, "bomb", srv.URL)

	_, _, err := discover(t, sqlDB, manager, store, "bomb")
	var catalogErr *mcp.CatalogError
	if !errors.As(err, &catalogErr) || catalogErr.Code != mcp.CodeCatalogInvalidSchema {
		t.Fatalf("deep-schema discover err = %v, want catalog_invalid_schema", err)
	}
	if state := catalogState(t, queries, "bomb"); state != mcpserver.CatalogPaused {
		t.Fatalf("catalog state = %q, want paused", state)
	}
	if tools, err := queries.ListMcpServerTools(t.Context(), "bomb"); err != nil || len(tools) != 0 {
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
	seedServer(t, queries, "a", mcptest.Start(t, mcptest.WithToolNames("b_c")).URL)
	seedServer(t, queries, "a_b", mcptest.Start(t, mcptest.WithToolNames("c")).URL)

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
	row := seedServer(t, queries, "helper", srv.URL)

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
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, "helper", cfg, listed); err == nil {
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
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, "helper", stale, listed); err == nil {
		t.Fatal("changed destination published a generation")
	}
	// Credential authority changed mid-flight.
	stale = cfg
	stale.Headers = map[string]string{"Authorization": "Bearer new"}
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, "helper", stale, listed); err == nil {
		t.Fatal("changed credential authority published a generation")
	}
	// The matching generation publishes.
	if err := mcpserver.UpsertListing(t.Context(), sqlDB, "helper", cfg, listed); err != nil {
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
	seedServer(t, queries, "helper", srv.URL)

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

// A transient network failure keeps the last validated generation active:
// only an observed security violation pauses the catalog.
func TestTransientFailureKeepsCatalogActive(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	store, manager := newStore(t, sqlDB), mcp.NewManager()
	t.Cleanup(manager.CloseAll)
	good := mcptest.Start(t)
	seedServer(t, queries, "helper", good.URL)

	if _, _, err := discover(t, sqlDB, manager, store, "helper"); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	if err := queries.UpdateMcpServer(t.Context(), db.UpdateMcpServerParams{
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
	seedServer(t, queries, "helper", srv.URL)

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
	for _, leaked := range []string{"sk-abcdefghijklmnop1234"} {
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
	row := seedServer(t, queries, "helper", srv.URL)

	if _, _, err := mcpserver.Discover(t.Context(), sqlDB, queries, manager, store, row); err != nil {
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
		Name:       row.Name,
		Url:        sql.NullString{String: srv.URL + "/moved", Valid: true},
		HeaderKeys: row.HeaderKeys, TimeoutMs: row.TimeoutMs,
		AllowPrivateNetwork: row.AllowPrivateNetwork, AllowPlaintext: row.AllowPlaintext,
		ID: "helper",
	}); err != nil {
		t.Fatalf("move row: %v", err)
	}
	if _, _, err := mcpserver.Publish(t.Context(), sqlDB, queries, row, client); err == nil {
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
	queries := db.New(sqlDB)
	store := newStore(t, sqlDB)
	srv := mcptest.Start(t)
	row := seedServer(t, queries, "helper", srv.URL)

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
	if _, _, err := mcpserver.Publish(t.Context(), sqlDB, queries, row, client); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := srv.Lists(); got != 1 {
		t.Fatalf("tools/list requests = %d, want 1 (the staged listing)", got)
	}
	if _, ok := client.InvokableByRemote("echo"); !ok {
		t.Fatal("publish did not commit the staged generation")
	}
}
