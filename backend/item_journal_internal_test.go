package backend

// Journal retention over the real store: the pruned prefix ends on a
// transaction boundary, the replay floor advances with it, terminal item
// bookkeeping no retained event needs is removed, and unresolved items
// survive — restart recovery still settles them from protected state.
// The budgets are package variables precisely so this test can shrink
// them.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestJournalRetentionPrunesPrefixAndProtectsUnresolved(t *testing.T) {
	oldEvery, oldRetain := journalPruneEvery, journalRetainPerAgent
	journalPruneEvery, journalRetainPerAgent = 1, 4
	t.Cleanup(func() { journalPruneEvery, journalRetainPerAgent = oldEvery, oldRetain })

	sqlDB := testdb.Open(t, true)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x42}, 32), DefaultToolLimits(), nil)
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

	// An unresolved item admitted before the retained window: its early
	// events age out, its bookkeeping must not.
	if _, err := turns.AcceptItem(ctx, agent, "orphan", "still queued", "prompt", nil); err != nil {
		t.Fatalf("accept orphan: %v", err)
	}
	for i := range 5 {
		id := fmt.Sprintf("item-%d", i)
		if _, err := turns.AcceptItem(ctx, agent, id, "prompt", "prompt", nil); err != nil {
			t.Fatalf("accept %s: %v", id, err)
		}
		if _, _, err := turns.StartItem(ctx, agent, id); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		if _, err := turns.FinishItem(ctx, agent, id, apitypes.ItemCompleted, "",
			[]ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "answer", TurnID: id}}); err != nil {
			t.Fatalf("finish %s: %v", id, err)
		}
	}

	history, head, floor, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	if head != 16 {
		t.Fatalf("head = %d, want 16", head)
	}
	if floor != head-journalRetainPerAgent+1 {
		t.Errorf("floor = %d, want %d (the oldest retained position)", floor, head-journalRetainPerAgent+1)
	}
	var stored int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ?`, agent).Scan(&stored); err != nil {
		t.Fatalf("count journal: %v", err)
	}
	if int64(stored) > journalRetainPerAgent {
		t.Errorf("retained events = %d, want at most %d", stored, journalRetainPerAgent)
	}
	// The floor names the oldest retained position, so the cursor one
	// below it has lost nothing — its page starts exactly at the floor —
	// while anything older has lost events.
	if _, err := turns.ReplayAfter(ctx, agent, history, floor-1, 100); err != nil {
		t.Errorf("replay at floor-1 err = %v, want resume", err)
	}
	if _, err := turns.ReplayAfter(ctx, agent, history, floor-2, 100); !errors.Is(err, ErrReplayExpired) {
		t.Errorf("replay below the floor err = %v, want ErrReplayExpired", err)
	}
	page, err := turns.ReplayAfter(ctx, agent, history, floor, 100)
	if err != nil {
		t.Fatalf("replay at the floor: %v", err)
	}
	// The cursor names the last applied position: a client at the floor
	// resumes with the event after it.
	if len(page.Events) == 0 || page.LastPos == 0 {
		t.Fatalf("page = %+v, want the retained events after the floor", page)
	}
	var first apitypes.LifecycleEvent
	if err := json.Unmarshal(page.Events[0], &first); err != nil {
		t.Fatalf("decode first replay event: %v", err)
	}
	if first.Pos != floor+1 {
		t.Errorf("page starts at %+v, want position %d", first, floor+1)
	}
	// Positions are never reissued: a prune that deleted through the old
	// cutoff still allocates the next event strictly above every prior
	// position.
	if _, err := turns.AcceptItem(ctx, agent, "post-prune", "prompt", "prompt", nil); err != nil {
		t.Fatalf("accept post-prune: %v", err)
	}
	_, newHead, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status after append: %v", err)
	}
	if newHead <= head {
		t.Errorf("head after prune+append = %d, want above the pre-prune head %d", newHead, head)
	}

	// The first settled item's events are gone, so its bookkeeping is
	// no longer needed; the message rows stay — retention never touches
	// conversation data.
	var items int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM queue_items WHERE agent_id = ? AND id = 'item-0'`, agent).Scan(&items); err != nil {
		t.Fatalf("count item-0: %v", err)
	}
	if items != 0 {
		t.Error("settled item bookkeeping survived pruning with no retained event")
	}
	if n := countThreadRows(t, sqlDB, "answer"); n < 5 {
		t.Errorf("answer rows = %d, want retention to keep every conversation row", n)
	}

	// The unresolved item is protected: its acceptance event aged out,
	// its bookkeeping did not, and recovery still cancels it.
	var state string
	if err := sqlDB.QueryRow(
		`SELECT state FROM queue_items WHERE agent_id = ? AND id = 'orphan'`, agent).Scan(&state); err != nil {
		t.Fatalf("read orphan state: %v", err)
	}
	if state != "accepted" {
		t.Errorf("orphan state = %q, want accepted across pruning", state)
	}
	if n := countJournalKind(t, sqlDB, agent, apitypes.LifecycleItemAccepted, "orphan"); n != 0 {
		t.Errorf("orphan acceptance events = %d, want the event pruned", n)
	}
	if err := turns.recoverUnfinished(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if err := sqlDB.QueryRow(
		`SELECT state FROM queue_items WHERE agent_id = ? AND id = 'orphan'`, agent).Scan(&state); err != nil {
		t.Fatalf("read orphan state after recovery: %v", err)
	}
	if state != apitypes.ItemCancelled {
		t.Errorf("orphan state after recovery = %q, want cancelled", state)
	}
}

// Replay must fail closed on a corrupt journal: a tail whose last batch
// never closes answers an error, never a partial page.
func TestReplayFailsOnOpenBatchTail(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x42}, 32), DefaultToolLimits(), nil)
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
	// Five single-event batches: acceptance is the smallest writer.
	for i := range 5 {
		id := fmt.Sprintf("item-%d", i)
		if _, err := turns.AcceptItem(ctx, agent, id, "prompt", "prompt", nil); err != nil {
			t.Fatalf("accept %s: %v", id, err)
		}
	}
	history, _, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	// Corrupt the journal the way a torn write would: the newest event
	// claims its batch continues past the stored tail.
	if _, err := sqlDB.Exec(
		`UPDATE lifecycle_journal SET batch_end = 0 WHERE agent_id = ? AND pos = 5`, agent); err != nil {
		t.Fatalf("clear batch_end: %v", err)
	}
	page, err := turns.ReplayAfter(ctx, agent, history, 0, 100)
	if err == nil || !strings.Contains(err.Error(), "journal tail ends inside a batch") {
		t.Fatalf("replay err = %v, page = %+v; want open-tail failure, not a partial page", err, page)
	}
}

// A batch that never meets its boundary within the page bound fails the
// read instead of extending across an unbounded journal.
func TestReplayFailsOnUnboundedPage(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, _, turns, err := New(sqlDB, zerolog.Nop(), bytes.Repeat([]byte{0x42}, 32), DefaultToolLimits(), nil)
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
	if _, err := turns.AcceptItem(ctx, agent, "bulk", "prompt", "prompt", nil); err != nil {
		t.Fatalf("accept bulk: %v", err)
	}
	history, _, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	// Boundary-less rows past the committed batch: the extension loop
	// must hit the bound, not page through all of them.
	for pos := int64(2); pos <= maxReplayPageRows+2; pos++ {
		if _, err := sqlDB.Exec(
			`INSERT INTO lifecycle_journal (agent_id, pos, item_id, kind, batch_end, payload, created_ms)
			 VALUES (?, ?, 'bulk', 'item_accepted', 0, '{}', ?)`, agent, pos, pos); err != nil {
			t.Fatalf("insert event %d: %v", pos, err)
		}
	}
	page, err := turns.ReplayAfter(ctx, agent, history, 0, 100)
	if err == nil || !strings.Contains(err.Error(), "replay page exceeded") {
		t.Fatalf("replay err = %v, page = %+v; want page-bound failure", err, page)
	}
}

func countThreadRows(t *testing.T, sqlDB *sql.DB, content string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM thread_messages WHERE content = ?`, content).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func countJournalKind(t *testing.T, sqlDB *sql.DB, agentID, kind, itemID string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ? AND kind = ? AND item_id = ?`,
		agentID, kind, itemID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}
