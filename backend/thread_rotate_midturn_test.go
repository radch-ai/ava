package backend_test

// RotateThreadMidTurn: the mid-loop rotation's durable effects —
// retained references, the rebind, the single refresh event — and its
// guards, which fail closed before any write lands.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

// midTurnItem starts one prompt item with two completed rounds in the
// active thread: prompt, group one with two parallel outcomes, group
// two with two parallel outcomes. Returns the item id and its thread.
func midTurnItem(t *testing.T, turns *backend.TurnService, agent, itemID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := turns.AcceptItem(ctx, agent, itemID, "run the research", "prompt", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.StartItem(ctx, agent, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := turns.AppendItemMessages(ctx, agent, itemID, []backend.ThreadMessageDraft{
		groupDraft(t, itemID, "round one", callDecl("c1", "fetch"), callDecl("c2", "fetch")),
		toolDraft(t, itemID, "c1", "fetch", `{}`, "one a", nil),
		toolDraft(t, itemID, "c2", "fetch", `{}`, "one b", nil),
		groupDraft(t, itemID, "round two", callDecl("c3", "fetch"), callDecl("c4", "fetch")),
		toolDraft(t, itemID, "c3", "fetch", `{}`, "two a", nil),
		toolDraft(t, itemID, "c4", "fetch", `{}`, "two b", nil),
	}); err != nil {
		t.Fatal(err)
	}
}

// midTurnRow is one stored row in the comparable shape the
// immutability and ordering assertions read.
type midTurnRow struct {
	ID        string
	Role      string
	Content   string
	CreatedAt int64
	TurnID    string
	ToolCalls string
	ThreadID  string
}

func storedRows(t *testing.T, sqlDB *sql.DB, agent string) []midTurnRow {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT id, role, content, created_at, turn_id, COALESCE(tool_calls, ''), thread_id
		 FROM thread_messages WHERE agent_id = ? ORDER BY created_at ASC, id ASC`, agent)
	if err != nil {
		t.Fatalf("list rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []midTurnRow
	for rows.Next() {
		var r midTurnRow
		if err := rows.Scan(&r.ID, &r.Role, &r.Content, &r.CreatedAt, &r.TurnID, &r.ToolCalls, &r.ThreadID); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("walk rows: %v", err)
	}
	return out
}

func TestRotateThreadMidTurnOrdinaryRetainsWholeTurn(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"
	midTurnItem(t, turns, agent, item)
	source := activeThreadOf(t, sqlDB, agent)
	before := storedRows(t, sqlDB, agent)

	activated, batch, err := turns.RotateThreadMidTurn(ctx, agent, item, "the running summary", backend.RetainCurrentTurn)
	if err != nil {
		t.Fatalf("RotateThreadMidTurn: %v", err)
	}
	successor := activated.ID
	if activated.PreviousID != source || successor == source {
		t.Fatalf("activated = %+v, want successor of %s", activated, source)
	}

	// Source rows are immutable: ids, thread, timestamps, content.
	// The successor's seeded summary is the only new row.
	got := storedRows(t, sqlDB, agent)
	if len(got) != len(before)+1 {
		t.Fatalf("row count changed: %d -> %d, want one seeded summary added", len(before), len(got))
	}
	for i := range before {
		if got[i] != before[i] {
			t.Fatalf("row %d changed: %+v -> %+v", i, before[i], got[i])
		}
	}

	// The successor references the prompt and every round, in order.
	refs := listRefs(t, sqlDB, agent, successor)
	if len(refs) != len(before) {
		t.Fatalf("refs = %v, want all %d rows", refs, len(before))
	}
	for i, r := range refs {
		if r != before[i].ID {
			t.Fatalf("ref %d = %s, want %s (native order)", i, r, before[i].ID)
		}
	}

	// The item's writes are rebound and the pointer swapped; the item
	// itself stays started.
	if got := itemThreadOf(t, sqlDB, agent, item); got != successor {
		t.Fatalf("item thread = %s, want successor %s", got, successor)
	}
	if got := activeThreadOf(t, sqlDB, agent); got != successor {
		t.Fatalf("active thread = %s, want successor", got)
	}
	if state := itemState(t, sqlDB, agent, item); state != "started" {
		t.Fatalf("item state = %s, want still started", state)
	}

	// One event, the refresh: it carries the post-activation page with
	// the divider first and no item settlement.
	if len(batch.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(batch.Events))
	}
	ev := batch.Events[0]
	if ev.Kind != apitypes.LifecycleConversationRefresh || ev.ItemID != item ||
		ev.ThreadID != successor || ev.PreviousThreadID != source || ev.Page == nil {
		t.Fatalf("event = %+v, want the refresh carrying the successor page", ev)
	}
	if len(ev.Page.Items) == 0 || ev.Page.Items[0].Role != apitypes.ThreadRole("divider") {
		t.Fatalf("page head = %+v, want the divider", ev.Page.Items[0])
	}
	if ev.Page.Items[1].Content != "the running summary" {
		t.Fatalf("page[1] = %+v, want the seeded summary", ev.Page.Items[1])
	}
	if ev.Page.ThreadID != successor {
		t.Fatalf("page thread = %s, want successor", ev.Page.ThreadID)
	}
}

func TestRotateThreadMidTurnRollingRetainsLatestBatch(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"
	midTurnItem(t, turns, agent, item)
	before := storedRows(t, sqlDB, agent)

	activated, _, err := turns.RotateThreadMidTurn(ctx, agent, item, "folded summary", backend.RetainLatestBatch)
	if err != nil {
		t.Fatalf("RotateThreadMidTurn: %v", err)
	}
	// Exactly the trailing batch: the second round's group plus both
	// parallel outcomes — not the prompt, not round one.
	refs := listRefs(t, sqlDB, agent, activated.ID)
	want := []string{before[4].ID, before[5].ID, before[6].ID}
	if len(refs) != 3 || refs[0] != want[0] || refs[1] != want[1] || refs[2] != want[2] {
		t.Fatalf("refs = %v, want the latest batch %v", refs, want)
	}
	// The prompt row stays physically in its source thread.
	if state := itemState(t, sqlDB, agent, item); state != "started" {
		t.Fatalf("item state = %s, want still started", state)
	}
}

// A second rotation after the turn wrote more rows references those
// physical rows of the middle thread directly — never a ref-to-ref.
func TestRotateThreadMidTurnRepeatedReferencesPhysicalRows(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"
	midTurnItem(t, turns, agent, item)
	first, _, err := turns.RotateThreadMidTurn(ctx, agent, item, "first summary", backend.RetainLatestBatch)
	if err != nil {
		t.Fatalf("first rotation: %v", err)
	}

	// The turn continues in the successor: one more complete round.
	if _, err := turns.AppendItemMessages(ctx, agent, item, []backend.ThreadMessageDraft{
		groupDraft(t, item, "round three", callDecl("c5", "fetch")),
		toolDraft(t, item, "c5", "fetch", `{}`, "three", nil),
	}); err != nil {
		t.Fatalf("append continuation: %v", err)
	}

	second, _, err := turns.RotateThreadMidTurn(ctx, agent, item, "second summary", backend.RetainLatestBatch)
	if err != nil {
		t.Fatalf("second rotation: %v", err)
	}
	if second.ID == first.ID || second.PreviousID != first.ID {
		t.Fatalf("second successor = %+v, want chained off the first", second)
	}
	refs := listRefs(t, sqlDB, agent, second.ID)
	if len(refs) != 2 {
		t.Fatalf("refs = %v, want the newest physical batch", refs)
	}
	for _, r := range refs {
		var thread string
		if err := sqlDB.QueryRow(
			`SELECT thread_id FROM thread_messages WHERE agent_id = ? AND id = ?`, agent, r).Scan(&thread); err != nil {
			t.Fatalf("resolve ref %s: %v", r, err)
		}
		if thread != first.ID {
			t.Fatalf("ref %s points into %s, want a physical row of %s", r, thread, first.ID)
		}
	}
}

func TestRotateThreadMidTurnGuards(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"

	// Empty summary, before anything else: nothing lands.
	midTurnItem(t, turns, agent, item)
	if _, _, err := turns.RotateThreadMidTurn(ctx, agent, item, "", backend.RetainCurrentTurn); err == nil {
		t.Fatal("empty summary rotated")
	}
	assertNothingRotated(t, sqlDB, agent)

	// Open invocations block the boundary.
	if _, err := sqlDB.Exec(
		`INSERT INTO tool_invocations (id, agent_id, item_id, call_id, name, args, started_ms, state)
		 VALUES ('inv-1', ?, ?, 'call-x', 'fetch', '{}', 0, 'open')`, agent, item); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.RotateThreadMidTurn(ctx, agent, item, "s", backend.RetainCurrentTurn); err == nil {
		t.Fatal("rotation crossed an open invocation")
	}
	assertNothingRotated(t, sqlDB, agent)
	if _, err := sqlDB.Exec(`DELETE FROM tool_invocations WHERE id = 'inv-1'`); err != nil {
		t.Fatal(err)
	}

	// A stale active pointer — some other boundary moved underneath.
	other := agent + "-t-other"
	if _, err := sqlDB.Exec(
		`INSERT INTO threads (id, agent_id) VALUES (?, ?)`, other, agent); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`UPDATE agents SET active_thread_id = ? WHERE id = ?`, other, agent); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.RotateThreadMidTurn(ctx, agent, item, "s", backend.RetainCurrentTurn); err == nil {
		t.Fatal("rotation onto a stale active thread")
	}
	if _, err := sqlDB.Exec(`UPDATE agents SET active_thread_id = ? WHERE id = ?`,
		itemThreadOf(t, sqlDB, agent, item), agent); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM threads WHERE id = ?`, other); err != nil {
		t.Fatal(err)
	}

	// A foreign agent's rotation attempt finds no item.
	if _, _, err := turns.RotateThreadMidTurn(ctx, "nobody", item, "s", backend.RetainCurrentTurn); err == nil {
		t.Fatal("foreign agent rotated")
	}

	// A rolling rotation with no complete batch refuses: fresh item,
	// prompt row only.
	if _, err := turns.AcceptItem(ctx, agent, "item-2", "plain", "prompt", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.StartItem(ctx, agent, "item-2"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.RotateThreadMidTurn(ctx, agent, "item-2", "s", backend.RetainLatestBatch); err == nil {
		t.Fatal("rolling rotation without a batch")
	}

	// A non-prompt item never rotates mid-turn.
	if _, err := turns.AcceptItem(ctx, agent, "compact-1", "", "compact", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.StartItem(ctx, agent, "compact-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.RotateThreadMidTurn(ctx, agent, "compact-1", "s", backend.RetainCurrentTurn); err == nil {
		t.Fatal("compact item rotated mid-turn")
	}

	// An accepted-but-unstarted item is not running.
	if _, err := turns.AcceptItem(ctx, agent, "item-3", "later", "prompt", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.RotateThreadMidTurn(ctx, agent, "item-3", "s", backend.RetainCurrentTurn); err == nil {
		t.Fatal("unstarted item rotated")
	}

	assertNothingRotated(t, sqlDB, agent)
}

func activeThreadOf(t *testing.T, sqlDB *sql.DB, agent string) string {
	t.Helper()
	var thread string
	if err := sqlDB.QueryRow(
		`SELECT active_thread_id FROM agents WHERE id = ?`, agent).Scan(&thread); err != nil {
		t.Fatalf("read active thread: %v", err)
	}
	return thread
}

func itemThreadOf(t *testing.T, sqlDB *sql.DB, agent, item string) string {
	t.Helper()
	var thread sql.NullString
	if err := sqlDB.QueryRow(
		`SELECT thread_id FROM queue_items WHERE agent_id = ? AND id = ?`, agent, item).Scan(&thread); err != nil {
		t.Fatalf("read item thread: %v", err)
	}
	return thread.String
}

func listRefs(t *testing.T, sqlDB *sql.DB, agent, thread string) []string {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT message_id FROM thread_context_refs WHERE agent_id = ? AND thread_id = ? ORDER BY position`,
		agent, thread)
	if err != nil {
		t.Fatalf("list refs: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan ref: %v", err)
		}
		out = append(out, id)
	}
	return out
}

func assertNothingRotated(t *testing.T, sqlDB *sql.DB, agent string) {
	t.Helper()
	if n := countRows(t, sqlDB, `SELECT COUNT(*) FROM threads WHERE agent_id = ?`, agent); n != 1 {
		t.Fatalf("threads = %d, want only the original", n)
	}
	if n := countRows(t, sqlDB, `SELECT COUNT(*) FROM thread_context_refs WHERE agent_id = ?`, agent); n != 0 {
		t.Fatalf("refs = %d, want none", n)
	}
	if n := countRows(t, sqlDB,
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = ? AND is_summary = 1`, agent); n != 0 {
		t.Fatalf("summary rows = %d, want none", n)
	}
}
