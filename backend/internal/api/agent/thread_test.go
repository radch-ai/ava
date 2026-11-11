package agent

// Thread endpoint tests over the real store: page order, the cursor
// walk, limit rules, and the error contract. Route mounting is pinned
// in backend's tests.

import (
	"database/sql"
	"encoding/json"
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
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return sqlDB, queries
}

func seedMessages(t *testing.T, sqlDB *sql.DB, agentID string, ids ...string) {
	t.Helper()
	for i, id := range ids {
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, created_at)
			 VALUES (?, ?, 'user', ?, ?)`,
			id, agentID, id, pageBase+int64(i)*1000,
		); err != nil {
			t.Fatalf("seed message %s: %v", id, err)
		}
	}
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
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[],"nextCursor":null}` {
		t.Errorf("body = %s, want the empty page envelope", got)
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
	want := `{"message":"Agent \"ghost\" not found"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestGetThreadNewestFirst(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedMessages(t, sqlDB, "research", "m1", "m2", "m3")

	code, page := getThreadPage(t, queries, "/agent/research/thread")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(page.Items))
	}
	for i, want := range []string{"m3", "m2", "m1"} {
		if page.Items[i].ID != want {
			t.Errorf("items[%d] = %s, want %s (newest first)", i, page.Items[i].ID, want)
		}
	}
	// 1_700_000_000_000 ms = 2023-11-14T22:13:20.000Z.
	if got := page.Items[2].CreatedAt; got != "2023-11-14T22:13:20.000Z" {
		t.Errorf("createdAt = %s, want the ms epoch as ISO", got)
	}
	if page.NextCursor != nil {
		t.Errorf("nextCursor = %v, want null on the last page", *page.NextCursor)
	}
}

func TestGetThreadCursorWalk(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedMessages(t, sqlDB, "research", "m1", "m2", "m3", "m4", "m5")

	wantPages := [][]string{
		{"m5", "m4"},
		{"m3", "m2"},
		{"m1"},
	}
	cursor := ""
	for pageIdx, wantIDs := range wantPages {
		path := "/agent/research/thread?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		code, page := getThreadPage(t, queries, path)
		if code != http.StatusOK {
			t.Fatalf("page %d code = %d, want 200", pageIdx, code)
		}
		if len(page.Items) != len(wantIDs) {
			t.Fatalf("page %d items = %d, want %d", pageIdx, len(page.Items), len(wantIDs))
		}
		for i, want := range wantIDs {
			if page.Items[i].ID != want {
				t.Errorf("page %d items[%d] = %s, want %s", pageIdx, i, page.Items[i].ID, want)
			}
		}
		last := pageIdx == len(wantPages)-1
		if last && page.NextCursor != nil {
			t.Errorf("last page nextCursor = %v, want null", *page.NextCursor)
		}
		if !last {
			if page.NextCursor == nil {
				t.Fatalf("page %d nextCursor = null, want %s", pageIdx, wantIDs[len(wantIDs)-1])
			}
			cursor = *page.NextCursor
		}
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
	if got := strings.TrimSpace(rec.Body.String()); got != `{"message":"Invalid cursor"}` {
		t.Errorf("body = %s, want invalid cursor message", got)
	}
}

func TestGetThreadCrossAgentCursor(t *testing.T) {
	sqlDB, queries := threadStore(t)
	seedMessages(t, sqlDB, "research", "m1", "m2")
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "scribe", Name: "Scribe", Icon: "pencil",
		SoulPersonality: "terse", SoulIdentity: "records",
		OperatingInstructions: "transcribe", ToolsContext: "",
	}); err != nil {
		t.Fatalf("seed scribe: %v", err)
	}
	seedMessages(t, sqlDB, "scribe", "s1")

	rec, err := serve(t, "GET /agent/{id}/thread", GetThread(queries),
		http.MethodGet, "/agent/research/thread?cursor=s1", "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 (cross-agent cursor)", rec.Code)
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
}
