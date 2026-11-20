package agent

// Thread endpoint tests over the real store: one thread per page, the
// keyset walk inside a thread, the divider at a thread boundary, the
// predecessor chain (load-more across /new), limit rules and the
// cursor contract. Route mounting is pinned in backend's tests.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// Distinct ms epochs for deterministic page order; ids index them
// oldest→newest (m1 oldest).
const pageBase = int64(1_700_000_000_000)

func threadStore(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	})
	return sqlDB, queries
}

// activateThread inserts a thread linked to the currently active one
// and points the agent at it — one /new, storage only.
func activateThread(t *testing.T, sqlDB *sql.DB, agentID, threadID string) {
	t.Helper()
	var previous string
	if err := sqlDB.QueryRow(
		`SELECT active_thread_id FROM agents WHERE id = ?`, agentID).Scan(&previous); err != nil {
		t.Fatalf("read active thread: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO threads (id, agent_id, previous_thread_id) VALUES (?, ?, ?)`,
		threadID, agentID, previous,
	); err != nil {
		t.Fatalf("seed thread %s: %v", threadID, err)
	}
	if _, err := sqlDB.Exec(
		`UPDATE agents SET active_thread_id = ? WHERE id = ?`, threadID, agentID,
	); err != nil {
		t.Fatalf("activate thread %s: %v", threadID, err)
	}
}

func seedMessages(t *testing.T, sqlDB *sql.DB, agentID string, ids ...string) {
	t.Helper()
	seedMessagesAt(t, sqlDB, agentID, testdb.ThreadID(agentID), pageBase, ids...)
}

// seedMessagesAt seeds one thread's user rows at fixed ms steps from
// base, so page order is deterministic within a thread.
func seedMessagesAt(t *testing.T, sqlDB *sql.DB, agentID, threadID string, base int64, ids ...string) {
	t.Helper()
	for i, id := range ids {
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
			 VALUES (?, ?, 'user', ?, ?, '', ?)`,
			id, agentID, id, base+int64(i)*1000, threadID,
		); err != nil {
			t.Fatalf("seed message %s: %v", id, err)
		}
	}
}

// seedTwoSessions seeds the canonical two-session fixture: the older
// thread's rows, one /new, then the active thread's rows. Tests vary
// only the row sets.
func seedTwoSessions(t *testing.T, sqlDB *sql.DB, agentID, olderID, activeID string, older, active []string) {
	t.Helper()
	seedMessagesAt(t, sqlDB, agentID, olderID, pageBase, older...)
	activateThread(t, sqlDB, agentID, activeID)
	seedMessagesAt(t, sqlDB, agentID, activeID, pageBase+10_000, active...)
}

func getThreadPage(t *testing.T, queries *db.Queries, path string) (int, apitypes.ThreadPage) {
	t.Helper()
	rec, err := serve(t, "GET /agent/{id}/thread", GetThread(queries), http.MethodGet, path, "")
	if err != nil {
		t.Fatalf("serve %s: %v", path, err)
	}
	var page apitypes.ThreadPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body, err)
	}
	return rec.Code, page
}

// pageIDs reports the ids a page answered, in order.
func pageIDs(page apitypes.ThreadPage) []string {
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestGetThreadEmpty(t *testing.T) {
	_, queries := threadStore(t)
	rec, err := serve(t, "GET /agent/{id}/thread", GetThread(queries),
		http.MethodGet, "/agent/research/thread", "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	// The empty page must serialize as [], not null — the UI's page
	// validator rejects a null items array.
	if body := rec.Body.String(); !strings.Contains(body, `"items":[]`) {
		t.Errorf("body = %s, want items to serialize as []", body)
	}
	var page apitypes.ThreadPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body, err)
	}
	if page.NextCursor != nil {
		t.Errorf("nextCursor = %q, want null", *page.NextCursor)
	}
	if page.ThreadID != "research-t1" {
		t.Errorf("threadId = %q, want research-t1", page.ThreadID)
	}
}

func TestGetThreadUnknownAgent(t *testing.T) {
	_, queries := threadStore(t)
	rec, err := serve(t, "GET /agent/{id}/thread", GetThread(queries),
		http.MethodGet, "/agent/ghost/thread", "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
	var body apitypes.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body, err)
	}
	if want := `Agent "ghost" not found`; body.Message != want {
		t.Errorf("message = %q, want %q", body.Message, want)
	}
}

func TestGetThreadPageOrder(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedMessages(t, sqlDB, "research", "m1", "m2", "m3")

	code, page := getThreadPage(t, queries, "/agent/research/thread")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if got := strings.Join(pageIDs(page), ","); got != "m1,m2,m3" {
		t.Fatalf("items = %s, want m1,m2,m3 (oldest first)", got)
	}
	if page.ThreadID != "research-t1" {
		t.Errorf("threadId = %s, want research-t1", page.ThreadID)
	}
	// pageBase ms = 2023-11-14T22:13:20.000Z; m1 carries it.
	if got := page.Items[0].CreatedAt; got != "2023-11-14T22:13:20.000Z" {
		t.Errorf("createdAt = %s, want the ms epoch as ISO", got)
	}
	if page.NextCursor != nil {
		t.Errorf("nextCursor = %v, want null on the initial thread's last page", *page.NextCursor)
	}
}

func TestGetThreadCarriesErrorRows(t *testing.T) {
	sqlDB, queries := threadStore(t)
	for i, seed := range []struct{ id, role, content string }{
		{"m1", "user", "prompt"},
		{"m2", "error", "no default model set"},
	} {
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
			 VALUES (?, 'research', ?, ?, ?, '', 'research-t1')`,
			seed.id, seed.role, seed.content, pageBase+int64(i)*1000,
		); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}

	code, page := getThreadPage(t, queries, "/agent/research/thread")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if got := page.Items[1]; got.ID != "m2" || got.Role != apitypes.ThreadRoleError ||
		got.Content != "no default model set" {
		t.Errorf("error row = %s/%s/%q, want m2/error/\"no default model set\"", got.ID, got.Role, got.Content)
	}
}

// A page never reaches into another thread: the no-cursor read starts
// at the active one, and older threads only appear through the
// predecessor cursor.
func TestGetThreadPageIsThreadScoped(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedTwoSessions(t, sqlDB, "research", "research-t1", "research-t2",
		[]string{"m1", "m2"}, []string{"m3"})

	code, page := getThreadPage(t, queries, "/agent/research/thread")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if got := strings.Join(pageIDs(page), ","); got != "research-t2,m3" {
		t.Fatalf("items = %s, want only the active thread's rows plus its divider", got)
	}
	if page.ThreadID != "research-t2" {
		t.Errorf("threadId = %s, want research-t2", page.ThreadID)
	}
}

// Exhausting a thread emits its divider and hands the reader the
// predecessor's newest page; the divider is keyed by the thread id.
func TestGetThreadDividerOnExhaustion(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedTwoSessions(t, sqlDB, "research", "research-t1", "research-t2",
		[]string{"m1", "m2"}, []string{"m3"})

	_, page := getThreadPage(t, queries, "/agent/research/thread")
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v, want the message plus its divider", page.Items)
	}
	// The divider marks where this thread starts: oldest end of the
	// page, above nothing else of this thread.
	first := page.Items[0]
	if first.Role != apitypes.ThreadRoleDivider || first.Content != "New session" ||
		first.ID != "research-t2" {
		t.Errorf("leading item = %s/%s/%q, want the research-t2 divider", first.ID, first.Role, first.Content)
	}
	if page.NextCursor == nil || *page.NextCursor != "research-t1:" {
		t.Fatalf("nextCursor = %v, want research-t1:", page.NextCursor)
	}

	// The predecessor's page carries the older history.
	_, older := getThreadPage(t, queries, "/agent/research/thread?cursor=research-t1:")
	if got := strings.Join(pageIDs(older), ","); got != "m1,m2" {
		t.Fatalf("predecessor page = %s, want m1,m2", got)
	}
	if older.ThreadID != "research-t1" {
		t.Errorf("threadId = %s, want research-t1", older.ThreadID)
	}
	if older.NextCursor != nil {
		t.Errorf("predecessor nextCursor = %v, want null (chain start)", *older.NextCursor)
	}
}

// A message-anchored page inside a thread carries no divider while
// older rows remain.
func TestGetThreadNoDividerMidThread(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedMessagesAt(t, sqlDB, "research", "research-t1", pageBase, "m1", "m2", "m3")
	activateThread(t, sqlDB, "research", "research-t2")
	seedMessagesAt(t, sqlDB, "research", "research-t2", pageBase+10_000, "m4", "m5")

	_, page := getThreadPage(t, queries, "/agent/research/thread?limit=1")
	if got := strings.Join(pageIDs(page), ","); got != "m5" {
		t.Fatalf("items = %s, want the newest row only", got)
	}
	if page.NextCursor == nil || *page.NextCursor != "research-t2:m5" {
		t.Fatalf("nextCursor = %v, want research-t2:m5", page.NextCursor)
	}

	// The continuation reaches the thread's oldest row, so the
	// boundary shows next: the divider leads, then m4.
	_, page = getThreadPage(t, queries, "/agent/research/thread?limit=1&cursor=research-t2:m5")
	if got := strings.Join(pageIDs(page), ","); got != "research-t2,m4" {
		t.Fatalf("continuation = %s, want the divider then m4", got)
	}
}

// Exactly limit visible rows exhaust the thread: the limit+1 probe row
// must not become a phantom page, and the divider still arrives.
func TestGetThreadExactLimitEmitsDivider(t *testing.T) {
	sqlDB, queries := threadStore(t)
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%02d", i+1)
	}
	seedMessagesAt(t, sqlDB, "research", "research-t1", pageBase, "old")
	activateThread(t, sqlDB, "research", "research-t2")
	seedMessagesAt(t, sqlDB, "research", "research-t2", pageBase+10_000, ids...)

	code, page := getThreadPage(t, queries, "/agent/research/thread?limit=20")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if len(page.Items) != 21 {
		t.Fatalf("items = %d, want 20 messages plus the divider", len(page.Items))
	}
	if page.Items[0].Role != apitypes.ThreadRoleDivider {
		t.Errorf("leading item = %+v, want the divider", page.Items[0])
	}
	if page.NextCursor == nil || *page.NextCursor != "research-t1:" {
		t.Fatalf("nextCursor = %v, want the predecessor cursor", page.NextCursor)
	}
}

// A thread with nothing in it still shows its boundary: /new with no
// prompt yet is one divider over the previous conversation.
func TestGetThreadEmptyActiveThread(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedTwoSessions(t, sqlDB, "research", "research-t1", "research-t2", []string{"m1"}, nil)

	code, page := getThreadPage(t, queries, "/agent/research/thread")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if len(page.Items) != 1 || page.Items[0].Role != apitypes.ThreadRoleDivider {
		t.Fatalf("items = %+v, want the single divider", page.Items)
	}
	if page.NextCursor == nil || *page.NextCursor != "research-t1:" {
		t.Fatalf("nextCursor = %v, want research-t1:", page.NextCursor)
	}
}

// Consecutive empty sessions stack dividers in display order, newest
// first, then reach the messages behind them.
func TestGetThreadConsecutiveEmptySessions(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedTwoSessions(t, sqlDB, "research", "research-t1", "research-t2", []string{"m1"}, nil)
	activateThread(t, sqlDB, "research", "research-t3")

	_, page := getThreadPage(t, queries, "/agent/research/thread")
	if len(page.Items) != 1 || page.Items[0].ID != "research-t3" {
		t.Fatalf("items = %+v, want the research-t3 divider", page.Items)
	}
	if page.NextCursor == nil || *page.NextCursor != "research-t2:" {
		t.Fatalf("nextCursor = %v, want research-t2:", page.NextCursor)
	}

	_, page = getThreadPage(t, queries, "/agent/research/thread?cursor=research-t2:")
	if len(page.Items) != 1 || page.Items[0].ID != "research-t2" {
		t.Fatalf("items = %+v, want the research-t2 divider", page.Items)
	}
	if page.NextCursor == nil || *page.NextCursor != "research-t1:" {
		t.Fatalf("nextCursor = %v, want research-t1:", page.NextCursor)
	}

	_, page = getThreadPage(t, queries, "/agent/research/thread?cursor=research-t1:")
	if got := strings.Join(pageIDs(page), ","); got != "m1" {
		t.Fatalf("items = %s, want m1", got)
	}
	if page.NextCursor != nil {
		t.Errorf("nextCursor = %v, want null at the chain start", *page.NextCursor)
	}
}

// Walking a deep thread crosses one divider and stops at the chain
// start: 45 older rows page 20/20/5 across the boundary.
func TestGetThreadPredecessorWalk(t *testing.T) {
	sqlDB, queries := threadStore(t)
	old := make([]string, 45)
	for i := range old {
		old[i] = fmt.Sprintf("o%02d", i+1)
	}
	seedMessagesAt(t, sqlDB, "research", "research-t1", pageBase, old...)
	activateThread(t, sqlDB, "research", "research-t2")
	seedMessagesAt(t, sqlDB, "research", "research-t2", pageBase+100_000, "n1")

	type wantPage struct {
		ids     []string
		divider bool
	}
	pages := []wantPage{
		{ids: []string{"n1"}, divider: true},
		{ids: old[25:45]},
		{ids: old[5:25]},
		{ids: old[0:5]},
	}
	cursor := ""
	for pageIdx, want := range pages {
		path := "/agent/research/thread?limit=20"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		code, page := getThreadPage(t, queries, path)
		if code != http.StatusOK {
			t.Fatalf("page %d code = %d, want 200", pageIdx, code)
		}
		ids := pageIDs(page)
		if want.divider {
			if len(ids) == 0 || ids[0] != "research-t2" {
				t.Fatalf("page %d = %v, want the leading divider", pageIdx, ids)
			}
			ids = ids[1:]
		}
		if strings.Join(ids, ",") != strings.Join(want.ids, ",") {
			t.Fatalf("page %d ids = %v, want %v", pageIdx, ids, want.ids)
		}
		last := pageIdx == len(pages)-1
		if last != (page.NextCursor == nil) {
			t.Fatalf("page %d nextCursor = %v, last = %v", pageIdx, page.NextCursor, last)
		}
		if !last {
			cursor = *page.NextCursor
		}
	}
}

// Equal timestamps keep the page boundary exact: the keyset carries
// the id as its tiebreak, so no row is skipped or repeated.
func TestGetThreadKeysetTiebreak(t *testing.T) {
	sqlDB, queries := threadStore(t)
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES ('a', 'research', 'user', 'a', ?, '', 'research-t1'),
		        ('b', 'research', 'user', 'b', ?, '', 'research-t1')`,
		pageBase, pageBase); err != nil {
		t.Fatalf("seed tied rows: %v", err)
	}

	// DESC order breaks the tie by id descending: b, then a.
	_, page := getThreadPage(t, queries, "/agent/research/thread?limit=1")
	if got := strings.Join(pageIDs(page), ","); got != "b" {
		t.Fatalf("first page = %s, want b", got)
	}
	if page.NextCursor == nil {
		t.Fatal("nextCursor = null, want the tiebreak cursor")
	}
	_, page = getThreadPage(t, queries, "/agent/research/thread?limit=1&cursor="+*page.NextCursor)
	if got := strings.Join(pageIDs(page), ","); got != "a" {
		t.Fatalf("second page = %s, want a", got)
	}
}

func TestGetThreadInvalidCursor(t *testing.T) {
	_, queries := threadStore(t)
	rec, err := serve(t, "GET /agent/{id}/thread", GetThread(queries),
		http.MethodGet, "/agent/research/thread?cursor=nope", "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	var body apitypes.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body, err)
	}
	if body.Message != "Invalid cursor" {
		t.Errorf("message = %q, want \"Invalid cursor\"", body.Message)
	}
}

// Every cursor that names something this agent cannot read is a 400:
// a foreign thread, a foreign agent's thread, a message of another
// thread, and a hidden structure row (never displayable, so never an
// anchor).
func TestGetThreadCursorValidation(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedMessages(t, sqlDB, "research", "m1")
	activateThread(t, sqlDB, "research", "research-t2")
	seedMessagesAt(t, sqlDB, "research", "research-t2", pageBase+10_000, "n1")
	seedStructureRow(t, sqlDB, "h1", pageBase+20_000)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "scribe", Name: "Scribe", Icon: "pencil",
		SoulPersonality: "terse", SoulIdentity: "records",
		OperatingInstructions: "transcribe", ToolsContext: "",
	})
	seedMessages(t, sqlDB, "scribe", "s1")

	for _, cursor := range []string{
		"nope",
		":",
		"research-t1:nomessage",
		"scribe-t1:",
		"scribe-t1:s1",
		"research-t2:h1",
	} {
		rec, err := serve(t, "GET /agent/{id}/thread", GetThread(queries),
			http.MethodGet, "/agent/research/thread?cursor="+cursor, "")
		if err != nil {
			t.Fatalf("serve cursor %q: %v", cursor, err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("cursor %q code = %d, want 400", cursor, rec.Code)
		}
	}
}

func TestGetThreadLimitRules(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedMessages(t, sqlDB, "research", "m1", "m2", "m3")

	// limit=0 floors at 1; limit=abc falls back to the default.
	for path, wantCount := range map[string]int{
		"/agent/research/thread?limit=0":   1,
		"/agent/research/thread?limit=abc": 3,
	} {
		code, page := getThreadPage(t, queries, path)
		if code != http.StatusOK {
			t.Fatalf("%s code = %d, want 200", path, code)
		}
		if len(page.Items) != wantCount {
			t.Errorf("%s items = %d, want %d", path, len(page.Items), wantCount)
		}
	}

	// limit=500 clamps at the cap of 100: 101 seeded rows page as 100
	// plus a continuation, not as one oversized page.
	deep := make([]string, 101)
	for i := range deep {
		deep[i] = fmt.Sprintf("d%03d", i+1)
	}
	seedMessagesAt(t, sqlDB, "research", "research-t1", pageBase+100_000, deep...)
	code, page := getThreadPage(t, queries, "/agent/research/thread?limit=500")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if len(page.Items) != 100 {
		t.Fatalf("items = %d, want the 100-row cap", len(page.Items))
	}
	if page.NextCursor == nil {
		t.Fatal("nextCursor = null, want a continuation beyond the cap")
	}
}

// seedStructureRow writes a structure-only assistant record:
// tool-call structure with no text, never rendered.
func seedStructureRow(t *testing.T, sqlDB *sql.DB, id string, createdAt int64) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, tool_calls, thread_id)
		 VALUES (?, 'research', 'assistant', '', ?, 't1', '[{"id":"c","name":"t","arguments":"{}"}]', 'research-t2')`,
		id, createdAt,
	); err != nil {
		t.Fatalf("seed structure row %s: %v", id, err)
	}
}

// A page of hidden structure rows must neither consume the limit nor
// conceal older visible history.
func TestGetThreadFiltersStructureRowsBeforeLimit(t *testing.T) {
	sqlDB, queries := threadStore(t)
	activateThread(t, sqlDB, "research", "research-t2")
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES ('m1', 'research', 'user', 'm1', ?, '', 'research-t2')`, pageBase); err != nil {
		t.Fatalf("seed m1: %v", err)
	}
	for i := range 25 {
		seedStructureRow(t, sqlDB, fmt.Sprintf("h%02d", i), pageBase+int64(i+1))
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES ('m2', 'research', 'user', 'm2', ?, '', 'research-t2')`, pageBase+100); err != nil {
		t.Fatalf("seed m2: %v", err)
	}

	code, page := getThreadPage(t, queries, "/agent/research/thread?limit=2")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if got := strings.Join(pageIDs(page), ","); got != "research-t2,m1,m2" {
		t.Fatalf("items = %s, want the two visible rows oldest first, divider first", got)
	}
	// Structure rows consumed no slot and hid nothing: the thread is
	// exhausted, so the walk continues into the predecessor.
	if page.NextCursor == nil || *page.NextCursor != "research-t1:" {
		t.Errorf("nextCursor = %v, want research-t1:", page.NextCursor)
	}
}
