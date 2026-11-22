package backend_test

// The conversation lifecycle journal over the real store: item
// transitions commit with the rows and events they describe, replay
// answers complete transaction batches and refuses cursors it cannot
// resume, snapshots pair the page with one committed head, and restart
// recovery settles unfinished items exactly once.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// journalService builds the backend's turn service over a fresh store
// with one seeded agent, exactly as the binary does at boot.
func journalService(t *testing.T) (*backend.TurnService, *sql.DB) {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	_, _, turns, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	return turns, sqlDB
}

// countRows runs one count query.
func countRows(t *testing.T, sqlDB *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// itemState reads one item's recorded state.
func itemState(t *testing.T, sqlDB *sql.DB, agentID, itemID string) string {
	t.Helper()
	var state string
	if err := sqlDB.QueryRow(
		`SELECT state FROM queue_items WHERE agent_id = ? AND id = ?`,
		agentID, itemID).Scan(&state); err != nil {
		t.Fatalf("read item state: %v", err)
	}
	return state
}

// journalPosition reports the agent's committed head.
func journalPosition(t *testing.T, turns *backend.TurnService, agentID string) int64 {
	t.Helper()
	_, head, _, err := turns.JournalStatus(context.Background(), agentID)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	return head
}

// decodeReplayPage decodes a replay answer's verbatim payloads — tests
// assert on the typed events the bytes carry.
func decodeReplayPage(t *testing.T, page *backend.JournalPage) []apitypes.LifecycleEvent {
	t.Helper()
	events := make([]apitypes.LifecycleEvent, 0, len(page.Events))
	for _, raw := range page.Events {
		var ev apitypes.LifecycleEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("decode replay event: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestItemLifecycleCommitsRowsWithEvents(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"

	accepted, err := turns.AcceptItem(ctx, agent, item, "hello", "prompt", nil)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(accepted.Events) != 1 {
		t.Fatalf("accept events = %d, want 1", len(accepted.Events))
	}
	if ev := accepted.Events[0]; ev.Kind != apitypes.LifecycleItemAccepted ||
		ev.ItemID != item || ev.ItemKind != "prompt" || ev.Content != "hello" ||
		ev.Pos != 1 || !ev.BatchEnd {
		t.Errorf("accept event = %+v, want the accepted fact at position 1", ev)
	}
	if !strings.HasPrefix(accepted.Cursor, "v1:") || !strings.HasSuffix(accepted.Cursor, ":1") {
		t.Errorf("cursor = %q, want the v1 shape at position 1", accepted.Cursor)
	}

	start, started, err := turns.StartItem(ctx, agent, item)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if start.UserMessageID == "" {
		t.Fatalf("start = %+v, want the prompt's committed user row id", start)
	}
	if ev := started.Events[0]; ev.Kind != apitypes.LifecycleItemStarted ||
		ev.ThreadID != testdb.ThreadID(agent) ||
		ev.UserMessageID != start.UserMessageID || len(ev.Rows) != 1 ||
		ev.Rows[0].ID != start.UserMessageID {
		t.Errorf("start event = %+v, want the user row riding item_started on the active thread", ev)
	}
	// The user row rides item_started once — no separate row event.
	if n := countRows(t, sqlDB, `SELECT COUNT(*) FROM thread_messages WHERE agent_id = ? AND role = 'user'`, agent); n != 1 {
		t.Errorf("user rows = %d, want exactly 1", n)
	}
	if state := itemState(t, sqlDB, agent, item); state != "started" {
		t.Errorf("item state = %q, want started", state)
	}

	// A mid-turn call group commits as one message_committed event.
	if _, err := turns.AppendItemMessages(ctx, agent, item, []backend.ThreadMessageDraft{{
		Role:      apitypes.ThreadRoleAssistant,
		Content:   "checking",
		TurnID:    item,
		ToolCalls: []byte(`[{"id":"call-1","name":"fetch","arguments":"{}"}]`),
	}}); err != nil {
		t.Fatalf("append item messages: %v", err)
	}

	startedTool, err := turns.RecordToolStarted(ctx, agent, item, "inv-1", "call-1", "fetch", `{"url":"x"}`)
	if err != nil {
		t.Fatalf("record tool start: %v", err)
	}
	if ev := startedTool.Events[0]; ev.Kind != apitypes.LifecycleToolStarted ||
		ev.InvocationID != "inv-1" || ev.CallID != "call-1" {
		t.Errorf("tool start event = %+v, want the invocation identity", ev)
	}

	finishedTool, err := turns.RecordToolFinished(ctx, agent, item, "inv-1", "42", nil)
	if err != nil {
		t.Fatalf("record tool finish: %v", err)
	}
	outcomeRow := finishedTool.Events[0].Rows[0]
	if outcomeRow.Role != apitypes.ThreadRoleTool || outcomeRow.Content == "" {
		t.Fatalf("tool outcome row = %+v, want the committed envelope", outcomeRow)
	}
	var envelope backend.ToolEnvelope
	if err := json.Unmarshal([]byte(outcomeRow.Content), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.CallID != "call-1" || envelope.Result != "42" || envelope.Name != "fetch" {
		t.Errorf("envelope = %+v, want the recorded invocation", envelope)
	}
	if ev := finishedTool.Events[0]; ev.Kind != apitypes.LifecycleToolFinished || len(ev.Rows) != 1 {
		t.Errorf("tool finish event = %+v, want the outcome row riding it", ev)
	}

	finished, err := turns.FinishItem(ctx, agent, item, apitypes.ItemCompleted, "", []backend.ThreadMessageDraft{{
		Role: apitypes.ThreadRoleAssistant, Content: "done", TurnID: item,
	}})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if ev := finished.Events[0]; ev.Kind != apitypes.LifecycleItemFinished ||
		ev.Outcome != apitypes.ItemCompleted || len(ev.Rows) != 1 ||
		ev.Rows[0].Content != "done" {
		t.Errorf("finish event = %+v, want the terminal answer riding it", ev)
	}
	if state := itemState(t, sqlDB, agent, item); state != apitypes.ItemCompleted {
		t.Errorf("item state = %q, want completed", state)
	}

	// One terminal outcome per item: a second finish is refused and
	// writes nothing.
	if _, err := turns.FinishItem(ctx, agent, item, apitypes.ItemFailed, "", nil); err == nil {
		t.Fatal("second finish succeeded, want a state conflict")
	}
	if n := countRows(t, sqlDB,
		`SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ? AND kind = 'item_finished'`, agent); n != 1 {
		t.Errorf("item_finished events = %d, want exactly 1", n)
	}

	history, _, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	page, err := turns.ReplayAfter(ctx, agent, history, 0, 100)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	wantKinds := []string{
		apitypes.LifecycleItemAccepted,
		apitypes.LifecycleItemStarted,
		apitypes.LifecycleMessageCommitted,
		apitypes.LifecycleToolStarted,
		apitypes.LifecycleToolFinished,
		apitypes.LifecycleItemFinished,
	}
	events := decodeReplayPage(t, page)
	if len(events) != len(wantKinds) {
		t.Fatalf("replayed %d events, want %d", len(events), len(wantKinds))
	}
	for i, ev := range events {
		if ev.Kind != wantKinds[i] {
			t.Errorf("event %d = %q, want %q", i, ev.Kind, wantKinds[i])
		}
		if ev.Pos != int64(i+1) {
			t.Errorf("event %d position = %d, want %d", i, ev.Pos, i+1)
		}
		// Every mutator above commits alone, so every event closes its
		// own transaction batch.
		if !ev.BatchEnd {
			t.Errorf("event %d batchEnd = false, want every single-event batch closed", i)
		}
	}
	if got := journalPosition(t, turns, agent); got != int64(len(wantKinds)) {
		t.Errorf("head = %d, want %d", got, len(wantKinds))
	}
}

func TestFinishItemRollbackLeavesNeitherRowNorEvent(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"

	if _, err := turns.AcceptItem(ctx, agent, item, "hello", "prompt", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, item); err != nil {
		t.Fatalf("start: %v", err)
	}
	rowsBefore := countRows(t, sqlDB, `SELECT COUNT(*) FROM thread_messages WHERE agent_id = ?`, agent)
	eventsBefore := countRows(t, sqlDB, `SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ?`, agent)

	// Break the journal write inside the settlement transaction: the row
	// insert and the event must roll back together.
	if _, err := sqlDB.Exec(`CREATE TRIGGER break_journal BEFORE INSERT ON lifecycle_journal
		BEGIN SELECT RAISE(ABORT, 'journal unavailable'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := turns.FinishItem(ctx, agent, item, apitypes.ItemCompleted, "",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "done", TurnID: item}}); err == nil {
		t.Fatal("finish succeeded with the journal write broken, want an error")
	}
	if got := countRows(t, sqlDB, `SELECT COUNT(*) FROM thread_messages WHERE agent_id = ?`, agent); got != rowsBefore {
		t.Errorf("rows = %d, want %d: a rolled-back batch leaves no row", got, rowsBefore)
	}
	if got := countRows(t, sqlDB, `SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ?`, agent); got != eventsBefore {
		t.Errorf("events = %d, want %d: a rolled-back batch leaves no event", got, eventsBefore)
	}
	if state := itemState(t, sqlDB, agent, item); state != "started" {
		t.Errorf("item state = %q, want started after the rollback", state)
	}
	if _, err := sqlDB.Exec(`DROP TRIGGER break_journal`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if _, err := turns.FinishItem(ctx, agent, item, apitypes.ItemCompleted, "",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "done", TurnID: item}}); err != nil {
		t.Fatalf("finish after rollback: %v", err)
	}
}

func TestThreadSwitchBatchReplaysWhole(t *testing.T) {
	turns, _ := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "cmd-1"

	if _, err := turns.AcceptItem(ctx, agent, item, "/new", "new_thread", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	start, _, err := turns.StartItem(ctx, agent, item)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if start.UserMessageID != "" {
		t.Errorf("command start = %+v, want no user row", start)
	}
	activated, batch, err := turns.ExecuteThreadSwitch(ctx, agent, item)
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if len(batch.Events) != 2 {
		t.Fatalf("switch batch = %d events, want 2", len(batch.Events))
	}
	changed, done := batch.Events[0], batch.Events[1]
	if changed.Kind != apitypes.LifecycleThreadChanged || changed.ThreadID != activated.ID ||
		changed.PreviousThreadID != activated.PreviousID || changed.Divider == nil ||
		changed.Divider.ID != activated.ID || changed.Divider.Content != "New session" {
		t.Errorf("thread_changed = %+v, want the committed boundary", changed)
	}
	if changed.BatchEnd {
		t.Error("thread_changed marked batch end, want the boundary on the last event only")
	}
	if done.Kind != apitypes.LifecycleItemFinished || done.Outcome != apitypes.ItemCompleted ||
		done.ThreadID != activated.ID || !done.BatchEnd {
		t.Errorf("item_finished = %+v, want completed on the new thread", done)
	}

	// A page that stops inside the transaction extends to its boundary:
	// the switch committed two events, one batch.
	history, _, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	page, err := turns.ReplayAfter(ctx, agent, history, 2, 1)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	events := decodeReplayPage(t, page)
	if len(events) != 2 {
		t.Fatalf("replayed %d events at limit 1, want the whole 2-event batch", len(events))
	}
	if !events[len(events)-1].BatchEnd {
		t.Error("page does not end on a transaction boundary")
	}
	following, err := turns.ReplayAfter(ctx, agent, history, 4, 10)
	if err != nil {
		t.Fatalf("replay at head: %v", err)
	}
	if len(following.Events) != 0 {
		t.Errorf("replay at head = %+v, want an empty page", following)
	}
}

func TestReplayCursorBounds(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"

	if _, err := turns.AcceptItem(ctx, agent, item, "hello", "prompt", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, item); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := turns.FinishItem(ctx, agent, item, apitypes.ItemStopped, "", nil); err != nil {
		t.Fatalf("finish: %v", err)
	}
	head := journalPosition(t, turns, agent)
	if head != 3 {
		t.Fatalf("head = %d, want 3", head)
	}
	history, _, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}

	// A cursor ahead of the committed head cannot resume.
	if _, err := turns.ReplayAfter(ctx, agent, history, head+1, 10); !errors.Is(err, backend.ErrReplayAhead) {
		t.Fatalf("ahead err = %v, want ErrReplayAhead", err)
	}
	// A cursor from another database's history never resumes here.
	if _, err := turns.ReplayAfter(ctx, agent, "another-history", 0, 10); !errors.Is(err, backend.ErrReplayForeign) {
		t.Fatalf("foreign err = %v, want ErrReplayForeign", err)
	}
	// Retention advances the floor: the cursor one below it resumes at
	// the floor itself, anything older must snapshot. The head stays
	// resumable, including an emptied journal.
	if _, err := sqlDB.Exec(`UPDATE agent_sync_state SET floor_pos = 2 WHERE agent_id = ?`, agent); err != nil {
		t.Fatalf("advance floor: %v", err)
	}
	if _, err := turns.ReplayAfter(ctx, agent, history, 0, 10); !errors.Is(err, backend.ErrReplayExpired) {
		t.Fatalf("expired err = %v, want ErrReplayExpired", err)
	}
	atEdge, err := turns.ReplayAfter(ctx, agent, history, 1, 10)
	if err != nil {
		t.Fatalf("replay at floor-1: %v", err)
	}
	atEdgeEvents := decodeReplayPage(t, atEdge)
	if len(atEdgeEvents) != 2 || atEdgeEvents[0].Pos != 2 {
		t.Errorf("floor-1 page = %+v, want the retained events from the floor", atEdgeEvents)
	}
	page, err := turns.ReplayAfter(ctx, agent, history, 2, 10)
	if err != nil {
		t.Fatalf("replay at floor: %v", err)
	}
	pageEvents := decodeReplayPage(t, page)
	if len(pageEvents) != 1 || pageEvents[0].Pos != 3 {
		t.Errorf("page = %+v, want the retained event after the floor", pageEvents)
	}
	if _, err := turns.ReplayAfter(ctx, agent, history, head, 10); err != nil {
		t.Fatalf("replay at head: %v", err)
	}

	// Unknown agent: replaying from zero answers an empty journal.
	empty, err := turns.ReplayAfter(ctx, "ghost", history, 0, 10)
	if err != nil {
		t.Fatalf("replay unknown agent: %v", err)
	}
	if len(empty.Events) != 0 {
		t.Errorf("unknown agent page = %+v, want empty", empty.Events)
	}
}

func TestParseCursorShape(t *testing.T) {
	formatted := backend.FormatCursor("abc123", 42)
	if formatted != "v1:abc123:42" {
		t.Errorf("formatted = %q, want v1:abc123:42", formatted)
	}
	historyID, pos, ok := backend.ParseCursor(formatted)
	if !ok || historyID != "abc123" || pos != 42 {
		t.Errorf("parsed = %q/%d/%v, want abc123/42/true", historyID, pos, ok)
	}
	for _, raw := range []string{
		"", "42", "v1:", "v1::42", "v1:abc", "v1:abc:", "v1:abc:-1",
		"v1:abc:1x", "v2:abc:1", "abc:1",
	} {
		if _, _, ok := backend.ParseCursor(raw); ok {
			t.Errorf("cursor %q parsed, want rejection", raw)
		}
	}
	// Position 0 is the shape of a cursor that applied nothing.
	if _, pos, ok := backend.ParseCursor(backend.FormatCursor("abc", 0)); !ok || pos != 0 {
		t.Errorf("zero cursor rejected, want position 0")
	}
}

func TestSyncSnapshotPairsPageWithHead(t *testing.T) {
	turns, _ := journalService(t)
	ctx := context.Background()
	const agent, item = "research", "item-1"

	// An agent with no journal still answers a baseline.
	empty, err := turns.SyncSnapshot(ctx, agent, 20)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if empty.Page == nil || empty.Page.ThreadID != testdb.ThreadID(agent) {
		t.Fatalf("empty snapshot = %+v, want the active thread at head zero", empty)
	}
	if _, pos, ok := backend.ParseCursor(empty.Cursor); !ok || pos != 0 {
		t.Fatalf("empty snapshot cursor = %q, want position zero", empty.Cursor)
	}
	if len(empty.Page.Items) != 0 {
		t.Errorf("empty snapshot items = %+v, want none", empty.Page.Items)
	}

	if _, err := turns.AcceptItem(ctx, agent, item, "hello", "prompt", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, item); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := turns.FinishItem(ctx, agent, item, apitypes.ItemCompleted, "",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "hi", TurnID: item}}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	snapshot, err := turns.SyncSnapshot(ctx, agent, 20)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	historyID, head, _, err := turns.JournalStatus(ctx, agent)
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	if snapshot.Cursor != backend.FormatCursor(historyID, head) {
		t.Errorf("snapshot cursor = %q, want the committed head %q",
			snapshot.Cursor, backend.FormatCursor(historyID, head))
	}
	// The page carries the committed conversation up to that head.
	var roles []apitypes.ThreadRole
	for _, row := range snapshot.Page.Items {
		roles = append(roles, row.Role)
	}
	if len(roles) != 2 || roles[0] != apitypes.ThreadRoleUser || roles[1] != apitypes.ThreadRoleAssistant {
		t.Errorf("page roles = %v, want the committed turn", roles)
	}

	if _, err := turns.SyncSnapshot(ctx, "ghost", 20); err == nil {
		t.Error("snapshot for an unknown agent succeeded, want ErrUnknownAgent")
	}
}

func TestRestartRecoverySettlesUnfinishedItems(t *testing.T) {
	sqlDB := testdb.Open(t, false)
	_, _, first, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	ctx := context.Background()

	// One admitted-but-never-started item, one started item, one settled
	// item: only the first two are recovery's business.
	if _, err := first.AcceptItem(ctx, "research", "queued", "never ran", "prompt", nil); err != nil {
		t.Fatalf("accept queued: %v", err)
	}
	if _, err := first.AcceptItem(ctx, "research", "running", "was running", "prompt", nil); err != nil {
		t.Fatalf("accept running: %v", err)
	}
	if _, _, err := first.StartItem(ctx, "research", "running"); err != nil {
		t.Fatalf("start running: %v", err)
	}
	var runningThread string
	if err := sqlDB.QueryRow(
		`SELECT thread_id FROM queue_items WHERE agent_id = 'research' AND id = 'running'`).
		Scan(&runningThread); err != nil {
		t.Fatalf("read running thread: %v", err)
	}
	if _, err := first.AcceptItem(ctx, "research", "settled", "done already", "prompt", nil); err != nil {
		t.Fatalf("accept settled: %v", err)
	}
	if _, _, err := first.StartItem(ctx, "research", "settled"); err != nil {
		t.Fatalf("start settled: %v", err)
	}
	if _, err := first.FinishItem(ctx, "research", "settled", apitypes.ItemCompleted, "", nil); err != nil {
		t.Fatalf("finish settled: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Reopening the same database is the restart: recovery runs at boot.
	reopened := testdb.Open(t, false)
	_, _, turns, err := backend.New(reopened, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New after restart: %v", err)
	}
	if state := itemState(t, reopened, "research", "queued"); state != apitypes.ItemCancelled {
		t.Errorf("queued state = %q, want cancelled", state)
	}
	if state := itemState(t, reopened, "research", "running"); state != apitypes.ItemInterrupted {
		t.Errorf("running state = %q, want interrupted", state)
	}
	if state := itemState(t, reopened, "research", "settled"); state != apitypes.ItemCompleted {
		t.Errorf("settled state = %q, want the committed outcome untouched", state)
	}
	// The interruption is one display notice in the item's own thread;
	// the queued item's text never became a transcript row.
	if n := countRows(t, reopened,
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = 'research' AND role = 'error'`); n != 1 {
		t.Errorf("interruption rows = %d, want 1", n)
	}
	var notice, noticeThread string
	if err := reopened.QueryRow(
		`SELECT content, thread_id FROM thread_messages WHERE agent_id = 'research' AND role = 'error'`).
		Scan(&notice, &noticeThread); err != nil {
		t.Fatalf("read notice: %v", err)
	}
	if notice != "interrupted by service restart" || noticeThread != runningThread {
		t.Errorf("notice = %q in %q, want the restart notice in the started item's thread",
			notice, noticeThread)
	}
	if n := countRows(t, reopened,
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = 'research' AND content = 'never ran'`); n != 0 {
		t.Errorf("queued submission rows = %d, want none", n)
	}

	// The cancellation notice names the submission from the retained
	// event, and its terminal outcome is durable.
	history, _, _, statusErr := turns.JournalStatus(ctx, "research")
	if statusErr != nil {
		t.Fatalf("journal status: %v", statusErr)
	}
	page, err := turns.ReplayAfter(ctx, "research", history, 0, 200)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	var cancelled *apitypes.LifecycleEvent
	for _, ev := range decodeReplayPage(t, page) {
		if ev.Kind == apitypes.LifecycleItemFinished && ev.ItemID == "queued" {
			cancelled = &ev
		}
	}
	if cancelled == nil {
		t.Fatal("no terminal event for the cancelled item")
	}
	if cancelled.Outcome != apitypes.ItemCancelled || cancelled.Content != "never ran" ||
		cancelled.Reason != "service_restart" || len(cancelled.Rows) != 0 {
		t.Errorf("cancelled event = %+v, want the submission and no rows", cancelled)
	}

	// Recovery is idempotent: another restart writes nothing new.
	head := journalPosition(t, turns, "research")
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
	third := testdb.Open(t, false)
	_, _, again, err := backend.New(third, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New after second restart: %v", err)
	}
	if got := journalPosition(t, again, "research"); got != head {
		t.Errorf("head after second recovery = %d, want %d", got, head)
	}
	if n := countRows(t, third,
		`SELECT COUNT(*) FROM thread_messages WHERE agent_id = 'research' AND role = 'error'`); n != 1 {
		t.Errorf("interruption rows after second recovery = %d, want 1", n)
	}
}
