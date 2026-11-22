package backend_test

// The journal's row writer: drafts land in the item's execution thread
// with minted ids and strictly rising timestamps — within one batch and
// across the commits a turn makes — and rows the replay projection would
// treat as corruption are rejected before any write.

import (
	"context"
	"database/sql"
	"encoding/json"
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

// startedItem admits and starts one prompt, returning the committed user
// row's id — the state every mid-turn write requires.
func startedItem(t *testing.T, svc *backend.TurnService, agentID, itemID, content string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.AcceptItem(ctx, agentID, itemID, content, "prompt", nil); err != nil {
		t.Fatalf("accept %s: %v", itemID, err)
	}
	start, _, err := svc.StartItem(ctx, agentID, itemID)
	if err != nil {
		t.Fatalf("start %s: %v", itemID, err)
	}
	return start.UserMessageID
}

func TestItemRowsLandInExecutionThread(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a1", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	userMsgID := startedItem(t, svc, "a1", "item-1", "hello")
	if _, err := svc.AppendItemMessages(ctx, "a1", "item-1",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "hi there"}}); err != nil {
		t.Fatalf("AppendItemMessages: %v", err)
	}

	rows := readThread(t, sqlDB, "a1")
	if len(rows) != 2 {
		t.Fatalf("thread rows = %d, want 2", len(rows))
	}
	// Oldest first: the prompt precedes the answer.
	prompt, answer := rows[0], rows[1]
	if prompt.ID != userMsgID || prompt.Role != "user" || prompt.Content != "hello" {
		t.Errorf("older row = %s/%s/%q, want the committed prompt", prompt.ID, prompt.Role, prompt.Content)
	}
	if answer.Role != "assistant" || answer.Content != "hi there" {
		t.Errorf("newest row = %s/%q, want assistant/\"hi there\"", answer.Role, answer.Content)
	}
	if answer.CreatedAt <= prompt.CreatedAt {
		t.Errorf("answer created_at %d <= prompt created_at %d, want strictly after",
			answer.CreatedAt, prompt.CreatedAt)
	}
	if answer.ID == "" || answer.ID == prompt.ID {
		t.Errorf("minted ids = %q/%q, want distinct non-empty", prompt.ID, answer.ID)
	}
}

// A tool outcome commits as its envelope row — the model-facing result
// verbatim, the invocation's call id pairing it to its assistant group —
// and the projection reads it back beside the call group.
func TestToolOutcomeRowRoundTrip(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a2", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	startedItem(t, svc, "a2", "item-2", "search for ava")
	if _, err := svc.AppendItemMessages(ctx, "a2", "item-2", []backend.ThreadMessageDraft{{
		Role:      apitypes.ThreadRoleAssistant,
		TurnID:    "item-2",
		ToolCalls: []byte(`[{"id":"call-1","name":"web_search","arguments":"{\"query\":\"ava superagent\"}"}]`),
	}}); err != nil {
		t.Fatalf("append call group: %v", err)
	}
	if _, err := svc.RecordToolStarted(ctx, "a2", "item-2", "inv-1", "call-1", "web_search",
		`{"query":"ava superagent"}`); err != nil {
		t.Fatalf("record tool start: %v", err)
	}
	if _, err := svc.RecordToolFinished(ctx, "a2", "item-2", "inv-1", `{"hits":2}`, nil); err != nil {
		t.Fatalf("record tool finish: %v", err)
	}
	if _, err := svc.FinishItem(ctx, "a2", "item-2", apitypes.ItemCompleted, "",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "found 2 hits", TurnID: "item-2"}}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	// Oldest first: user, call group, tool, answer, with strictly rising
	// timestamps.
	rows := readThread(t, sqlDB, "a2")
	want := []string{"user", "assistant", "tool", "assistant"}
	if len(rows) != len(want) {
		t.Fatalf("thread rows = %d, want %d", len(rows), len(want))
	}
	for i, role := range want {
		if rows[i].Role != role {
			t.Errorf("rows[%d].Role = %s, want %s", i, rows[i].Role, role)
		}
		if i > 0 && rows[i].CreatedAt <= rows[i-1].CreatedAt {
			t.Errorf("rows[%d].CreatedAt = %d, want strictly above rows[%d] = %d",
				i, rows[i].CreatedAt, i-1, rows[i-1].CreatedAt)
		}
	}
	var envelope backend.ToolEnvelope
	if err := json.Unmarshal([]byte(rows[2].Content), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Name != "web_search" || envelope.CallID != "call-1" || envelope.Result != `{"hits":2}` {
		t.Errorf("envelope = %+v, want the recorded invocation", envelope)
	}
	// The model-facing replay pairs the outcome with its call.
	msgs, err := svc.GetAgentThread(ctx, "a2")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	toolSeen := false
	for _, m := range msgs {
		if m.Role == apitypes.ThreadRoleTool {
			toolSeen = true
			if m.ToolCallID != "call-1" || m.Content != `{"hits":2}` {
				t.Errorf("projected tool message = %+v, want the paired outcome", m)
			}
		}
	}
	if !toolSeen {
		t.Fatal("projection lost the tool outcome")
	}
}

func TestRowTimestampsRiseAcrossCommits(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a1", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	// A turn can complete within one millisecond; the writer owns
	// ordering, not luck.
	startedItem(t, svc, "a1", "item-1", "first")
	if _, err := svc.FinishItem(ctx, "a1", "item-1", apitypes.ItemCompleted, "",
		[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: "answer", TurnID: "item-1"}}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	rows := readThread(t, sqlDB, "a1")
	if len(rows) != 2 {
		t.Fatalf("thread rows = %d, want 2", len(rows))
	}
	if rows[1].CreatedAt <= rows[0].CreatedAt {
		t.Errorf("second commit created_at %d <= first commit %d, want strictly after",
			rows[1].CreatedAt, rows[0].CreatedAt)
	}
}

// Admission is the gate: an unknown agent has no item to accept, so no
// row or event can ever name one.
func TestAcceptItemRejectsUnknownAgent(t *testing.T) {
	_, svc := resolverDB(t)
	_, err := svc.AcceptItem(context.Background(), "ghost", "item-1", "hello", "prompt", nil)
	if err == nil {
		t.Fatal("accept for an unknown agent succeeded, want a foreign key rejection")
	}
}

// Terminal rows name themselves: a failure's stored row id is the one the
// event carries, so live and reloaded state converge.
func TestFinishItemUsesSuppliedID(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "a3", Name: "Thread", Icon: "globe"})
	ctx := context.Background()

	startedItem(t, svc, "a3", "item-3", "hello")
	batch, err := svc.FinishItem(ctx, "a3", "item-3", apitypes.ItemFailed, "",
		[]backend.ThreadMessageDraft{{
			ID: "err-row-1", Role: apitypes.ThreadRoleError, Content: "no default model set",
		}})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if rows := batch.Events[0].Rows; len(rows) != 1 || rows[0].ID != "err-row-1" {
		t.Fatalf("event rows = %+v, want the supplied id", rows)
	}

	var id, role, content string
	if err := sqlDB.QueryRow(
		`SELECT id, role, content FROM thread_messages WHERE agent_id = 'a3' AND role = 'error'`).
		Scan(&id, &role, &content); err != nil {
		t.Fatalf("read error row: %v", err)
	}
	if id != "err-row-1" || role != "error" || content != "no default model set" {
		t.Errorf("row = %s/%s/%q, want err-row-1/error/\"no default model set\"", id, role, content)
	}
}

// One batch is one transaction: a failing row leaves no earlier row
// visible, and the next read returns only what committed.
func TestItemMessageBatchIsAtomic(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "atomic", Name: "Thread", Icon: "globe"})
	ctx := context.Background()
	startedItem(t, svc, "atomic", "item-1", "prompt")

	// A pre-existing id makes the batch's second row fail.
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES ('taken', 'atomic', 'error', 'existing', 1, '', ?)`, testdb.ThreadID("atomic")); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	_, err := svc.AppendItemMessages(ctx, "atomic", "item-1",
		[]backend.ThreadMessageDraft{
			{Role: apitypes.ThreadRoleAssistant, Content: "first"},
			{ID: "taken", Role: apitypes.ThreadRoleAssistant, Content: "boom"},
			{Role: apitypes.ThreadRoleAssistant, Content: "third"},
		})
	if err == nil {
		t.Fatal("batch with a duplicate id succeeded, want a failure")
	}

	rows := readThread(t, sqlDB, "atomic")
	// The committed prompt plus the pre-existing duplicate row, nothing
	// from the failed batch.
	if len(rows) != 2 {
		t.Fatalf("rows after failed batch = %+v, want the prompt and the seeded row", rows)
	}
	for _, row := range rows {
		if row.Content == "first" || row.Content == "boom" || row.Content == "third" {
			t.Fatalf("failed batch left a row: %+v", row)
		}
	}
}

func TestItemMessagesRejectCorruptDrafts(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "guard", Name: "Thread", Icon: "globe"})
	ctx := context.Background()
	startedItem(t, svc, "guard", "item-1", "prompt")

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
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleTool, Content: "{nope"}},
		},
		{
			name:   "tool row without call id",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleTool, Content: string(noCall)}},
		},
		{
			name: "tool row with non-string result",
			drafts: []backend.ThreadMessageDraft{
				{Role: apitypes.ThreadRoleTool, Content: `{"name":"x","callId":"c1","result":{"a":1}}`},
			},
		},
		{
			name:   "group with a nameless call",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, ToolCalls: badGroup}},
		},
		{
			name:   "group with repeated call ids",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, ToolCalls: dupGroup}},
		},
		{
			name: "batch repeating a supplied row id",
			drafts: []backend.ThreadMessageDraft{
				{ID: "dup", Role: apitypes.ThreadRoleAssistant, Content: "one"},
				{ID: "dup", Role: apitypes.ThreadRoleAssistant, Content: "two"},
			},
		},
		{
			name:   "tool calls on a user row",
			drafts: []backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, ToolCalls: []byte("[]")}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.AppendItemMessages(ctx, "guard", "item-1", tc.drafts); err == nil {
				t.Fatal("AppendItemMessages succeeded, want a structure rejection")
			}
			// Only the committed prompt stands.
			if rows := readThread(t, sqlDB, "guard"); len(rows) != 1 {
				t.Fatalf("rows after rejected draft = %+v, want the prompt only", rows)
			}
		})
	}
}

func TestWatermarkSeededFromStore(t *testing.T) {
	sqlDB, _ := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "future", Name: "Thread", Icon: "globe"})

	// A row minted an hour ahead — a restart into a corrected clock
	// must never write behind it.
	ahead := time.Now().Add(time.Hour).UnixMilli()
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, thread_id)
		 VALUES ('ahead', 'future', 'user', 'from the future', ?, 't0', ?)`, ahead, testdb.ThreadID("future")); err != nil {
		t.Fatalf("seed future row: %v", err)
	}

	// A fresh service over the same store — the boot-time seed.
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits())
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	startedItem(t, svc, "future", "item-1", "after all")

	rows := readThread(t, sqlDB, "future")
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].ID != "ahead" || rows[1].CreatedAt <= rows[0].CreatedAt {
		t.Fatalf("order = %+v, want the future row first and the write strictly after", rows)
	}
}
