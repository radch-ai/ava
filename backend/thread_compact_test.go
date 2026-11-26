package backend_test

// /compact's durable half: ExecuteCompaction seeds the successor thread
// with the summary as its first assistant message, links it to the
// formerly active thread, swaps the pointer, and settles the item — all
// in one transaction whose two events (thread_changed then
// item_finished) are the committed transition's whole report.

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// runCompaction admits and starts one compact item, then executes it —
// the real item transitions the gateway drives.
func runCompaction(t *testing.T, turns *backend.TurnService, agentID, itemID, summary string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
	t.Helper()
	ctx := context.Background()
	if _, err := turns.AcceptItem(ctx, agentID, itemID, "", "compact", nil); err != nil {
		t.Fatalf("accept compact: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agentID, itemID); err != nil {
		t.Fatalf("start compact: %v", err)
	}
	return turns.ExecuteCompaction(ctx, agentID, itemID, summary)
}

func TestExecuteCompactionSeedsSummaryAndActivates(t *testing.T) {
	turns, sqlDB := journalService(t)
	const agent = "research"
	seedRows(t, sqlDB, agent, []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "did the thing", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, "payload", nil),
	})
	oldThread := testdb.ThreadID(agent)

	const summary = "Goals: ship compaction. Decision: one shared engine."
	activated, batch, err := runCompaction(t, turns, agent, "compact-1", summary)
	if err != nil {
		t.Fatalf("execute compaction: %v", err)
	}
	if activated.ID == "" || activated.ID == oldThread {
		t.Fatalf("activated = %+v, want a fresh successor", activated)
	}
	if activated.PreviousID != oldThread {
		t.Errorf("activated previous = %q, want %q", activated.PreviousID, oldThread)
	}

	// The successor is a compact boundary linked to the old thread,
	// and the pointer moved to it.
	var kind, previous string
	if err := sqlDB.QueryRow(
		`SELECT kind, previous_thread_id FROM threads WHERE agent_id = ? AND id = ?`,
		agent, activated.ID).Scan(&kind, &previous); err != nil {
		t.Fatal(err)
	}
	if kind != "compact" || previous != oldThread {
		t.Errorf("successor thread = (%q, %q), want (compact, %q)", kind, previous, oldThread)
	}
	var active string
	if err := sqlDB.QueryRow(`SELECT active_thread_id FROM agents WHERE id = ?`, agent).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != activated.ID {
		t.Errorf("active thread = %q, want the successor %q", active, activated.ID)
	}

	// The successor carries exactly the seeded summary as its first
	// assistant message, keyed to the item that minted it.
	var (
		role, content, turnID string
		rows                  int
	)
	if err := sqlDB.QueryRow(
		`SELECT role, content, turn_id FROM thread_messages WHERE agent_id = ? AND thread_id = ?`,
		agent, activated.ID).Scan(&role, &content, &turnID); err != nil {
		t.Fatal(err)
	}
	if role != "assistant" || content != summary || turnID != "compact-1" {
		t.Errorf("seeded row = (%q, %q, %q), want (assistant, summary, compact-1)", role, content, turnID)
	}
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = ? AND thread_id = ?`,
		agent, activated.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("successor rows = %d, want exactly the summary", rows)
	}

	// The item completed and the batch is the committed transition's
	// whole report: thread_changed with the compact divider, then
	// item_finished carrying the seeded row.
	if got := itemState(t, sqlDB, agent, "compact-1"); got != apitypes.ItemCompleted {
		t.Errorf("compact item state = %q, want completed", got)
	}
	if len(batch.Events) != 2 {
		t.Fatalf("batch events = %d, want thread_changed then item_finished", len(batch.Events))
	}
	changed := batch.Events[0]
	if changed.Kind != apitypes.LifecycleThreadChanged || changed.ThreadID != activated.ID ||
		changed.PreviousThreadID != oldThread {
		t.Errorf("thread_changed = %+v, want the successor link", changed)
	}
	if changed.Divider == nil || changed.Divider.Content != "Compacted" {
		t.Errorf("thread_changed divider = %+v, want the Compacted label", changed.Divider)
	}
	finished := batch.Events[1]
	if finished.Kind != apitypes.LifecycleItemFinished || finished.Outcome != apitypes.ItemCompleted ||
		finished.ThreadID != activated.ID {
		t.Errorf("item_finished = %+v, want completion in the successor", finished)
	}
	if len(finished.Rows) != 1 || finished.Rows[0].Role != "assistant" || finished.Rows[0].Content != summary {
		t.Errorf("item_finished rows = %+v, want the seeded summary", finished.Rows)
	}
}

func TestExecuteCompactionRejectsWrongItem(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent = "research"

	// A shake item cannot compact.
	if _, err := turns.AcceptItem(ctx, agent, "shake-1", "", "shake", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.StartItem(ctx, agent, "shake-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.ExecuteCompaction(ctx, agent, "shake-1", "summary"); err == nil {
		t.Error("shake-kind item compacted, want kind rejection")
	}
	// An accepted-but-unstarted item cannot compact.
	if _, err := turns.AcceptItem(ctx, agent, "compact-1", "", "compact", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.ExecuteCompaction(ctx, agent, "compact-1", "summary"); err == nil {
		t.Error("unstarted item compacted, want not-started rejection")
	}
	if n := countRows(t, sqlDB, `SELECT COUNT(*) FROM threads WHERE agent_id = 'research'`); n != 1 {
		t.Errorf("threads = %d, want only the original — nothing written", n)
	}
}

func TestExecuteCompactionEmptySummaryRejected(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent = "research"
	if _, err := turns.AcceptItem(ctx, agent, "compact-1", "", "compact", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.StartItem(ctx, agent, "compact-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := turns.ExecuteCompaction(ctx, agent, "compact-1", ""); err == nil {
		t.Error("empty summary compacted, want rejection")
	}
	if n := countRows(t, sqlDB, `SELECT COUNT(*) FROM threads WHERE agent_id = 'research'`); n != 1 {
		t.Errorf("threads = %d, want only the original — nothing written", n)
	}
}

// A compaction committed before a restart is the stored truth a fresh
// process replays: the successor is active, its projection leads with
// the summary, and the display chain walks into the predecessor over a
// Compacted divider.
func TestExecuteCompactionRestartReplaysSummary(t *testing.T) {
	sqlDB := testdb.Open(t, false)
	_, _, turns, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "p", SoulIdentity: "i",
		OperatingInstructions: "o", ToolsContext: "t",
	})
	ctx := context.Background()
	// An oversized payload the shake elides first: its reference must
	// survive the rotation that follows.
	big := strings.Repeat("x", 3000)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "did the thing", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, big, nil),
	})
	runShake(t, turns, "research", "shake-1")
	const summary = "Carried context: the fetch tool answer stands."
	if _, _, err := runCompaction(t, turns, "research", "compact-1", summary); err != nil {
		t.Fatalf("execute compaction: %v", err)
	}

	// A fresh process on the same file: the summary is the only
	// model-facing message of the active thread.
	_, _, cold, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("reopen backend: %v", err)
	}
	msgs, err := cold.GetAgentThread(ctx, "research")
	if err != nil {
		t.Fatalf("cold GetAgentThread: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].Content != summary {
		t.Fatalf("cold projection = %+v, want exactly the summary as an assistant message", msgs)
	}

	// The active display page leads with the Compacted divider and its
	// cursor walks into the predecessor's original exchange.
	snap, err := cold.SyncSnapshot(ctx, "research", 20)
	if err != nil {
		t.Fatalf("cold SyncSnapshot: %v", err)
	}
	if snap.Page == nil || len(snap.Page.Items) == 0 ||
		snap.Page.Items[0].Role != "divider" || snap.Page.Items[0].Content != "Compacted" {
		t.Fatalf("successor page head = %+v, want the Compacted divider", snap.Page)
	}
	if snap.Page.NextCursor == nil || !strings.Contains(*snap.Page.NextCursor, testdb.ThreadID("research")) {
		t.Fatalf("successor cursor = %+v, want a walk into the predecessor", snap.Page.NextCursor)
	}
}

// The elided predecessor row stays restorable across the rotation: the
// reference read is agent-scoped, not thread-scoped.
func TestExecuteCompactionKeepsReferencesRestorable(t *testing.T) {
	sqlDB := testdb.Open(t, false)
	_, _, turns, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "p", SoulIdentity: "i",
		OperatingInstructions: "o", ToolsContext: "t",
	})
	ctx := context.Background()
	big := strings.Repeat("x", 3000)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "did the thing", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, big, nil),
		// A later plain turn makes t1's payload old enough to elide.
		userDraft("t2 prompt", "t2"),
		groupDraft(t, "t2", "settled", callDecl("c2", "fetch")),
		toolDraft(t, "t2", "c2", "fetch", `{}`, "tail", nil),
	})
	runShake(t, turns, "research", "shake-1")
	if _, _, err := runCompaction(t, turns, "research", "compact-1", "the summary"); err != nil {
		t.Fatalf("execute compaction: %v", err)
	}
	stored, err := turns.ReadToolOutput(ctx, "research", "research-row-2")
	if err != nil {
		t.Fatalf("predecessor row read after compaction: %v", err)
	}
	if stored != big {
		t.Errorf("restored predecessor output = %d bytes, want the original %d", len(stored), len(big))
	}
}

// The itemless rotation — the pre-prompt automation's committed half —
// seeds and swaps exactly like ExecuteCompaction but settles nothing:
// the tagged prompt item stays accepted and settles when its own turn
// finishes, and the single thread_changed event carries the seeded row
// (there is no item_finished to ride).
func TestRotateThreadCompactedSeedsAndSwaps(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent = "research"
	seedRows(t, sqlDB, agent, []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "did the thing", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, "payload", nil),
	})
	oldThread := testdb.ThreadID(agent)

	// The tag must name a real queued item of this agent.
	if _, err := turns.AcceptItem(ctx, agent, "prompt-1", "hello", "prompt", nil); err != nil {
		t.Fatalf("accept prompt: %v", err)
	}
	head := journalPosition(t, turns, agent)

	const summary = "Goals: ship pre-prompt automation. Decision: one engine."
	activated, batch, err := turns.RotateThreadCompacted(ctx, agent, "prompt-1", summary)
	if err != nil {
		t.Fatalf("rotate thread compacted: %v", err)
	}
	if activated == nil || activated.ID == "" || activated.ID == oldThread {
		t.Fatalf("activated = %+v, want a fresh successor", activated)
	}
	if activated.PreviousID != oldThread {
		t.Errorf("activated previous = %q, want %q", activated.PreviousID, oldThread)
	}

	// The successor is a compact boundary linked to the old thread and
	// the pointer moved to it.
	var kind, previous string
	if err := sqlDB.QueryRow(
		`SELECT kind, previous_thread_id FROM threads WHERE agent_id = ? AND id = ?`,
		agent, activated.ID).Scan(&kind, &previous); err != nil {
		t.Fatal(err)
	}
	if kind != "compact" || previous != oldThread {
		t.Errorf("successor thread = (%q, %q), want (compact, %q)", kind, previous, oldThread)
	}
	var active string
	if err := sqlDB.QueryRow(`SELECT active_thread_id FROM agents WHERE id = ?`, agent).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != activated.ID {
		t.Errorf("active thread = %q, want the successor %q", active, activated.ID)
	}

	// The summary is the successor's single row, keyed to the tagged
	// prompt item.
	var role, content, turnID string
	if err := sqlDB.QueryRow(
		`SELECT role, content, turn_id FROM thread_messages WHERE agent_id = ? AND thread_id = ?`,
		agent, activated.ID).Scan(&role, &content, &turnID); err != nil {
		t.Fatal(err)
	}
	if role != "assistant" || content != summary || turnID != "prompt-1" {
		t.Errorf("seeded row = (%q, %q, %q), want (assistant, summary, prompt-1)", role, content, turnID)
	}

	// Exactly one journal event: thread_changed carrying the divider
	// and the seeded row; the tagged item never settles.
	if got := countRows(t, sqlDB, `SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ?`, agent); got != int(head)+1 {
		t.Fatalf("lifecycle_journal rows = %d, want %d (one event)", got, head+1)
	}
	if len(batch.Events) != 1 {
		t.Fatalf("batch events = %d, want the single thread_changed", len(batch.Events))
	}
	changed := batch.Events[0]
	if changed.Kind != apitypes.LifecycleThreadChanged || changed.ThreadID != activated.ID ||
		changed.PreviousThreadID != oldThread || changed.ItemID != "prompt-1" ||
		changed.ItemKind != "prompt" {
		t.Errorf("thread_changed = %+v, want the tagged successor link", changed)
	}
	if changed.Divider == nil || changed.Divider.Content != "Compacted" {
		t.Errorf("thread_changed divider = %+v, want the Compacted label", changed.Divider)
	}
	if len(changed.Rows) != 1 || changed.Rows[0].Role != "assistant" || changed.Rows[0].Content != summary {
		t.Errorf("thread_changed rows = %+v, want the seeded summary riding the event", changed.Rows)
	}
	if got := itemState(t, sqlDB, agent, "prompt-1"); got != "accepted" {
		t.Errorf("tagged prompt state = %q, want accepted (no settle)", got)
	}

	// An empty summary is refused, exactly like ExecuteCompaction —
	// while the tag is still valid, so the summary guard is what fires.
	if _, _, err := turns.RotateThreadCompacted(ctx, agent, "prompt-1", ""); err == nil {
		t.Error("empty summary succeeded")
	}

	// The tag's guards are enforced, not assumed: an unknown id, a
	// non-prompt kind, and a non-accepted state all fail, exactly like
	// every sibling mutation re-reads its item.
	if _, _, err := turns.RotateThreadCompacted(ctx, agent, "nope", summary); err == nil {
		t.Error("unknown tag item succeeded")
	}
	if _, err := turns.AcceptItem(ctx, agent, "cmd-1", "/compact", "compact", nil); err != nil {
		t.Fatalf("accept compact item: %v", err)
	}
	if _, _, err := turns.RotateThreadCompacted(ctx, agent, "cmd-1", summary); err == nil {
		t.Error("compact-kind tag succeeded")
	}
	if _, _, err := turns.StartItem(ctx, agent, "prompt-1"); err != nil {
		t.Fatalf("start prompt item: %v", err)
	}
	if _, _, err := turns.RotateThreadCompacted(ctx, agent, "prompt-1", summary); err == nil {
		t.Error("started-state tag succeeded")
	}
	if got := itemState(t, sqlDB, agent, "prompt-1"); got != "started" {
		t.Errorf("prompt state = %q, want started", got)
	}
}
