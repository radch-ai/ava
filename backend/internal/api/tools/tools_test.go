package tools_test

// GET/PATCH /tools handler tests over the real store: the catalog
// listing and the global-switch edit contract.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/api/tools"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func serve(t *testing.T, pattern string, h api.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	pattern = strings.TrimPrefix(pattern, method+" ")
	for i, seg := range strings.Split(pattern, "/") {
		if name, ok := strings.CutPrefix(seg, "{"); ok {
			if name, ok = strings.CutSuffix(name, "}"); ok {
				req.SetPathValue(name, strings.Split(path, "/")[i])
			}
		}
	}
	rec := httptest.NewRecorder()
	if err := h(rec, req); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return rec
}

// toolStore opens a fresh store with two catalog rows, the first
// globally disabled to prove the listing reflects saved state.
func toolStore(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	for i, id := range []string{"ddg", "notes"} {
		if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
			ID: id, Name: id + " name", Description: id + " does things",
		}); err != nil {
			t.Fatalf("seed tool %q: %v", id, err)
		}
		if i == 0 {
			if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
				Enabled: 0, ID: id,
			}); err != nil {
				t.Fatalf("disable %q: %v", id, err)
			}
		}
	}
	return sqlDB, queries
}

func TestGetToolsListsCatalog(t *testing.T) {
	_, queries := toolStore(t)

	rec := serve(t, "GET /tools", tools.GetTools(queries), http.MethodGet, "/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"tools":[{"id":"ddg","name":"ddg name","description":"ddg does things","enabled":false,"kind":"builtin"},{"id":"notes","name":"notes name","description":"notes does things","enabled":true,"kind":"builtin"}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestToggleToolFlipsGlobalSwitch(t *testing.T) {
	sqlDB, queries := toolStore(t)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/tools/ddg", `{"enabled":true,"kind":"builtin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"id":"ddg","name":"ddg name","description":"ddg does things","enabled":true,"kind":"builtin"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	row, err := queries.GetTool(t.Context(), "ddg")
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled != 1 {
		t.Errorf("saved enabled = %d, want 1", row.Enabled)
	}
}

// The switch and its approval-change record commit together: one row,
// decision approve/revoke, the static reason code, terminal state.
func TestToggleToolWritesAudit(t *testing.T) {
	sqlDB, queries := toolStore(t)

	for _, step := range []struct {
		body    string
		outcome string
		reason  string
		enabled int64
	}{
		{`{"enabled":true}`, "approve", "global_enabled", 1},
		{`{"enabled":false}`, "revoke", "global_disabled", 0},
	} {
		rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, nil),
			http.MethodPatch, "/tools/ddg", step.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("toggle %s: status = %d, want 200", step.body, rec.Code)
		}
		row, err := queries.GetTool(t.Context(), "ddg")
		if err != nil {
			t.Fatal(err)
		}
		if row.Enabled != step.enabled {
			t.Fatalf("toggle %s: enabled = %d, want %d", step.body, row.Enabled, step.enabled)
		}

		var (
			event, decision, reason, state string
			toolID, agentID                string
		)
		if err := sqlDB.QueryRowContext(t.Context(), `
SELECT event, decision, reason_code, state, COALESCE(tool_id,''), COALESCE(agent_id,'')
FROM tool_audit_events ORDER BY id DESC LIMIT 1`).
			Scan(&event, &decision, &reason, &state, &toolID, &agentID); err != nil {
			t.Fatalf("read audit: %v", err)
		}
		if event != "approval_change" || decision != step.outcome || reason != step.reason || state != "terminal" {
			t.Fatalf("audit row = %s/%s/%s/%s, want approval_change/%s/%s/terminal",
				event, decision, reason, state, step.outcome, step.reason)
		}
		if toolID != "ddg" || agentID != "" {
			t.Fatalf("audit row names tool %q agent %q, want the global toggle's tool only", toolID, agentID)
		}
	}
}

func TestToggleToolUnknownTool(t *testing.T) {
	sqlDB, queries := toolStore(t)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/tools/ghost", `{"enabled":true,"kind":"builtin"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	want := `{"message":"Tool \"ghost\" not found"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	// A rejected toggle writes no record: the switch never moved.
	var rows int
	if err := sqlDB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM tool_audit_events`).Scan(&rows); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if rows != 0 {
		t.Errorf("audit rows = %d, want none for a rejected toggle", rows)
	}
}

// false is a real value: an absent field is the only 400 here.
func TestToggleToolRequiresEnabled(t *testing.T) {
	sqlDB, queries := toolStore(t)

	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"yes"}`} {
		rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, nil),
			http.MethodPatch, "/tools/ddg", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); !strings.Contains(got, "message") {
			t.Errorf("body %s: response carries no message: %s", body, got)
		}
	}
	// The absent-field case names the missing requirement.
	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/tools/ddg", `{}`)
	want := `{"message":"enabled is required"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestToggleToolFalsePersists(t *testing.T) {
	sqlDB, queries := toolStore(t)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/tools/notes", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	row, err := queries.GetTool(t.Context(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled != 0 {
		t.Errorf("saved enabled = %d, want 0", row.Enabled)
	}
}

// A burst on one tool draws a 429; the next toggle passes once the
// window elapses.
func TestToggleToolThrottlesBursts(t *testing.T) {
	sqlDB, queries := toolStore(t)
	throttle := api.NewThrottle(50 * time.Millisecond)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/tools/ddg", `{"enabled":true,"kind":"builtin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("first toggle: status = %d, want 200", rec.Code)
	}
	rec = serve(t, "PATCH /tools/{id}", tools.ToggleTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/tools/ddg", `{"enabled":false}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("burst toggle: status = %d, want 429", rec.Code)
	}

	time.Sleep(60 * time.Millisecond)
	rec = serve(t, "PATCH /tools/{id}", tools.ToggleTool(throttle, sqlDB, queries, nil),
		http.MethodPatch, "/tools/ddg", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-window toggle: status = %d, want 200", rec.Code)
	}
}

// serveRaw mirrors serve but hands the handler error back: restriction
// paths answer 200 while joining a degraded-audit error for the log.
func serveRaw(t *testing.T, pattern string, h api.Handler, method, path, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	pattern = strings.TrimPrefix(pattern, method+" ")
	for i, seg := range strings.Split(pattern, "/") {
		if name, ok := strings.CutPrefix(seg, "{"); ok {
			if name, ok = strings.CutSuffix(name, "}"); ok {
				req.SetPathValue(name, strings.Split(path, "/")[i])
			}
		}
	}
	rec := httptest.NewRecorder()
	return rec, h(rec, req)
}

// countingCanceler counts the revocation the toggle asks for.
type countingCanceler struct {
	toolCalls []string
}

func (c *countingCanceler) CancelServerTools(string) int { return 0 }
func (c *countingCanceler) CancelToolCalls(_, toolID string) int {
	c.toolCalls = append(c.toolCalls, toolID)
	return 0
}
func (c *countingCanceler) CancelAgentServerTools(string, string) int       { return 0 }
func (c *countingCanceler) CancelAgentToolCalls(string, string, string) int { return 0 }

// failAudit makes every audit insert fail.
func failAudit(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install audit trigger: %v", err)
	}
}

// A failing audit never keeps a disabled tool live: the restriction
// commits alone, its calls are cancelled, and the record degrades to a
// logged error. An enable is an activation — its audit failure rolls the
// switch back.
func TestToggleToolDisableSurvivesAuditFailure(t *testing.T) {
	sqlDB, queries := toolStore(t)
	canceler := &countingCanceler{}
	failAudit(t, sqlDB)

	rec, err := serveRaw(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, canceler),
		http.MethodPatch, "/tools/notes", `{"enabled":false}`)
	if err == nil {
		t.Fatal("unauditable disable returned no degraded-audit error")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200", rec.Code, rec.Body.String())
	}
	row, err := queries.GetTool(t.Context(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled != 0 {
		t.Errorf("disabled tool enabled = %d, want 0 despite the failed record", row.Enabled)
	}
	if len(canceler.toolCalls) != 1 || canceler.toolCalls[0] != "notes" {
		t.Errorf("cancelled = %v, want the disabled tool", canceler.toolCalls)
	}

	rec, err = serveRaw(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, canceler),
		http.MethodPatch, "/tools/ddg", `{"enabled":true}`)
	if err == nil {
		t.Fatal("unauditable enable returned no error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, rec.Body.String())
	}
	row, err = queries.GetTool(t.Context(), "ddg")
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled != 0 {
		t.Errorf("enable rolled back to enabled = %d, want 0", row.Enabled)
	}
}

// A database fault reading the tool is a server error, not a missing
// tool: the 404 is reserved for an absent row.
func TestToggleToolReadFaultIs500(t *testing.T) {
	sqlDB, queries := toolStore(t)
	if _, err := sqlDB.Exec(`ALTER TABLE tools RENAME TO tools_away`); err != nil {
		t.Fatalf("break catalog: %v", err)
	}

	rec, err := serveRaw(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), sqlDB, queries, nil),
		http.MethodPatch, "/tools/ddg", `{"enabled":true}`)
	if err == nil {
		t.Fatal("read fault returned no error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, rec.Body.String())
	}
}
