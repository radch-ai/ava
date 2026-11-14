package backend_test

// AppendThreadMessages pins the gateway's persistence sink: drafts
// land as thread rows with minted ids and strictly rising
// timestamps — within one call and across the two calls a turn
// makes.

import (
	"context"
	"database/sql"
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
