package backend_test

// AppendThreadMessages pins the gateway's persistence sink: drafts
// land as thread rows with minted ids and strictly rising
// timestamps — within one call and across the two calls a turn
// makes.

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

func seedThreadAgent(t *testing.T, sqlDB *sql.DB, id string) {
	t.Helper()
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES (?, 'Thread', 'globe', '', '', '', '')`, id); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

func readThread(t *testing.T, sqlDB *sql.DB, agentID string) []db.ListThreadFirstPageRow {
	t.Helper()
	rows, err := db.New(sqlDB).ListThreadFirstPage(context.Background(),
		db.ListThreadFirstPageParams{AgentID: agentID, Limit: 10})
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	return rows
}

func TestAppendThreadMessagesMintsOrderedRows(t *testing.T) {
	sqlDB := resolverDB(t)
	seedThreadAgent(t, sqlDB, "a1")

	err := backend.AppendThreadMessages(context.Background(), "a1",
		[]apitypes.ThreadMessageDraft{
			{Role: apitypes.ThreadRoleUser, Content: "hello"},
			{Role: apitypes.ThreadRoleAssistant, Content: "hi there"},
		})
	if err != nil {
		t.Fatalf("AppendThreadMessages: %v", err)
	}

	rows := readThread(t, sqlDB, "a1")
	if len(rows) != 2 {
		t.Fatalf("thread rows = %d, want 2", len(rows))
	}
	// Newest first: the answer ranks above the prompt.
	answer, prompt := rows[0], rows[1]
	if answer.Role != "assistant" || answer.Content != "hi there" {
		t.Errorf("newest row = %s/%q, want assistant/\"hi there\"", answer.Role, answer.Content)
	}
	if prompt.Role != "user" || prompt.Content != "hello" {
		t.Errorf("older row = %s/%q, want user/\"hello\"", prompt.Role, prompt.Content)
	}
	if answer.CreatedAt <= prompt.CreatedAt {
		t.Errorf("answer created_at %d <= prompt created_at %d, want strictly after",
			answer.CreatedAt, prompt.CreatedAt)
	}
	if answer.ID == "" || prompt.ID == "" || answer.ID == prompt.ID {
		t.Errorf("minted ids = %q/%q, want distinct non-empty", prompt.ID, answer.ID)
	}
}

func TestAppendThreadMessagesToolDraftRoundTrip(t *testing.T) {
	sqlDB := resolverDB(t)
	seedThreadAgent(t, sqlDB, "a2")

	// One turn's rows: prompt, a tool invocation, the answer — the
	// envelope verbatim as content.
	envelope, err := json.Marshal(apitypes.ToolEnvelope{
		Name:   "web_search",
		Args:   json.RawMessage(`{"query":"ava superagent"}`),
		Result: json.RawMessage(`{"hits":2}`),
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	err = backend.AppendThreadMessages(context.Background(), "a2", []apitypes.ThreadMessageDraft{
		{Role: apitypes.ThreadRoleUser, Content: "search for ava"},
		{Role: apitypes.ThreadRoleTool, Content: string(envelope)},
		{Role: apitypes.ThreadRoleAssistant, Content: "found 2 hits"},
	})
	if err != nil {
		t.Fatalf("AppendThreadMessages: %v", err)
	}

	// Newest first, so the timeline reads user → tool → assistant
	// with timestamps strictly falling down the page.
	rows := readThread(t, sqlDB, "a2")
	if len(rows) != 3 {
		t.Fatalf("thread rows = %d, want 3", len(rows))
	}
	want := []struct{ role, content string }{
		{"assistant", "found 2 hits"},
		{"tool", string(envelope)},
		{"user", "search for ava"},
	}
	for i, w := range want {
		if rows[i].Role != w.role || rows[i].Content != w.content {
			t.Errorf("rows[%d] = %s/%q, want %s/%q", i, rows[i].Role, rows[i].Content, w.role, w.content)
		}
		if i > 0 && rows[i].CreatedAt >= rows[i-1].CreatedAt {
			t.Errorf("rows[%d].CreatedAt = %d, want strictly below rows[%d] = %d",
				i, rows[i].CreatedAt, i-1, rows[i-1].CreatedAt)
		}
	}
}

func TestAppendThreadMessagesTimestampsRiseAcrossCalls(t *testing.T) {
	sqlDB := resolverDB(t)
	seedThreadAgent(t, sqlDB, "a1")

	ctx := context.Background()
	// Two turns can complete within one millisecond; the sink owns
	// ordering, not luck.
	for _, drafts := range [][]apitypes.ThreadMessageDraft{
		{{Role: apitypes.ThreadRoleUser, Content: "first"}},
		{{Role: apitypes.ThreadRoleAssistant, Content: "answer"}},
	} {
		if err := backend.AppendThreadMessages(ctx, "a1", drafts); err != nil {
			t.Fatalf("AppendThreadMessages: %v", err)
		}
	}

	rows := readThread(t, sqlDB, "a1")
	if len(rows) != 2 {
		t.Fatalf("thread rows = %d, want 2", len(rows))
	}
	if rows[0].CreatedAt <= rows[1].CreatedAt {
		t.Errorf("second call created_at %d <= first call created_at %d, want strictly after",
			rows[0].CreatedAt, rows[1].CreatedAt)
	}
}

func TestAppendThreadMessagesRejectsUnknownAgent(t *testing.T) {
	resolverDB(t)
	err := backend.AppendThreadMessages(context.Background(), "ghost",
		[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "hello"}})
	if err == nil {
		t.Fatal("append for unknown agent succeeded, want the FK to reject it")
	}
}

func TestAppendThreadMessagesUsesSuppliedID(t *testing.T) {
	sqlDB := resolverDB(t)
	seedThreadAgent(t, sqlDB, "a3")

	// Terminal error rows name themselves on the wire; the sink must
	// store that exact id, not mint a fresh one.
	err := backend.AppendThreadMessages(context.Background(), "a3",
		[]apitypes.ThreadMessageDraft{
			{ID: "err-row-1", Role: apitypes.ThreadRoleError, Content: "no default model set"},
		})
	if err != nil {
		t.Fatalf("AppendThreadMessages: %v", err)
	}

	rows := readThread(t, sqlDB, "a3")
	if len(rows) != 1 {
		t.Fatalf("thread rows = %d, want 1", len(rows))
	}
	if rows[0].ID != "err-row-1" || rows[0].Role != "error" || rows[0].Content != "no default model set" {
		t.Errorf("row = %s/%s/%q, want err-row-1/error/\"no default model set\"",
			rows[0].ID, rows[0].Role, rows[0].Content)
	}
}
