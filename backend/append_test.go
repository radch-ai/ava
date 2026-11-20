package backend_test

// AppendThreadMessages pins the gateway's persistence sink: drafts
// land as thread rows with minted ids and strictly rising
// timestamps — within one call and across the two calls a turn
// makes.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func readThread(t *testing.T, sqlDB *sql.DB, agentID string) []db.ListActiveThreadRowsRow {
	t.Helper()
	rows, err := db.New(sqlDB).ListActiveThreadRows(context.Background(), db.ListActiveThreadRowsParams{
		AgentID:  agentID,
		ThreadID: testdb.ThreadID(agentID),
	})
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	return rows
}

func TestAppendThreadMessagesMintsOrderedRows(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a1", Name: "Thread", Icon: "globe"})

	err := svc.AppendThreadMessages(context.Background(), "a1",
		[]backend.ThreadMessageDraft{
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
	// Oldest first: the prompt precedes the answer.
	prompt, answer := rows[0], rows[1]
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
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a2", Name: "Thread", Icon: "globe"})

	// One turn's rows: prompt, a tool invocation, the answer — the
	// envelope verbatim as content.
	envelope, err := json.Marshal(backend.ToolEnvelope{
		Name:   "web_search",
		CallID: "call-1",
		Args:   json.RawMessage(`{"query":"ava superagent"}`),
		Result: `{"hits":2}`,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	err = svc.AppendThreadMessages(context.Background(), "a2", []backend.ThreadMessageDraft{
		{Role: apitypes.ThreadRoleUser, Content: "search for ava"},
		{Role: apitypes.ThreadRoleTool, Content: string(envelope)},
		{Role: apitypes.ThreadRoleAssistant, Content: "found 2 hits"},
	})
	if err != nil {
		t.Fatalf("AppendThreadMessages: %v", err)
	}

	// Oldest first: user, tool, assistant, with strictly rising
	// timestamps.
	rows := readThread(t, sqlDB, "a2")
	if len(rows) != 3 {
		t.Fatalf("thread rows = %d, want 3", len(rows))
	}
	want := []struct{ role, content string }{
		{"user", "search for ava"},
		{"tool", string(envelope)},
		{"assistant", "found 2 hits"},
	}
	for i, w := range want {
		if rows[i].Role != w.role || rows[i].Content != w.content {
			t.Errorf("rows[%d] = %s/%q, want %s/%q", i, rows[i].Role, rows[i].Content, w.role, w.content)
		}
		if i > 0 && rows[i].CreatedAt <= rows[i-1].CreatedAt {
			t.Errorf("rows[%d].CreatedAt = %d, want strictly above rows[%d] = %d",
				i, rows[i].CreatedAt, i-1, rows[i-1].CreatedAt)
		}
	}
}

func TestAppendThreadMessagesTimestampsRiseAcrossCalls(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a1", Name: "Thread", Icon: "globe"})

	ctx := context.Background()
	// Two turns can complete within one millisecond; the sink owns
	// ordering, not luck.
	for _, drafts := range [][]backend.ThreadMessageDraft{
		{{Role: apitypes.ThreadRoleUser, Content: "first"}},
		{{Role: apitypes.ThreadRoleAssistant, Content: "answer"}},
	} {
		if err := svc.AppendThreadMessages(ctx, "a1", drafts); err != nil {
			t.Fatalf("AppendThreadMessages: %v", err)
		}
	}

	rows := readThread(t, sqlDB, "a1")
	if len(rows) != 2 {
		t.Fatalf("thread rows = %d, want 2", len(rows))
	}
	if rows[1].CreatedAt <= rows[0].CreatedAt {
		t.Errorf("second call created_at %d <= first call created_at %d, want strictly after",
			rows[1].CreatedAt, rows[0].CreatedAt)
	}
}

func TestAppendThreadMessagesRejectsUnknownAgent(t *testing.T) {
	_, svc := resolverDB(t)
	err := svc.AppendThreadMessages(context.Background(), "ghost",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: "hello"}})
	if !errors.Is(err, backend.ErrUnknownAgent) {
		t.Fatalf("append error = %v, want ErrUnknownAgent", err)
	}
}

func TestAppendThreadMessagesUsesSuppliedID(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a3", Name: "Thread", Icon: "globe"})

	// Terminal error rows name themselves on the wire; the sink must
	// store that exact id, not mint a fresh one.
	err := svc.AppendThreadMessages(context.Background(), "a3",
		[]backend.ThreadMessageDraft{
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

// One batch is one transaction: a failing row leaves no earlier row
// visible, and the next read returns only what committed.
func TestAppendThreadMessagesBatchIsAtomic(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "atomic", Name: "Thread", Icon: "globe"})

	// A pre-existing id makes the batch's second row fail.
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES ('taken', 'atomic', 'error', 'existing', 1, '', ?)`, testdb.ThreadID("atomic")); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	err := svc.AppendThreadMessages(context.Background(), "atomic",
		[]backend.ThreadMessageDraft{
			{Role: apitypes.ThreadRoleUser, Content: "first", TurnID: "t1"},
			{ID: "taken", Role: apitypes.ThreadRoleAssistant, Content: "boom", TurnID: "t1"},
			{Role: apitypes.ThreadRoleUser, Content: "third", TurnID: "t1"},
		})
	if err == nil {
		t.Fatal("batch with a duplicate id succeeded, want a commit failure")
	}

	rows := readThread(t, sqlDB, "atomic")
	if len(rows) != 1 || rows[0].ID != "taken" {
		t.Fatalf("rows after failed batch = %+v, want only the pre-existing row", rows)
	}
	msgs, err := svc.GetAgentThread(context.Background(), "atomic")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("thread after failed batch = %+v, want empty", msgs)
	}
}

func TestAppendThreadMessagesRejectsCorruptDrafts(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "guard", Name: "Thread", Icon: "globe"})

	// A call group with a nameless call and one with a repeated id —
	// both fail the read path's structure contract at write time.
	badGroup, _ := json.Marshal([]backend.AgentThreadCall{
		{ID: "c1", Name: "", Arguments: `{}`},
	})
	dupGroup, _ := json.Marshal([]backend.AgentThreadCall{
		{ID: "c1", Name: "a", Arguments: `{}`},
		{ID: "c1", Name: "b", Arguments: `{}`},
	})
	noCall, _ := json.Marshal(backend.ToolEnvelope{Name: "x", Result: "r"})

	cases := []struct {
		name   string
		drafts []backend.ThreadMessageDraft
	}{
		{
			name:   "tool row with undecodable content",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleTool, Content: "{nope", TurnID: "t1"}},
		},
		{
			name:   "tool row without call id",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleTool, Content: string(noCall), TurnID: "t1"}},
		},
		{
			name: "tool row with non-string result",
			drafts: []backend.ThreadMessageDraft{
				{Role: apitypes.ThreadRoleTool, Content: `{"name":"x","callId":"c1","result":{"a":1}}`, TurnID: "t1"},
			},
		},
		{
			name:   "group with a nameless call",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, ToolCalls: badGroup, TurnID: "t1"}},
		},
		{
			name:   "group with repeated call ids",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, ToolCalls: dupGroup, TurnID: "t1"}},
		},
		{
			name: "batch repeating a supplied row id",
			drafts: []backend.ThreadMessageDraft{
				{ID: "dup", Role: apitypes.ThreadRoleUser, Content: "one", TurnID: "t1"},
				{ID: "dup", Role: apitypes.ThreadRoleAssistant, Content: "two", TurnID: "t1"},
			},
		},
		{
			name:   "tool calls on a user row",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, ToolCalls: []byte("[]"), TurnID: "t1"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.AppendThreadMessages(context.Background(), "guard", tc.drafts); err == nil {
				t.Fatal("AppendThreadMessages succeeded, want a structure rejection")
			}
			if rows := readThread(t, sqlDB, "guard"); len(rows) != 0 {
				t.Fatalf("rows after rejected draft = %+v, want none", rows)
			}
		})
	}
}

func TestAppendThreadMessagesWatermarkSeededFromStore(t *testing.T) {
	sqlDB, _ := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "future", Name: "Thread", Icon: "globe"})

	// A row minted an hour ahead — a restart into a corrected clock
	// must never append behind it.
	ahead := time.Now().Add(time.Hour).UnixMilli()
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES ('ahead', 'future', 'user', 'from the future', ?, 't0', ?)`, ahead, testdb.ThreadID("future")); err != nil {
		t.Fatalf("seed future row: %v", err)
	}

	// A fresh service over the same store — the boot-time seed.
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	if err := svc.AppendThreadMessages(context.Background(), "future",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "after all", TurnID: "t1"}}); err != nil {
		t.Fatalf("AppendThreadMessages: %v", err)
	}

	rows := readThread(t, sqlDB, "future")
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].ID != "ahead" || rows[1].CreatedAt <= rows[0].CreatedAt {
		t.Fatalf("order = %+v, want the future row first and the append strictly after", rows)
	}
}
