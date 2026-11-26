package backend_test

// The reference-aware projection: a mid-loop rotation's successor reads
// back summary, then its referenced rows in retained order, then its own
// new rows — in both the model context and the display page — while the
// source thread stops showing the rows a successor carries. Anchors may
// name referenced rows, keyset paging walks the union, and the placement
// survives a restart.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/threadpage"
)

// rotateViaRefs simulates a mid-loop rotation's committed shape with raw
// SQL: a compact successor linked to the active thread, a seeded summary
// row, ordered references to the named source rows, and the pointer
// swap. Returns the source thread and the successor.
func rotateViaRefs(t *testing.T, sqlDB *sql.DB, agent, summary string, refs []string) (source, successor string) {
	t.Helper()
	if err := sqlDB.QueryRow(`SELECT active_thread_id FROM agents WHERE id = ?`, agent).Scan(&source); err != nil {
		t.Fatalf("read active thread: %v", err)
	}
	successor = agent + "-succ"
	base := time.Now().Add(time.Hour).UnixMilli()
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("begin rotation: %v", err)
	}
	stmts := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO threads (id, agent_id, previous_thread_id, kind, created_at)
		 VALUES (?, ?, ?, 'compact', ?)`, []any{successor, agent, source, base}},
		{`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id, is_summary)
		 VALUES (?, ?, 'assistant', ?, ?, '', ?, 1)`, []any{successor + "-summary", agent, summary, base + 1, successor}},
		{`UPDATE agents SET active_thread_id = ? WHERE id = ?`, []any{successor, agent}},
	}
	for i, s := range stmts {
		if _, err := tx.Exec(s.q, s.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("rotation stmt %d: %v", i, err)
		}
	}
	for i, id := range refs {
		if _, err := tx.Exec(
			`INSERT INTO thread_context_refs (agent_id, thread_id, position, message_id)
			 VALUES (?, ?, ?, ?)`, agent, successor, i, id); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert ref %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit rotation: %v", err)
	}
	return source, successor
}

// seedSuccessorRow writes one more physical row into a thread — the
// continuation a running turn writes after the rotation.
func seedSuccessorRow(t *testing.T, sqlDB *sql.DB, agent, threadID, id, role, content string) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES (?, ?, ?, ?, ?, 't2', ?)`,
		id, agent, role, content, time.Now().Add(2*time.Hour).UnixMilli(), threadID); err != nil {
		t.Fatalf("seed successor row: %v", err)
	}
}

func refsProjectionRows(t *testing.T, sqlDB *sql.DB, agent string) []string {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT id FROM thread_messages WHERE agent_id = ? ORDER BY created_at ASC, id ASC`, agent)
	if err != nil {
		t.Fatalf("list rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("walk rows: %v", err)
	}
	return ids
}

// The successor's model context composes summary, referenced rows in
// retained native order, and its own new rows — never by source
// timestamps — with the summary marked IsSummary.
func TestRefProjectionComposesSummaryRefsNewRows(t *testing.T) {
	turns, sqlDB := journalService(t)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "looking",
			backend.AgentThreadCall{ID: "call-a", Name: "web_search", Arguments: `{"q":"a"}`},
			backend.AgentThreadCall{ID: "call-b", Name: "web_search", Arguments: `{"q":"b"}`}),
		toolDraft(t, "t1", "call-b", "web_search", `{"q":"b"}`, "b hits", nil),
		toolDraft(t, "t1", "call-a", "web_search", `{"q":"a"}`, "a hits", nil),
		{Role: apitypes.ThreadRoleAssistant, Content: "done", TurnID: "t1"},
	})
	ids := refsProjectionRows(t, sqlDB, "research")
	_, successor := rotateViaRefs(t, sqlDB, "research", "the summary", ids)
	seedSuccessorRow(t, sqlDB, "research", successor, "research-new", "user", "continue")

	msgs, err := turns.GetAgentThread(context.Background(), "research")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 7 {
		t.Fatalf("projection = %d msgs, want 7: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != apitypes.ThreadRoleAssistant || msgs[0].Content != "the summary" || !msgs[0].IsSummary {
		t.Fatalf("msgs[0] = %+v, want the seeded summary marked IsSummary", msgs[0])
	}
	if msgs[1].Content != "search" || msgs[1].Role != apitypes.ThreadRoleUser {
		t.Fatalf("msgs[1] = %+v, want the referenced prompt", msgs[1])
	}
	if msgs[2].Content != "looking" || len(msgs[2].ToolCalls) != 2 {
		t.Fatalf("msgs[2] = %+v, want the referenced call group", msgs[2])
	}
	// Outcomes emit in the group's declared order (a then b), each
	// carrying its own stored row id.
	if msgs[3].ToolCallID != "call-a" || msgs[3].ID != "research-row-3" ||
		msgs[4].ToolCallID != "call-b" || msgs[4].ID != "research-row-2" {
		t.Fatalf("outcomes out of declared order: %+v %+v", msgs[3], msgs[4])
	}
	if msgs[5].Content != "done" {
		t.Fatalf("msgs[5] = %+v, want the referenced answer", msgs[5])
	}
	if msgs[6].Content != "continue" || msgs[6].IsSummary {
		t.Fatalf("msgs[6] = %+v, want the successor's own new row", msgs[6])
	}
}

// A carried source row has one display location: the successor's page
// shows it after the divider and summary; the source thread's page
// suppresses it. A structure-only referenced group row never consumes a
// display slot, exactly like a stored one.
func TestRefProjectionSuppressesCarriedRowsInSource(t *testing.T) {
	turns, sqlDB := journalService(t)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, "hits", nil),
	})
	ids := refsProjectionRows(t, sqlDB, "research")
	source, successor := rotateViaRefs(t, sqlDB, "research", "the summary", ids)
	q := db.New(sqlDB)

	snap, err := turns.SyncSnapshot(context.Background(), "research", 20)
	if err != nil {
		t.Fatalf("SyncSnapshot: %v", err)
	}
	got := make([]string, 0, len(snap.Page.Items))
	for _, it := range snap.Page.Items {
		got = append(got, string(it.Role)+"/"+it.Content)
	}
	// The structure-only group row (empty content) places no display
	// slot; the tool row shows its stored envelope.
	want := []string{"divider/Compacted", "assistant/the summary", "user/search"}
	if len(got) != len(want)+1 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] ||
		!strings.HasPrefix(got[3], "tool/") || !strings.Contains(got[3], "fetch") {
		t.Fatalf("successor page = %v, want %v plus the tool envelope", got, want)
	}
	if snap.Page.ThreadID != successor {
		t.Fatalf("page thread = %s, want successor %s", snap.Page.ThreadID, successor)
	}

	// The source thread no longer displays any carried row: the empty
	// structure-only group never showed, and both visible rows moved.
	srcPage, err := threadpage.Read(context.Background(), q, "research", source+":", 20)
	if err != nil {
		t.Fatalf("read source page: %v", err)
	}
	if len(srcPage.Items) != 0 {
		t.Fatalf("source page = %+v, want every carried row suppressed", srcPage.Items)
	}
}

// An anchor may name a referenced row; the keyset walk covers the whole
// union — divider, summary, references, new rows — and crosses into the
// predecessor when the successor is exhausted.
func TestRefProjectionAnchorAndKeysetWalk(t *testing.T) {
	_, sqlDB := journalService(t)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "looking", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, "hits", nil),
		{Role: apitypes.ThreadRoleAssistant, Content: "done", TurnID: "t1"},
	})
	ids := refsProjectionRows(t, sqlDB, "research")
	_, successor := rotateViaRefs(t, sqlDB, "research", "the summary", ids)
	seedSuccessorRow(t, sqlDB, "research", successor, "research-new", "user", "continue")
	q := db.New(sqlDB)
	ctx := context.Background()

	// The tool row is referenced, not stored — its anchor still places.
	anchored, err := threadpage.Read(ctx, q, "research", successor+":"+ids[2], 20)
	if err != nil {
		t.Fatalf("anchored read: %v", err)
	}
	var contents []string
	for _, it := range anchored.Items {
		contents = append(contents, it.Content)
	}
	if fmt.Sprint(contents) != "[Compacted the summary search looking]" {
		t.Fatalf("anchored page = %v, want the boundary plus rows strictly older than the tool outcome", contents)
	}

	// A limit-2 walk covers the whole union and ends at the boundary.
	var walk []string
	cursor := ""
	pages := 0
	for {
		page, err := threadpage.Read(ctx, q, "research", cursor, 2)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, it := range page.Items {
			walk = append(walk, string(it.Role)+"/"+firstWord(it.Content))
		}
		pages++
		if page.NextCursor == nil || pages > 8 {
			break
		}
		cursor = *page.NextCursor
	}
	want := []string{
		"assistant/done", "user/continue",
		"assistant/looking", "tool/",
		"divider/Compacted", "assistant/the summary", "user/search",
	}
	if fmt.Sprint(walk) != fmt.Sprint(want) {
		t.Fatalf("keyset walk = %v, want %v", walk, want)
	}
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, "{"); i >= 0 {
		return s[:i]
	}
	return s
}

// A repeated rotation references the physical source rows again — the
// middle thread's reference to a row the latest successor also carries
// is suppressed there, so each source id keeps one display location.
func TestRefProjectionSuppressesIntermediateCarriage(t *testing.T) {
	turns, sqlDB := journalService(t)
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("first prompt", "t1"),
	})
	ids := refsProjectionRows(t, sqlDB, "research")
	_, first := rotateViaRefs(t, sqlDB, "research", "first summary", ids)
	seedSuccessorRow(t, sqlDB, "research", first, "research-mid", "user", "second prompt")

	// Second rotation from the middle thread: re-references the
	// original prompt row (physical, never the middle thread's
	// reference) plus the middle thread's own row.
	second := "research-succ2"
	base := time.Now().Add(3 * time.Hour).UnixMilli()
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("begin second rotation: %v", err)
	}
	for _, s := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO threads (id, agent_id, previous_thread_id, kind, created_at)
		 VALUES (?, 'research', ?, 'compact', ?)`, []any{second, first, base}},
		{`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id, is_summary)
		 VALUES (?, 'research', 'assistant', 'second summary', ?, '', ?, 1)`, []any{second + "-summary", base + 1, second}},
		{`INSERT INTO thread_context_refs (agent_id, thread_id, position, message_id)
		 VALUES ('research', ?, 0, ?)`, []any{second, ids[0]}},
		{`INSERT INTO thread_context_refs (agent_id, thread_id, position, message_id)
		 VALUES ('research', ?, 1, 'research-mid')`, []any{second}},
		{`UPDATE agents SET active_thread_id = ? WHERE id = 'research'`, []any{second}},
	} {
		if _, err := tx.Exec(s.q, s.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("second rotation stmt: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit second rotation: %v", err)
	}

	q := db.New(sqlDB)
	// The latest successor shows both referenced rows once.
	snap, err := turns.SyncSnapshot(context.Background(), "research", 20)
	if err != nil {
		t.Fatalf("SyncSnapshot: %v", err)
	}
	var latest []string
	for _, it := range snap.Page.Items {
		latest = append(latest, it.Content)
	}
	if fmt.Sprint(latest) != "[Compacted second summary first prompt second prompt]" {
		t.Fatalf("latest successor page = %v", latest)
	}

	// The middle thread keeps only what the latest successor did not
	// carry: its summary, not its reference to the original prompt.
	mid, err := threadpage.Read(context.Background(), q, "research", first+":", 20)
	if err != nil {
		t.Fatalf("read middle page: %v", err)
	}
	var midContents []string
	for _, it := range mid.Items {
		midContents = append(midContents, it.Content)
	}
	if fmt.Sprint(midContents) != "[Compacted first summary]" {
		t.Fatalf("middle page = %v, want its summary only — the carried reference suppresses", midContents)
	}
}

// The placement the running process computed is the placement a fresh
// process reads back from storage: no gateway state involved.
func TestRefProjectionRestartReplaysSamePlacement(t *testing.T) {
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
	seedRows(t, sqlDB, "research", []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "looking", callDecl("c1", "fetch")),
		toolDraft(t, "t1", "c1", "fetch", `{}`, "hits", nil),
	})
	ids := refsProjectionRows(t, sqlDB, "research")
	_, successor := rotateViaRefs(t, sqlDB, "research", "the summary", ids)
	seedSuccessorRow(t, sqlDB, "research", successor, "research-new", "user", "continue")

	warm, err := turns.GetAgentThread(context.Background(), "research")
	if err != nil {
		t.Fatalf("warm GetAgentThread: %v", err)
	}

	_, _, cold, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("reopen backend: %v", err)
	}
	coldMsgs, err := cold.GetAgentThread(context.Background(), "research")
	if err != nil {
		t.Fatalf("cold GetAgentThread: %v", err)
	}
	if len(coldMsgs) != len(warm) {
		t.Fatalf("cold projection = %d msgs, warm = %d", len(coldMsgs), len(warm))
	}
	for i := range coldMsgs {
		if coldMsgs[i].Role != warm[i].Role || coldMsgs[i].Content != warm[i].Content ||
			coldMsgs[i].IsSummary != warm[i].IsSummary {
			t.Fatalf("cold msg %d = %+v, warm = %+v", i, coldMsgs[i], warm[i])
		}
	}
	snap, err := cold.SyncSnapshot(context.Background(), "research", 20)
	if err != nil {
		t.Fatalf("cold SyncSnapshot: %v", err)
	}
	if snap.Page.Items[0].Role != "divider" || snap.Page.Items[1].Content != "the summary" {
		t.Fatalf("cold page head = %+v, want divider then summary", snap.Page.Items[:2])
	}
}
