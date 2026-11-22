package backend

// Recovery-adjacent invariants over the real store: an ambiguous commit
// is resolved by readback, boot-time retention trims a journal that
// filled its budget before shutdown, and a started item's still-open
// invocations settle when recovery interrupts it.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestReadbackBatchAnswersAppliedCommits(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x42}, 32), DefaultToolLimits())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	ctx := context.Background()
	const agent = "research"

	if _, err := turns.AcceptItem(ctx, agent, "item-1", "hello", "prompt", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	history, head, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}

	// The pre-call head of the committed accept was zero: readback sees
	// the applied transaction and rebuilds its batch.
	batch := turns.readbackBatch(ctx, agent, 0)
	if batch == nil {
		t.Fatal("readback after an applied commit = nil, want the committed batch")
	}
	if len(batch.Events) != 1 || batch.Events[0].Kind != apitypes.LifecycleItemAccepted {
		t.Errorf("readback events = %+v, want the accepted event", batch.Events)
	}
	if want := FormatCursor(history, head); batch.Cursor != want {
		t.Errorf("readback cursor = %q, want %q", batch.Cursor, want)
	}

	// A pre-call head at or above the stored head means nothing landed.
	if got := turns.readbackBatch(ctx, agent, head); got != nil {
		t.Errorf("readback at the head = %+v, want nil", got.Events)
	}
	if got := turns.readbackBatch(ctx, agent, head+5); got != nil {
		t.Errorf("readback past the head = %+v, want nil", got.Events)
	}
}

// A journal that filled its budget before shutdown is trimmed at boot:
// recovery runs retention before the service reports readiness.
func TestRecoveryPrunesAtBoot(t *testing.T) {
	oldEvery, oldRetain := journalPruneEvery, journalRetainPerAgent
	// A threshold the appends never reach keeps runtime pruning out of
	// the picture; only the boot-time pass may prune.
	journalPruneEvery, journalRetainPerAgent = 1<<30, 4
	t.Cleanup(func() { journalPruneEvery, journalRetainPerAgent = oldEvery, oldRetain })

	sqlDB := testdb.Open(t, false)
	master := bytes.Repeat([]byte{0x42}, 32)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), master, DefaultToolLimits())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	ctx := context.Background()
	for i := range 8 {
		id := "item-" + string(rune('0'+i))
		if _, err := turns.AcceptItem(ctx, "research", id, "p", "prompt", nil); err != nil {
			t.Fatalf("accept %s: %v", id, err)
		}
		if _, _, err := turns.StartItem(ctx, "research", id); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		if _, err := turns.FinishItem(ctx, "research", id, apitypes.ItemCompleted, "", nil); err != nil {
			t.Fatalf("finish %s: %v", id, err)
		}
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Reopening the same file runs recovery, which prunes: the floor
	// must advance even though the append counter never fired.
	reopened := testdb.Open(t, false)
	defer func() { _ = reopened.Close() }()
	if _, _, _, err := New(reopened, zerolog.Nop(), master, DefaultToolLimits()); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	var floor int64
	if err := reopened.QueryRow(
		`SELECT floor_pos FROM agent_sync_state WHERE agent_id = 'research'`).Scan(&floor); err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if floor <= 1 {
		t.Errorf("floor after boot recovery = %d, want retention to have pruned at boot", floor)
	}
}

// A started item's still-open invocations settle when recovery
// interrupts it: finished with no recorded result is the honest
// bookkeeping for an outcome nobody observed.
func TestRecoverySettlesOpenInvocations(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x42}, 32), DefaultToolLimits())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	ctx := context.Background()
	const agent, item = "research", "item-1"

	if _, err := turns.AcceptItem(ctx, agent, item, "hello", "prompt", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, item); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := turns.RecordToolStarted(ctx, agent, item, "inv-1", "call-1", "fetch", "{}"); err != nil {
		t.Fatalf("record tool start: %v", err)
	}

	if err := turns.recoverUnfinished(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	var state string
	if err := sqlDB.QueryRow(
		`SELECT state FROM tool_invocations WHERE agent_id = ? AND item_id = ? AND id = 'inv-1'`,
		agent, item).Scan(&state); err != nil {
		t.Fatalf("read invocation: %v", err)
	}
	if state != "finished" {
		t.Errorf("open invocation after recovery = %q, want finished", state)
	}
}

// The authoritative baseline carries retained cancellation notices: a
// cancellation mints no transcript row, so a fresh projection learns
// which submissions never ran from the snapshot itself.
func TestSyncSnapshotCarriesCancelledNotices(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x42}, 32), DefaultToolLimits())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	ctx := context.Background()
	const agent = "research"

	// One cancelled item and one completed turn: only the cancellation
	// earns a notice.
	if _, err := turns.AcceptItem(ctx, agent, "never-ran", "queued words", "prompt", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := turns.recoverUnfinished(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if _, err := turns.AcceptItem(ctx, agent, "item-2", "ran", "prompt", nil); err != nil {
		t.Fatalf("accept 2: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, "item-2"); err != nil {
		t.Fatalf("start 2: %v", err)
	}
	if _, err := turns.FinishItem(ctx, agent, "item-2", apitypes.ItemCompleted, "",
		[]ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "done", TurnID: "item-2"}}); err != nil {
		t.Fatalf("finish 2: %v", err)
	}

	snapshot, err := turns.SyncSnapshot(ctx, agent, 20)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var notice *apitypes.ThreadMessage
	for i := range snapshot.Page.Items {
		if snapshot.Page.Items[i].ID == "cancelled-never-ran" {
			notice = &snapshot.Page.Items[i]
		}
	}
	if notice == nil {
		t.Fatalf("snapshot items = %+v, want the cancellation notice", snapshot.Page.Items)
	}
	if notice.Role != apitypes.ThreadRoleError ||
		notice.Content != "cancelled (service_restart): queued words" {
		t.Errorf("notice = %+v, want the restart reason and submission", notice)
	}
	for _, row := range snapshot.Page.Items {
		if row.ID != "cancelled-never-ran" && strings.Contains(row.Content, "cancelled") {
			t.Errorf("unexpected notice %+v", row)
		}
	}
	// A notice is timestamped like any other display row, so the
	// baseline stays in transcript order: the answered turn below it
	// was committed after the cancellation.
	for i := 1; i < len(snapshot.Page.Items); i++ {
		if snapshot.Page.Items[i-1].CreatedAt > snapshot.Page.Items[i].CreatedAt {
			t.Fatalf("snapshot items out of order: %+v", snapshot.Page.Items)
		}
	}
}

// A terminal outcome settles every invocation the item still had open:
// the turn is over, so nothing can finish them afterwards.
func TestFinishItemSettlesOpenInvocations(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x42}, 32), DefaultToolLimits())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	ctx := context.Background()
	const agent, item = "research", "item-1"

	if _, err := turns.AcceptItem(ctx, agent, item, "hello", "prompt", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, item); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := turns.RecordToolStarted(ctx, agent, item, "inv-1", "call-1", "fetch", "{}"); err != nil {
		t.Fatalf("record tool start: %v", err)
	}
	if _, err := turns.FinishItem(ctx, agent, item, apitypes.ItemCompleted, "", nil); err != nil {
		t.Fatalf("finish: %v", err)
	}

	var state string
	if err := sqlDB.QueryRow(
		`SELECT state FROM tool_invocations WHERE agent_id = ? AND item_id = ? AND id = 'inv-1'`,
		agent, item).Scan(&state); err != nil {
		t.Fatalf("read invocation: %v", err)
	}
	if state != "finished" {
		t.Errorf("invocation after a terminal outcome = %q, want finished", state)
	}
}
