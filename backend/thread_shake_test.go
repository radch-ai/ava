package backend_test

// /shake's durable half: the row selection, the protection of the
// active tail, the idempotent flag flip, the terminal batch, and the
// retrieval read that restores exactly what the projection elided.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// callDecl is the declaration shorthand the fixtures below use.
func callDecl(id, name string) backend.AgentThreadCall {
	return backend.AgentThreadCall{ID: id, Name: name, Arguments: `{}`}
}

// runShake admits and starts one shake item, then runs it: the real
// item transitions the gateway drives.
func runShake(t *testing.T, turns *backend.TurnService, agentID, itemID string) int {
	t.Helper()
	ctx := context.Background()
	if _, err := turns.AcceptItem(ctx, agentID, itemID, "", "shake", nil); err != nil {
		t.Fatalf("accept shake: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agentID, itemID); err != nil {
		t.Fatalf("start shake: %v", err)
	}
	res, _, err := turns.ShakeThreadContext(ctx, agentID, itemID)
	if err != nil {
		t.Fatalf("shake: %v", err)
	}
	return res
}

// wantReference is the placeholder literal the projection emits — pinned
// here so a wording change is a visible test failure.
func wantReference(rowID string, size int) string {
	return fmt.Sprintf("[elided tool output: %d bytes. Restore it with the read_tool_output tool: {\"reference\":%q}]", size, rowID)
}

// rowElided reads one row's flag.
func rowElided(t *testing.T, sqlDB *sql.DB, agentID, rowID string) int {
	t.Helper()
	var elided int
	if err := sqlDB.QueryRow(
		`SELECT elided FROM thread_messages WHERE agent_id = ? AND id = ?`,
		agentID, rowID).Scan(&elided); err != nil {
		t.Fatalf("read row %s: %v", rowID, err)
	}
	return elided
}

// toolMessage finds the projected outcome for one call id.
func toolMessage(t *testing.T, msgs []backend.AgentThreadMessage, callID string) backend.AgentThreadMessage {
	t.Helper()
	for _, m := range msgs {
		if m.Role == apitypes.ThreadRoleTool && m.ToolCallID == callID {
			return m
		}
	}
	t.Fatalf("no projected tool message for call %q", callID)
	return backend.AgentThreadMessage{}
}

// The selection rule, one thread: old large results elide (an error
// result included, because the projection emits it verbatim), small
// results keep immediacy, and the newest turn is never touched.
func TestShakeElidesOldLargeToolRows(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent = "research"

	big := strings.Repeat("x", 3000)
	small := strings.Repeat("s", 100)
	errText := strings.Repeat("e", 3000)
	drafts := []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, big, nil), // row 2
		userDraft("t2 prompt", "t2"),
		groupDraft(t, "t2", "", callDecl("c2", "fetch")),
		toolDraft(t, "t2", "c2", "fetch", `{}`, small, nil), // row 5
		userDraft("t3 prompt", "t3"),
		groupDraft(t, "t3", "", callDecl("c3", "fetch")),
		toolDraft(t, "t3", "c3", "fetch", `{}`, "", &errText), // row 8
		userDraft("t4 prompt", "t4"),
		groupDraft(t, "t4", "", callDecl("c4", "fetch")),
		toolDraft(t, "t4", "c4", "fetch", `{}`, big, nil), // row 11
	}
	seedRows(t, sqlDB, agent, drafts)

	res := runShake(t, turns, agent, "shake-1")
	if res != 2 {
		t.Fatalf("elided rows = %d, want the two old large results", res)
	}
	for _, row := range []struct {
		id   string
		want int
	}{
		{"research-row-2", 1},  // old, large: elided
		{"research-row-5", 0},  // old, small: immediacy wins
		{"research-row-8", 1},  // old, large error result: elided
		{"research-row-11", 0}, // newest turn: protected
		{"research-row-0", 0},  // user rows are never candidates
		{"research-row-1", 0},  // assistant rows are never candidates
	} {
		if got := rowElided(t, sqlDB, agent, row.id); got != row.want {
			t.Errorf("row %s elided = %d, want %d", row.id, got, row.want)
		}
	}

	// The projection now emits placeholders for the elided rows and the
	// untouched payloads for the rest.
	msgs, err := turns.GetAgentThread(ctx, agent)
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	elided := toolMessage(t, msgs, "c1")
	if !elided.Elided {
		t.Error("c1 outcome not marked elided")
	}
	if want := wantReference("research-row-2", len(big)); elided.Content != want {
		t.Errorf("c1 content = %q, want %q", elided.Content, want)
	}
	if got := toolMessage(t, msgs, "c2"); got.Elided || got.Content != small {
		t.Errorf("c2 outcome = %+v, want the untouched small result", got)
	}
	if got := toolMessage(t, msgs, "c3"); !got.Elided || got.Content != wantReference("research-row-8", len(errText)) {
		t.Errorf("c3 outcome = %+v, want the elided error placeholder", got)
	}
	if got := toolMessage(t, msgs, "c4"); got.Elided || got.Content != big {
		t.Errorf("c4 outcome changed, want the protected tail untouched")
	}

	// The stored originals are intact: elision is a projection fact.
	for rowID, want := range map[string]string{
		"research-row-2": big,
		"research-row-8": errText,
	} {
		got, err := turns.ReadToolOutput(ctx, agent, rowID)
		if err != nil {
			t.Fatalf("read %s: %v", rowID, err)
		}
		if got != want {
			t.Errorf("read %s = %d bytes, want the original %d", rowID, len(got), len(want))
		}
	}

	// A second shake finds nothing left to elide and still completes.
	if res := runShake(t, turns, agent, "shake-2"); res != 0 {
		t.Errorf("second shake elided = %d, want a zero-elision no-op", res)
	}
	if got := itemState(t, sqlDB, agent, "shake-2"); got != apitypes.ItemCompleted {
		t.Errorf("shake item state = %q, want completed", got)
	}
}

// The eligibility floor counts bytes, not characters: multibyte
// output past the byte floor must elide even when its character count
// sits below it.
func TestShakeElidesMultibyteResultPastByteFloor(t *testing.T) {
	turns, sqlDB := journalService(t)
	const agent = "research"
	cjk := strings.Repeat("世", 1500) // 4500 bytes, 1500 characters
	seedRows(t, sqlDB, agent, []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, cjk, nil), // row 2
		userDraft("t2 prompt", "t2"),
		groupDraft(t, "t2", "", callDecl("c2", "fetch")),
		toolDraft(t, "t2", "c2", "fetch", `{}`, "tail", nil), // row 5
	})

	if res := runShake(t, turns, agent, "shake-1"); res != 1 {
		t.Fatalf("elided = %d, want 1: the 4500-byte result is past the byte floor", res)
	}
	if got := rowElided(t, sqlDB, agent, "research-row-2"); got != 1 {
		t.Errorf("multibyte row elided = %d, want 1", got)
	}
}

// The elision floor is exact: a result of exactly ShakeMinResultBytes
// leaves, one byte under it stays — the envelope-decoding exact check
// is the authority the SQL prefilter only approximates.
func TestShakeFloorBoundary(t *testing.T) {
	turns, sqlDB := journalService(t)
	const agent = "research"
	atFloor := strings.Repeat("a", 2048)
	underFloor := strings.Repeat("b", 2047)
	seedRows(t, sqlDB, agent, []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, atFloor, nil), // row 2
		groupDraft(t, "t1", "", callDecl("c2", "fetch")),
		toolDraft(t, "t1", "c2", "fetch", `{}`, underFloor, nil), // row 4
		userDraft("t2 chat, no tools", "t2"),
	})

	if res := runShake(t, turns, agent, "shake-1"); res != 1 {
		t.Fatalf("elided = %d, want exactly the at-floor row", res)
	}
	if got := rowElided(t, sqlDB, agent, "research-row-2"); got != 1 {
		t.Errorf("at-floor row elided = %d, want 1", got)
	}
	if got := rowElided(t, sqlDB, agent, "research-row-4"); got != 0 {
		t.Errorf("under-floor row elided = %d, want kept", got)
	}
}

// The protection anchor is the thread's newest turn, whatever rows it
// carries: a newest turn without tool rows of its own must not shield
// older payloads -- /shake still elides them.
func TestShakeElidesWhenNewestTurnHasNoToolRows(t *testing.T) {
	turns, sqlDB := journalService(t)
	const agent = "research"
	big := strings.Repeat("x", 3000)
	seedRows(t, sqlDB, agent, []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, big, nil),
		userDraft("t2 chat, no tools", "t2"), // newest turn: no tool rows
	})

	if res := runShake(t, turns, agent, "shake-1"); res != 1 {
		t.Fatalf("elided rows = %d, want 1: the toolless newest turn must not protect the old payload", res)
	}
	if got := rowElided(t, sqlDB, agent, "research-row-2"); got != 1 {
		t.Errorf("old tool row elided = %d, want 1", got)
	}
}

// One shake commits exactly one lifecycle event: the item's completion
// in the active thread. No thread rotates and no row is minted.
func TestShakeCommitsOneTerminalEvent(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent = "research"
	seedRows(t, sqlDB, agent, []backend.ThreadMessageDraft{
		userDraft("prompt", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, strings.Repeat("x", 4096), nil),
		// The newest turn still owns tool rows: it is the protected
		// tail, so its outcome keeps its payload.
		userDraft("follow-up", "t2"),
		groupDraft(t, "t2", "", callDecl("c2", "fetch")),
		toolDraft(t, "t2", "c2", "fetch", `{}`, "tail", nil),
	})

	if _, err := turns.AcceptItem(ctx, agent, "shake-1", "", "shake", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, "shake-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	before := countRows(t, sqlDB, `SELECT COUNT(*) FROM thread_messages`)
	res, batch, err := turns.ShakeThreadContext(ctx, agent, "shake-1")
	if err != nil {
		t.Fatalf("shake: %v", err)
	}
	if res != 1 {
		t.Errorf("elided = %d, want 1", res)
	}
	if len(batch.Events) != 1 {
		t.Fatalf("batch events = %d, want exactly the completion", len(batch.Events))
	}
	ev := batch.Events[0]
	if ev.Kind != apitypes.LifecycleItemFinished || ev.ItemID != "shake-1" ||
		ev.ItemKind != "shake" || ev.ThreadID != testdb.ThreadID(agent) ||
		ev.Outcome != apitypes.ItemCompleted || !ev.BatchEnd || len(ev.Rows) != 0 {
		t.Errorf("event = %+v, want item_finished(completed) in the active thread", ev)
	}
	if after := countRows(t, sqlDB, `SELECT COUNT(*) FROM thread_messages`); after != before {
		t.Errorf("thread rows = %d, want %d: shaking mints no row", after, before)
	}
	if got := itemState(t, sqlDB, agent, "shake-1"); got != apitypes.ItemCompleted {
		t.Errorf("item state = %q, want completed", got)
	}
}

// The operation refuses anything but a started shake item.
func TestShakeRejectsWrongItem(t *testing.T) {
	turns, _ := journalService(t)
	ctx := context.Background()
	const agent = "research"

	if _, err := turns.AcceptItem(ctx, agent, "p1", "hello", "prompt", nil); err != nil {
		t.Fatalf("accept prompt: %v", err)
	}
	if _, _, err := turns.StartItem(ctx, agent, "p1"); err != nil {
		t.Fatalf("start prompt: %v", err)
	}
	if _, _, err := turns.ShakeThreadContext(ctx, agent, "p1"); err == nil ||
		!strings.Contains(err.Error(), `has kind "prompt"`) {
		t.Errorf("prompt item shake error = %v, want the kind guard", err)
	}

	if _, err := turns.AcceptItem(ctx, agent, "s1", "", "shake", nil); err != nil {
		t.Fatalf("accept shake: %v", err)
	}
	if _, _, err := turns.ShakeThreadContext(ctx, agent, "s1"); err == nil ||
		!strings.Contains(err.Error(), "item is not started") {
		t.Errorf("unstarted shake error = %v, want the start guard", err)
	}
}

// Elision is durable: a fresh service over the same file projects the
// placeholder exactly as the writing process did.
func TestShakeElisionSurvivesRestart(t *testing.T) {
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
	big := strings.Repeat("x", 3000)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("prompt", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, big, nil),
		userDraft("prompt 2", "t2"),
		groupDraft(t, "t2", "", callDecl("c2", "fetch")),
		toolDraft(t, "t2", "c2", "fetch", `{}`, "tail", nil),
	})
	runShake(t, turns, "research", "shake-1")

	_, _, cold, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("reopen backend: %v", err)
	}
	msgs, err := cold.GetAgentThread(context.Background(), "research")
	if err != nil {
		t.Fatalf("cold GetAgentThread: %v", err)
	}
	got := toolMessage(t, msgs, "c1")
	if !got.Elided {
		t.Error("cold projection lost the elided flag")
	}
	if want := wantReference("research-row-2", len(big)); got.Content != want {
		t.Errorf("cold content = %q, want %q", got.Content, want)
	}
	if origin, err := cold.ReadToolOutput(context.Background(), "research", "research-row-2"); err != nil || origin != big {
		t.Errorf("cold retrieval = %d bytes, %v; want the original", len(origin), err)
	}
}

// Retrieval answers only elided rows: it returns the exact stored text,
// truncates above the cap on a rune boundary with a marker, and treats
// every unusable reference — unknown, foreign, or still in context — as
// unknown.
func TestReadToolOutput(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "other", Name: "Other", Icon: "globe",
		SoulPersonality: "p", SoulIdentity: "i",
		OperatingInstructions: "o", ToolsContext: "t",
	})

	// The shared delivered-output marker: the head keeps cap-minus-marker
	// bytes so the delivered text never exceeds the ceiling.
	const marker = "…[truncated by output limit]"
	exact := strings.Repeat("a", 100)
	huge := strings.Repeat("b", 70000)
	cjk := strings.Repeat("世", 40000)                           // 3-byte runes: the cap cut lands mid-rune
	jsonHuge := `{"blob":"` + strings.Repeat("x", 70000) + `"}` // valid JSON
	inContext := strings.Repeat("p", 3000)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		toolDraft(t, "t1", "c1", "fetch", `{}`, exact, nil),     // research-row-0
		toolDraft(t, "t1", "c2", "fetch", `{}`, huge, nil),      // research-row-1
		toolDraft(t, "t1", "c3", "fetch", `{}`, cjk, nil),       // research-row-2
		toolDraft(t, "t1", "c4", "fetch", `{}`, jsonHuge, nil),  // research-row-3
		toolDraft(t, "t1", "c5", "fetch", `{}`, inContext, nil), // research-row-4: never elided
	})
	seedRows(t, sqlDB, "other", []backend.ThreadMessageDraft{
		toolDraft(t, "t1", "c1", "fetch", `{}`, "other agent", nil),
	})
	if _, err := sqlDB.Exec(
		`UPDATE thread_messages SET elided = 1 WHERE agent_id = 'research' AND id IN ('research-row-0', 'research-row-1', 'research-row-2', 'research-row-3')`); err != nil {
		t.Fatalf("mark elided: %v", err)
	}

	if got, err := turns.ReadToolOutput(ctx, "research", "research-row-0"); err != nil || got != exact {
		t.Errorf("exact read = %q, %v; want the stored text", got, err)
	}
	// 65536 - len(marker): the marker rides inside the cap.
	got, err := turns.ReadToolOutput(ctx, "research", "research-row-1")
	want := huge[:65506] + marker
	if err != nil || got != want {
		t.Errorf("capped read = %d bytes, %v; want the marker-inside-cap truncation", len(got), err)
	}
	// The cut lands one byte into a 3-byte rune: the head backs onto
	// the boundary at 65505 and stays valid UTF-8.
	got, err = turns.ReadToolOutput(ctx, "research", "research-row-2")
	want = cjk[:65505] + marker
	if err != nil || got != want {
		t.Errorf("rune-boundary read = %d bytes, %v; want the boundary-trimmed original", len(got), err)
	}
	// Structured output is never sliced into malformed JSON: the
	// restore answers with the same overflow denial a catalog tool's
	// delivered outcome would.
	got, err = turns.ReadToolOutput(ctx, "research", "research-row-3")
	if err != nil || !strings.Contains(got, `"code":"output_limit_exceeded"`) {
		t.Errorf("overflow read = %q, %v; want the output_limit_exceeded denial", got, err)
	}
	// An un-elided row's payload is already in the model's context: its
	// reference reads as unknown, like every other unusable one.
	if _, err := turns.ReadToolOutput(ctx, "research", "research-row-4"); err == nil {
		t.Error("un-elided reference read succeeded")
	}
	if _, err := turns.ReadToolOutput(ctx, "research", "nope"); err == nil {
		t.Error("unknown reference read succeeded")
	}
	if _, err := turns.ReadToolOutput(ctx, "research", ""); err == nil {
		t.Error("empty reference read succeeded")
	}
	// Another agent's row is unknown to this agent, never readable.
	if _, err := turns.ReadToolOutput(ctx, "research", "other-row-0"); err == nil {
		t.Error("cross-agent reference read succeeded")
	}
}

// The itemless shake elides the same rows /shake does without settling
// an item or journaling an event: the prompt whose claim armed it is
// still queued, and the turn that follows publishes everything
// observable.
func TestShakeAgentContextElidesWithoutSettling(t *testing.T) {
	turns, sqlDB := journalService(t)
	ctx := context.Background()
	const agent = "research"

	big := strings.Repeat("x", 3000)
	drafts := []backend.ThreadMessageDraft{
		userDraft("t1 prompt", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, big, nil), // row 3: elidable
		userDraft("t2 prompt", "t2"),
		groupDraft(t, "t2", "", callDecl("c2", "fetch")),
		toolDraft(t, "t2", "c2", "fetch", `{}`, big, nil), // row 6: newest turn, protected
	}
	seedRows(t, sqlDB, agent, drafts)

	// A queued prompt arms the shake; it must stay untouched.
	if _, err := turns.AcceptItem(ctx, agent, "prompt-1", "hello", "prompt", nil); err != nil {
		t.Fatalf("accept prompt: %v", err)
	}
	head := journalPosition(t, turns, agent)
	journalRows := countRows(t, sqlDB, `SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ?`, agent)

	elided, err := turns.ShakeAgentContext(ctx, agent)
	if err != nil {
		t.Fatalf("shake agent context: %v", err)
	}
	if elided != 1 {
		t.Fatalf("elided = %d, want the one old oversized row", elided)
	}
	if got := rowElided(t, sqlDB, agent, "research-row-2"); got != 1 {
		t.Errorf("old row elided = %d, want 1", got)
	}
	if got := rowElided(t, sqlDB, agent, "research-row-5"); got != 0 {
		t.Errorf("newest-turn row elided = %d, want 0 (protected)", got)
	}
	if got := itemState(t, sqlDB, agent, "prompt-1"); got != "accepted" {
		t.Errorf("queued prompt state = %q, want accepted", got)
	}
	if got := countRows(t, sqlDB, `SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ?`, agent); got != journalRows {
		t.Errorf("lifecycle_journal rows = %d, want %d (zero events)", got, journalRows)
	}
	if got := journalPosition(t, turns, agent); got != head {
		t.Errorf("journal head = %d, want %d (unchanged)", got, head)
	}

	// A second run finds nothing left to elide and flips nothing.
	elided, err = turns.ShakeAgentContext(ctx, agent)
	if err != nil {
		t.Fatalf("second shake agent context: %v", err)
	}
	if elided != 0 {
		t.Fatalf("second elided = %d, want 0", elided)
	}
	if got := rowElided(t, sqlDB, agent, "research-row-5"); got != 0 {
		t.Errorf("newest-turn row elided after rerun = %d, want 0", got)
	}
	if got := countRows(t, sqlDB, `SELECT COUNT(*) FROM lifecycle_journal WHERE agent_id = ?`, agent); got != journalRows {
		t.Errorf("lifecycle_journal rows after rerun = %d, want %d", got, journalRows)
	}
}
