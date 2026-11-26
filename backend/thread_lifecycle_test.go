package backend_test

// Thread lifecycle pins the agent's active pointer: a command item's
// switch creates the next thread, links it to its predecessor, and swaps
// the pointer in one transaction; replay reads only the active thread.

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// switchThread admits and starts one command item, then runs its durable
// switch.
func switchThread(t *testing.T, svc *backend.TurnService, agentID, itemID string) (*backend.ActivatedThread, error) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.AcceptItem(ctx, agentID, itemID, "/new", "new_thread", nil); err != nil {
		t.Fatalf("accept %s: %v", itemID, err)
	}
	if _, _, err := svc.StartItem(ctx, agentID, itemID); err != nil {
		t.Fatalf("start %s: %v", itemID, err)
	}
	activated, _, err := svc.ExecuteThreadSwitch(ctx, agentID, itemID)
	return activated, err
}

func TestExecuteThreadSwitchSwapsPointer(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "lc1", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	first := activeThread(t, sqlDB, "lc1")
	if first != testdb.ThreadID("lc1") {
		t.Fatalf("active thread = %q, want the seeded %q", first, testdb.ThreadID("lc1"))
	}
	// History in the old thread stays where it is.
	startedItem(t, svc, "lc1", "item-1", "old")

	freshRes, err := switchThread(t, svc, "lc1", "cmd-1")
	if err != nil {
		t.Fatalf("ExecuteThreadSwitch: %v", err)
	}
	fresh := freshRes.ID
	if fresh == first || fresh == "" {
		t.Fatalf("new thread = %q, want a fresh id distinct from %q", fresh, first)
	}
	if active := activeThread(t, sqlDB, "lc1"); active != fresh {
		t.Fatalf("active thread = %q, want the new %q", active, fresh)
	}

	// The old thread keeps its row; the active read starts empty.
	var oldRows int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM thread_messages WHERE thread_id = ?`, first).Scan(&oldRows); err != nil {
		t.Fatalf("count old thread rows: %v", err)
	}
	if oldRows != 1 {
		t.Fatalf("old thread rows = %d, want 1", oldRows)
	}
	empty, err := svc.GetAgentThread(ctx, "lc1")
	if err != nil {
		t.Fatalf("replay new thread: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("new thread rows = %d, want 0", len(empty))
	}

	// Each switch mints another identity.
	secondRes, err := switchThread(t, svc, "lc1", "cmd-2")
	if err != nil {
		t.Fatalf("second ExecuteThreadSwitch: %v", err)
	}
	second := secondRes.ID
	if second == fresh || second == first {
		t.Fatalf("second new thread = %q, want distinct from %q and %q", second, first, fresh)
	}
}

// Each switch records the thread it replaced: the display chain the /new
// dividers are built from, with the committed timestamp the event carries.
func TestExecuteThreadSwitchLinksPredecessor(t *testing.T) {
	// A file-backed store: the restart below closes the handle and
	// reopens the same database.
	sqlDB := testdb.Open(t, false)
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "lc-chain", Name: "Chain", Icon: "globe"})

	first := activeThread(t, sqlDB, "lc-chain")
	res1, err := switchThread(t, svc, "lc-chain", "cmd-1")
	if err != nil {
		t.Fatalf("first switch: %v", err)
	}
	if res1.PreviousID != first {
		t.Errorf("first previous = %q, want %q", res1.PreviousID, first)
	}
	res2, err := switchThread(t, svc, "lc-chain", "cmd-2")
	if err != nil {
		t.Fatalf("second switch: %v", err)
	}
	if res2.PreviousID != res1.ID {
		t.Errorf("second previous = %q, want %q", res2.PreviousID, res1.ID)
	}
	var firstStamp, secondStamp int64
	if err := sqlDB.QueryRow(
		`SELECT created_at FROM threads WHERE id = ?`, res1.ID).Scan(&firstStamp); err != nil {
		t.Fatalf("read first stamp: %v", err)
	}
	if err := sqlDB.QueryRow(
		`SELECT created_at FROM threads WHERE id = ?`, res2.ID).Scan(&secondStamp); err != nil {
		t.Fatalf("read second stamp: %v", err)
	}
	if firstStamp <= 0 || secondStamp < firstStamp {
		t.Errorf("created stamps = %d, %d, want positive and non-decreasing", firstStamp, secondStamp)
	}

	// The stored chain matches the results, and survives a restart:
	// the handle closes, the same file reopens, the state is read from
	// disk rather than from the live connection.
	var previous sql.NullString
	if err := sqlDB.QueryRow(
		`SELECT previous_thread_id FROM threads WHERE id = ?`, res2.ID).Scan(&previous); err != nil {
		t.Fatalf("read stored predecessor: %v", err)
	}
	if !previous.Valid || previous.String != res1.ID {
		t.Fatalf("stored predecessor = %v, want %q", previous, res1.ID)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	reopenedDB := testdb.Open(t, false)
	_, _, reopened, err := backend.New(reopenedDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("backend.New (reopen): %v", err)
	}
	if active := activeThread(t, reopenedDB, "lc-chain"); active != res2.ID {
		t.Errorf("active thread after restart = %q, want %q", active, res2.ID)
	}
	if _, err := reopened.GetAgentThread(context.Background(), "lc-chain"); err != nil {
		t.Fatalf("GetAgentThread after restart: %v", err)
	}
}

// A failed switch leaves no half-made thread: the insert rolls back with
// the pointer swap it belongs to.
func TestExecuteThreadSwitchRollbackLeavesNoThread(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "lc-rb", Name: "Rollback", Icon: "globe"})
	if _, err := svc.AcceptItem(context.Background(), "lc-rb", "cmd-1", "/new", "new_thread", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, _, err := svc.StartItem(context.Background(), "lc-rb", "cmd-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	before := countThreads(t, sqlDB)

	// The pointer table gone: the committed thread row must roll back
	// with the failed swap.
	if _, err := sqlDB.Exec(`ALTER TABLE agents RENAME TO agents_off`); err != nil {
		t.Fatalf("rename agents: %v", err)
	}
	if _, _, err := svc.ExecuteThreadSwitch(context.Background(), "lc-rb", "cmd-1"); err == nil {
		t.Fatal("switch succeeded with the pointer table gone")
	}
	if _, err := sqlDB.Exec(`ALTER TABLE agents_off RENAME TO agents`); err != nil {
		t.Fatalf("restore agents: %v", err)
	}
	if after := countThreads(t, sqlDB); after != before {
		t.Fatalf("threads after failed switch = %d, want %d (no orphan)", after, before)
	}
	// The item stays started, so its outcome is still unreported.
	var state string
	if err := sqlDB.QueryRow(
		`SELECT state FROM queue_items WHERE agent_id = 'lc-rb' AND id = 'cmd-1'`).Scan(&state); err != nil {
		t.Fatalf("read item state: %v", err)
	}
	if state != "started" {
		t.Errorf("item state = %q, want started after the rollback", state)
	}
}

// activeThread reads the pointer the way nothing in production does
// anymore: straight from the row, so the tests pin storage, not the
// API that resolves it.
func activeThread(t *testing.T, sqlDB *sql.DB, agentID string) string {
	t.Helper()
	var id string
	if err := sqlDB.QueryRow(
		`SELECT active_thread_id FROM agents WHERE id = ?`, agentID).Scan(&id); err != nil {
		t.Fatalf("read active thread: %v", err)
	}
	return id
}

func TestExecuteThreadSwitchUnknownItemFails(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	ctx := context.Background()

	before := countThreads(t, sqlDB)
	if _, _, err := svc.ExecuteThreadSwitch(ctx, "ghost", "cmd-1"); err == nil {
		t.Fatal("ExecuteThreadSwitch succeeded for an unknown agent")
	}
	if after := countThreads(t, sqlDB); after != before {
		t.Fatalf("threads after failed switch = %d, want %d (no orphan)", after, before)
	}
}

// countThreads is the whole table's row count: a switch either leaves one
// more row or none.
func countThreads(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM threads`).Scan(&n); err != nil {
		t.Fatalf("count threads: %v", err)
	}
	return n
}

func TestGetAgentThreadIsScopedToActiveThread(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "sc", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	startedItem(t, svc, "sc", "item-1", "first thread")
	secondRes, err := switchThread(t, svc, "sc", "cmd-1")
	if err != nil {
		t.Fatalf("ExecuteThreadSwitch: %v", err)
	}
	second := secondRes.ID
	startedItem(t, svc, "sc", "item-2", "second thread")

	msgs, err := svc.GetAgentThread(ctx, "sc")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "second thread" {
		t.Fatalf("scoped replay = %+v, want only the active thread's row", msgs)
	}
	if second == testdb.ThreadID("sc") {
		t.Fatal("the fresh thread must be a new identity")
	}
}

func TestActiveThreadSurvivesRestart(t *testing.T) {
	sqlDB := testdb.Open(t, false)
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "persist", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	createdRes, err := switchThread(t, svc, "persist", "cmd-1")
	if err != nil {
		t.Fatalf("ExecuteThreadSwitch: %v", err)
	}
	created := createdRes.ID

	// A second boot over the same file reads the same pointer: the
	// handle closes first, so nothing survives in a live connection.
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	reopenedDB := testdb.Open(t, false)
	_, _, reopened, err := backend.New(reopenedDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("backend.New (reopen): %v", err)
	}
	if active := activeThread(t, reopenedDB, "persist"); active != created {
		t.Fatalf("active thread after restart = %q, want %q", active, created)
	}
	msgs, err := reopened.GetAgentThread(ctx, "persist")
	if err != nil {
		t.Fatalf("GetAgentThread after restart: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("messages in the fresh thread = %d, want 0", len(msgs))
	}
}

// The initial thread is visible before any prompt: creation is eager,
// not lazy.
func TestNewAgentHasEmptyInitialThread(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "fresh", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	active := activeThread(t, sqlDB, "fresh")
	if active != testdb.ThreadID("fresh") {
		t.Fatalf("initial active thread = %q, want the seeded %q", active, testdb.ThreadID("fresh"))
	}
	msgs, err := svc.GetAgentThread(ctx, "fresh")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("initial thread rows = %d, want 0", len(msgs))
	}
}

// Item writes and thread switches share one writer: an item's rows land
// whole in the thread it started in, never straddling a concurrent swap.
func TestConcurrentSwitchAndAppendStayWhole(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "race", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	const prompts = 10
	for i := range prompts {
		startedItem(t, svc, "race", fmt.Sprintf("item-%d", i), fmt.Sprintf("q%d", i))
	}

	var (
		mu      sync.Mutex
		created = map[string]struct{}{testdb.ThreadID("race"): {}}
		wg      sync.WaitGroup
		errCh   = make(chan error, prompts+5)
	)
	for i := range prompts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := svc.AppendItemMessages(ctx, "race", fmt.Sprintf("item-%d", i),
				[]backend.ThreadMessageDraft{{
					ID:      fmt.Sprintf("answer-%d", i),
					Role:    apitypes.ThreadRoleAssistant,
					Content: "a",
				}}); err != nil {
				errCh <- fmt.Errorf("append %d: %w", i, err)
			}
		}(i)
	}
	for i := range 5 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			activated, err := switchThread(t, svc, "race", fmt.Sprintf("cmd-%d", i))
			if err != nil {
				errCh <- fmt.Errorf("switch %d: %w", i, err)
				return
			}
			mu.Lock()
			created[activated.ID] = struct{}{}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent write: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id, thread_id FROM thread_messages WHERE role = 'assistant'`)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	threadOfRow := make(map[string]string, prompts)
	for rows.Next() {
		var id, threadID string
		if err := rows.Scan(&id, &threadID); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		threadOfRow[id] = threadID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(threadOfRow) != prompts {
		t.Fatalf("stored answers = %d, want %d", len(threadOfRow), prompts)
	}
	for i := range prompts {
		threadID := threadOfRow[fmt.Sprintf("answer-%d", i)]
		if threadID == "" {
			t.Fatalf("answer %d missing", i)
		}
		if _, ok := created[threadID]; !ok {
			t.Fatalf("answer %d landed in unknown thread %q", i, threadID)
		}
	}
}
