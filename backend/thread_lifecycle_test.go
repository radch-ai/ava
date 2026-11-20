package backend_test

// Thread lifecycle pins the agent's active pointer: creation swaps it
// atomically, reads never repair, and message writes land in whatever
// thread is active when the sink resolves.

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

func TestCreateAndActivateThreadSwapsPointer(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "lc1", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	first := activeThread(t, sqlDB, "lc1")
	if first != testdb.ThreadID("lc1") {
		t.Fatalf("active thread = %q, want the seeded %q", first, testdb.ThreadID("lc1"))
	}
	// History in the old thread stays where it is.
	if err := svc.AppendThreadMessages(ctx, "lc1",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "old", TurnID: "t1"}}); err != nil {
		t.Fatalf("append old thread: %v", err)
	}

	freshRes, err := svc.CreateAndActivateThread(ctx, "lc1")
	if err != nil {
		t.Fatalf("CreateAndActivateThread: %v", err)
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

	// Each activation mints another identity.
	secondRes, err := svc.CreateAndActivateThread(ctx, "lc1")
	if err != nil {
		t.Fatalf("second CreateAndActivateThread: %v", err)
	}
	second := secondRes.ID
	if second == fresh || second == first {
		t.Fatalf("second new thread = %q, want distinct from %q and %q", second, first, fresh)
	}
}

// Each activation records the thread it replaced: the display chain
// the /new dividers are built from, with the committed timestamp the
// completion event carries.
func TestCreateAndActivateThreadLinksPredecessor(t *testing.T) {
	// A file-backed store: the restart below closes the handle and
	// reopens the same database.
	sqlDB := testdb.Open(t, false)
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "lc-chain", Name: "Chain", Icon: "globe"})
	ctx := context.Background()

	first := activeThread(t, sqlDB, "lc-chain")
	res1, err := svc.CreateAndActivateThread(ctx, "lc-chain")
	if err != nil {
		t.Fatalf("first activation: %v", err)
	}
	if res1.PreviousID != first {
		t.Errorf("first previous = %q, want %q", res1.PreviousID, first)
	}
	res2, err := svc.CreateAndActivateThread(ctx, "lc-chain")
	if err != nil {
		t.Fatalf("second activation: %v", err)
	}
	if res2.PreviousID != res1.ID {
		t.Errorf("second previous = %q, want %q", res2.PreviousID, res1.ID)
	}
	if res1.CreatedAt <= 0 || res2.CreatedAt < res1.CreatedAt {
		t.Errorf("created stamps = %d, %d, want positive and non-decreasing", res1.CreatedAt, res2.CreatedAt)
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
	_, _, reopened, err := backend.New(reopenedDB, zerolog.Nop(), testMasterKey)
	if err != nil {
		t.Fatalf("backend.New (reopen): %v", err)
	}
	if active := activeThread(t, reopenedDB, "lc-chain"); active != res2.ID {
		t.Errorf("active thread after restart = %q, want %q", active, res2.ID)
	}
	if _, err := reopened.GetAgentThread(ctx, "lc-chain"); err != nil {
		t.Fatalf("GetAgentThread after restart: %v", err)
	}
}

// A failed write leaves no half-made thread: the insert rolls back
// with the pointer swap it belongs to.
func TestCreateAndActivateThreadRollbackLeavesNoThread(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "lc-rb", Name: "Rollback", Icon: "globe"})
	before := countThreads(t, sqlDB)

	// The pointer table gone: the thread insert commits inside the
	// transaction, the pointer update fails, so the rollback must
	// take the thread row with it.
	if _, err := sqlDB.Exec(`ALTER TABLE agents RENAME TO agents_off`); err != nil {
		t.Fatalf("rename agents: %v", err)
	}
	if _, err := svc.CreateAndActivateThread(context.Background(), "lc-rb"); err == nil {
		t.Fatal("activation succeeded with the pointer table gone")
	}
	if _, err := sqlDB.Exec(`ALTER TABLE agents_off RENAME TO agents`); err != nil {
		t.Fatalf("restore agents: %v", err)
	}
	if after := countThreads(t, sqlDB); after != before {
		t.Fatalf("threads after failed activation = %d, want %d (no orphan)", after, before)
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

func TestCreateAndActivateThreadUnknownAgentFails(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	ctx := context.Background()

	before := countThreads(t, sqlDB)
	if _, err := svc.CreateAndActivateThread(ctx, "ghost"); err == nil {
		t.Fatal("CreateAndActivateThread succeeded for an unknown agent")
	}
	if after := countThreads(t, sqlDB); after != before {
		t.Fatalf("threads after failed activation = %d, want %d (no orphan)", after, before)
	}
}

// countThreads is the whole table's row count: an activation either
// leaves one more row or none.
func countThreads(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM threads`).Scan(&n); err != nil {
		t.Fatalf("count threads: %v", err)
	}
	return n
}

func TestGetAgentThreadIsScopedToActiveThread(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "sc", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	if err := svc.AppendThreadMessages(ctx, "sc",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "first thread", TurnID: "t1"}}); err != nil {
		t.Fatalf("append first thread: %v", err)
	}
	secondRes, err := svc.CreateAndActivateThread(ctx, "sc")
	if err != nil {
		t.Fatalf("CreateAndActivateThread: %v", err)
	}
	second := secondRes.ID
	if err := svc.AppendThreadMessages(ctx, "sc",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "second thread", TurnID: "t2"}}); err != nil {
		t.Fatalf("append second thread: %v", err)
	}

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
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "persist", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	createdRes, err := svc.CreateAndActivateThread(ctx, "persist")
	if err != nil {
		t.Fatalf("CreateAndActivateThread: %v", err)
	}
	created := createdRes.ID

	// A second boot over the same file reads the same pointer: the
	// handle closes first, so nothing survives in a live connection.
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	reopenedDB := testdb.Open(t, false)
	_, _, reopened, err := backend.New(reopenedDB, zerolog.Nop(), testMasterKey)
	if err != nil {
		t.Fatalf("backend.New (reopen): %v", err)
	}
	active := activeThread(t, reopenedDB, "persist")
	if active != created {
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
	sqlDB, svc := resolverDB(t)
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

// Batches and pointer swaps share one writer: concurrent appends and
// activations each land whole, in one thread, never straddling a swap.
func TestConcurrentSwapAndAppendStayWhole(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "race", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	var (
		mu      sync.Mutex
		created = map[string]struct{}{testdb.ThreadID("race"): {}}
		wg      sync.WaitGroup
		errCh   = make(chan error, 15)
	)
	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := svc.AppendThreadMessages(ctx, "race", []backend.ThreadMessageDraft{
				{ID: fmt.Sprintf("pair-%d-a", i), Role: apitypes.ThreadRoleUser, Content: "q", TurnID: fmt.Sprintf("t%d", i)},
				{ID: fmt.Sprintf("pair-%d-b", i), Role: apitypes.ThreadRoleAssistant, Content: "a", TurnID: fmt.Sprintf("t%d", i)},
			}); err != nil {
				errCh <- fmt.Errorf("append %d: %w", i, err)
			}
		}(i)
	}
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.CreateAndActivateThread(ctx, "race")
			if err != nil {
				errCh <- fmt.Errorf("activate: %w", err)
				return
			}
			mu.Lock()
			created[res.ID] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent write: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id, thread_id FROM thread_messages`)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	threadOfRow := make(map[string]string, 20)
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
	if len(threadOfRow) != 20 {
		t.Fatalf("stored rows = %d, want 20", len(threadOfRow))
	}
	for i := range 10 {
		a, b := threadOfRow[fmt.Sprintf("pair-%d-a", i)], threadOfRow[fmt.Sprintf("pair-%d-b", i)]
		if a == "" || a != b {
			t.Fatalf("pair %d split across threads: %q / %q", i, a, b)
		}
		if _, ok := created[a]; !ok {
			t.Fatalf("pair %d landed in unknown thread %q", i, a)
		}
	}
}
