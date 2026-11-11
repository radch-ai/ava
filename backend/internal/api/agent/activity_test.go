package agent

// Activity endpoint tests over the real store: page order, message
// presence, the cursor walk, and the error contract. Route mounting
// is pinned in backend's tests.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

func activityStore(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()
	sqlDB, queries := threadStore(t)
	return sqlDB, queries
}

// seedActivity inserts ids oldest→newest; every row carries message
// except the ids listed in bare.
func seedActivity(t *testing.T, sqlDB *sql.DB, agentID string, bare map[string]bool, ids ...string) {
	t.Helper()
	for i, id := range ids {
		var message any
		if !bare[id] {
			message = "summary " + id
		}
		if _, err := sqlDB.Exec(
			`INSERT INTO agent_activity (id, agent_id, triggered_by, action, status, message, created_at)
			 VALUES (?, ?, 'cron', 'cron.run', 'completed', ?, ?)`,
			id, agentID, message, pageBase+int64(i)*1000,
		); err != nil {
			t.Fatalf("seed activity %s: %v", id, err)
		}
	}
}

func getActivityPage(t *testing.T, queries *db.Queries, path string) (int, apitypes.ActivityPage) {
	t.Helper()
	rec, err := serve(t, "GET /agent/{id}/activity", GetAgentActivity(queries), http.MethodGet, path, "")
	if err != nil {
		t.Fatalf("serve %s: %v", path, err)
	}
	var page apitypes.ActivityPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body, err)
	}
	return rec.Code, page
}

func TestGetActivityEmpty(t *testing.T) {
	_, queries := activityStore(t)
	rec, err := serve(t, "GET /agent/{id}/activity", GetAgentActivity(queries),
		http.MethodGet, "/agent/research/activity", "")
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

func TestGetActivityUnknownAgent(t *testing.T) {
	_, queries := activityStore(t)
	rec, err := serve(t, "GET /agent/{id}/activity", GetAgentActivity(queries),
		http.MethodGet, "/agent/ghost/activity", "")
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

func TestGetActivityNewestFirstAndMessagePresence(t *testing.T) {
	sqlDB, queries := activityStore(t)
	// a2 is bare: NULL message, omitted on the wire.
	seedActivity(t, sqlDB, "research", map[string]bool{"a2": true}, "a1", "a2", "a3")

	code, page := getActivityPage(t, queries, "/agent/research/activity")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(page.Items))
	}
	for i, want := range []string{"a3", "a2", "a1"} {
		if page.Items[i].ID != want {
			t.Errorf("items[%d] = %s, want %s (newest first)", i, page.Items[i].ID, want)
		}
	}
	if got := page.Items[0].Message; got == nil || *got != "summary a3" {
		t.Errorf("a3 message = %v, want the response summary", got)
	}
	if got := page.Items[1].Message; got != nil {
		t.Errorf("a2 message = %v, want nil (NULL omitted)", *got)
	}
}

func TestGetActivityCursorWalk(t *testing.T) {
	sqlDB, queries := activityStore(t)
	seedActivity(t, sqlDB, "research", nil, "a1", "a2", "a3", "a4", "a5")

	wantPages := [][]string{
		{"a5", "a4"},
		{"a3", "a2"},
		{"a1"},
	}
	cursor := ""
	for pageIdx, wantIDs := range wantPages {
		path := "/agent/research/activity?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		code, page := getActivityPage(t, queries, path)
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

func TestGetActivityInvalidCursor(t *testing.T) {
	_, queries := activityStore(t)
	rec, err := serve(t, "GET /agent/{id}/activity", GetAgentActivity(queries),
		http.MethodGet, "/agent/research/activity?cursor=nope", "")
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

func TestGetActivityCrossAgentCursor(t *testing.T) {
	sqlDB, queries := activityStore(t)
	seedActivity(t, sqlDB, "research", nil, "a1", "a2")
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "scribe", Name: "Scribe", Icon: "pencil",
		SoulPersonality: "terse", SoulIdentity: "records",
		OperatingInstructions: "transcribe", ToolsContext: "",
	}); err != nil {
		t.Fatalf("seed scribe: %v", err)
	}
	seedActivity(t, sqlDB, "scribe", nil, "s1")

	rec, err := serve(t, "GET /agent/{id}/activity", GetAgentActivity(queries),
		http.MethodGet, "/agent/research/activity?cursor=s1", "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 (cross-agent cursor)", rec.Code)
	}
}
