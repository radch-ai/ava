package backend_test

// GetAgentThread pins the thread projection over durable rows: the
// complete conversation reads back oldest-first with native assistant
// call groups preserved (text, ids, names, verbatim arguments,
// declared order), outcomes paired to their group, error rows
// excluded, incomplete groups projected to their actual outcomes, and
// corrupt structure rejected as an error — never a silent unpairing.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func groupDraft(t *testing.T, turnID, content string, calls ...backend.AgentThreadCall) backend.ThreadMessageDraft {
	t.Helper()
	raw, err := json.Marshal(calls)
	if err != nil {
		t.Fatalf("marshal calls: %v", err)
	}
	return backend.ThreadMessageDraft{
		Role: apitypes.ThreadRoleAssistant, Content: content,
		TurnID: turnID, ToolCalls: raw,
	}
}

func toolDraft(t *testing.T, turnID, callID, name, args, result string, errText *string) backend.ThreadMessageDraft {
	t.Helper()
	env, err := json.Marshal(backend.ToolEnvelope{
		Name: name, CallID: callID, Args: json.RawMessage(args),
		Result: result, Error: errText,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return backend.ThreadMessageDraft{
		Role: apitypes.ThreadRoleTool, Content: string(env),
		TurnID: turnID,
	}
}

func userDraft(content, turnID string) backend.ThreadMessageDraft {
	return backend.ThreadMessageDraft{
		Role: apitypes.ThreadRoleUser, Content: content, TurnID: turnID,
	}
}

func TestGetAgentThreadPreservesNativeGroups(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r1", Name: "Thread", Icon: "globe"})

	// One turn: prompt, two parallel calls declared in order a,b but
	// completed b then a, then the answer.
	drafts := []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "looking",
			backend.AgentThreadCall{ID: "call-a", Name: "web_search", Arguments: `{"q":"a"}`},
			backend.AgentThreadCall{ID: "call-b", Name: "web_search", Arguments: `{"q":"b"}`},
		),
		toolDraft(t, "t1", "call-b", "web_search", `{"q":"b"}`, "b hits", nil),
		toolDraft(t, "t1", "call-a", "web_search", `{"q":"a"}`, "a hits", nil),
		{Role: apitypes.ThreadRoleAssistant, Content: "done", TurnID: "t1"},
	}
	if err := svc.AppendThreadMessages(context.Background(), "r1", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}

	msgs, err := svc.GetAgentThread(context.Background(), "r1")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("replay messages = %d, want 5", len(msgs))
	}
	if msgs[0].Role != apitypes.ThreadRoleUser || msgs[0].Content != "search" {
		t.Errorf("msgs[0] = %s/%q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != apitypes.ThreadRoleAssistant || msgs[1].Content != "looking" {
		t.Errorf("msgs[1] = %s/%q, want the group's text", msgs[1].Role, msgs[1].Content)
	}
	if len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("group calls = %d, want 2", len(msgs[1].ToolCalls))
	}
	if msgs[1].ToolCalls[0].ID != "call-a" || msgs[1].ToolCalls[1].ID != "call-b" {
		t.Errorf("declared order lost: %+v", msgs[1].ToolCalls)
	}
	if msgs[1].ToolCalls[0].Arguments != `{"q":"a"}` {
		t.Errorf("arguments = %q, want verbatim", msgs[1].ToolCalls[0].Arguments)
	}
	// Outcomes follow in declared order, regardless of completion.
	if msgs[2].ToolCallID != "call-a" || msgs[2].Content != "a hits" {
		t.Errorf("msgs[2] = %s/%q, want call-a outcome first", msgs[2].ToolCallID, msgs[2].Content)
	}
	if msgs[3].ToolCallID != "call-b" || msgs[3].Content != "b hits" {
		t.Errorf("msgs[3] = %s/%q", msgs[3].ToolCallID, msgs[3].Content)
	}
	if msgs[4].Role != apitypes.ThreadRoleAssistant || msgs[4].Content != "done" {
		t.Errorf("msgs[4] = %s/%q, want the terminal answer", msgs[4].Role, msgs[4].Content)
	}
}

func TestGetAgentThreadProjectsIncompleteGroup(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r2", Name: "Thread", Icon: "globe"})

	// Two calls declared; only one outcome landed (interrupted turn).
	drafts := []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "working",
			backend.AgentThreadCall{ID: "call-a", Name: "web_search", Arguments: `{}`},
			backend.AgentThreadCall{ID: "call-b", Name: "web_search", Arguments: `{}`},
		),
		toolDraft(t, "t1", "call-a", "web_search", `{}`, "a hits", nil),
	}
	if err := svc.AppendThreadMessages(context.Background(), "r2", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}

	msgs, err := svc.GetAgentThread(context.Background(), "r2")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("replay messages = %d, want 3", len(msgs))
	}
	if len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].ID != "call-a" {
		t.Errorf("projected calls = %+v, want only the answered call", msgs[1].ToolCalls)
	}
	if msgs[1].Content != "working" {
		t.Errorf("group text = %q, want preserved", msgs[1].Content)
	}
	if msgs[2].ToolCallID != "call-a" {
		t.Errorf("outcome = %s, want call-a", msgs[2].ToolCallID)
	}
}

func TestGetAgentThreadKeepsTextOnlyInterruptedGroup(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r3", Name: "Thread", Icon: "globe"})

	drafts := []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "thinking out loud",
			backend.AgentThreadCall{ID: "call-a", Name: "web_search", Arguments: `{}`},
		),
	}
	if err := svc.AppendThreadMessages(context.Background(), "r3", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, err := svc.GetAgentThread(context.Background(), "r3")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("replay messages = %d, want 2", len(msgs))
	}
	if len(msgs[1].ToolCalls) != 0 {
		t.Errorf("text-only group kept calls: %+v", msgs[1].ToolCalls)
	}
	if msgs[1].Content != "thinking out loud" {
		t.Errorf("group text = %q", msgs[1].Content)
	}
}

func TestGetAgentThreadExcludesStructureOnlyGroup(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r4", Name: "Thread", Icon: "globe"})

	// No text, no outcome: the group is display structure only.
	drafts := []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "",
			backend.AgentThreadCall{ID: "call-a", Name: "web_search", Arguments: `{}`},
		),
	}
	if err := svc.AppendThreadMessages(context.Background(), "r4", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, err := svc.GetAgentThread(context.Background(), "r4")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != apitypes.ThreadRoleUser {
		t.Fatalf("replay = %+v, want only the prompt", msgs)
	}
}

func TestGetAgentThreadMapsToolErrorToContent(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r5", Name: "Thread", Icon: "globe"})

	errText := "backend down"
	drafts := []backend.ThreadMessageDraft{
		userDraft("search", "t1"),
		groupDraft(t, "t1", "",
			backend.AgentThreadCall{ID: "call-a", Name: "web_search", Arguments: `{}`},
		),
		toolDraft(t, "t1", "call-a", "web_search", `{}`, "partial", &errText),
	}
	if err := svc.AppendThreadMessages(context.Background(), "r5", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, err := svc.GetAgentThread(context.Background(), "r5")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if msgs[2].Content != "backend down" {
		t.Errorf("tool content = %q, want the error text", msgs[2].Content)
	}
}

func TestGetAgentThreadRoundTripsJSONLiteralResults(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r6", Name: "Thread", Icon: "globe"})

	results := []string{`"quoted"`, `null`, `{"a":1}`, "  spaced  ", ""}
	drafts := []backend.ThreadMessageDraft{userDraft("go", "t1")}
	calls := make([]backend.AgentThreadCall, len(results))
	for i := range results {
		calls[i] = backend.AgentThreadCall{
			ID: fmt.Sprintf("call-%d", i), Name: "echo", Arguments: `{}`,
		}
	}
	drafts = append(drafts, groupDraft(t, "t1", "", calls...))
	for i, r := range results {
		drafts = append(drafts, toolDraft(t, "t1", calls[i].ID, "echo", `{}`, r, nil))
	}
	if err := svc.AppendThreadMessages(context.Background(), "r6", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, err := svc.GetAgentThread(context.Background(), "r6")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	for i, want := range results {
		got := msgs[2+i].Content
		if got != want {
			t.Errorf("result %d = %q, want %q", i, got, want)
		}
	}
}

func TestGetAgentThreadScopesReusedIDsToTheirGroup(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r7", Name: "Thread", Icon: "globe"})

	// Two model rounds in one turn reuse provider ids (call_0), and a
	// later turn repeats one again. Outcomes must pair positionally.
	drafts := []backend.ThreadMessageDraft{
		userDraft("first", "t1"),
		groupDraft(t, "t1", "", backend.AgentThreadCall{ID: "call_0", Name: "a", Arguments: `{}`}),
		toolDraft(t, "t1", "call_0", "a", `{}`, "first outcome", nil),
		{Role: apitypes.ThreadRoleAssistant, Content: "round one", TurnID: "t1"},
		groupDraft(t, "t1", "", backend.AgentThreadCall{ID: "call_0", Name: "b", Arguments: `{}`}),
		toolDraft(t, "t1", "call_0", "b", `{}`, "second outcome", nil),
		{Role: apitypes.ThreadRoleAssistant, Content: "round two", TurnID: "t1"},
		userDraft("again", "t2"),
		groupDraft(t, "t2", "", backend.AgentThreadCall{ID: "call_0", Name: "c", Arguments: `{}`}),
		toolDraft(t, "t2", "call_0", "c", `{}`, "third outcome", nil),
	}
	if err := svc.AppendThreadMessages(context.Background(), "r7", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, err := svc.GetAgentThread(context.Background(), "r7")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	var outcomes []string
	for _, m := range msgs {
		if m.Role == apitypes.ThreadRoleTool {
			outcomes = append(outcomes, m.Content)
		}
	}
	if len(outcomes) != 3 ||
		outcomes[0] != "first outcome" || outcomes[1] != "second outcome" || outcomes[2] != "third outcome" {
		t.Errorf("outcomes = %v, want each paired with its own group", outcomes)
	}
}

func TestGetAgentThreadExcludesErrorRows(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "r8", Name: "Thread", Icon: "globe"})

	drafts := []backend.ThreadMessageDraft{
		userDraft("visible", "t1"),
		{Role: apitypes.ThreadRoleError, Content: "failed"},
	}
	if err := svc.AppendThreadMessages(context.Background(), "r8", drafts); err != nil {
		t.Fatalf("append: %v", err)
	}

	msgs, err := svc.GetAgentThread(context.Background(), "r8")
	if err != nil {
		t.Fatalf("GetAgentThread: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "visible" {
		t.Fatalf("replay = %+v, want only the conversation row", msgs)
	}
}

func TestGetAgentThreadRejectsCorruptStructure(t *testing.T) {
	cases := []struct {
		name   string
		drafts func(t *testing.T) []backend.ThreadMessageDraft
	}{
		{
			name: "undecodable envelope",
			drafts: func(t *testing.T) []backend.ThreadMessageDraft {
				return []backend.ThreadMessageDraft{
					userDraft("x", "t1"),
					{Role: apitypes.ThreadRoleTool, Content: "{nope", TurnID: "t1"},
				}
			},
		},
		{
			name: "missing call id",
			drafts: func(t *testing.T) []backend.ThreadMessageDraft {
				env, _ := json.Marshal(backend.ToolEnvelope{Name: "x", Result: ""})
				return []backend.ThreadMessageDraft{
					userDraft("x", "t1"),
					{Role: apitypes.ThreadRoleTool, Content: string(env), TurnID: "t1"},
				}
			},
		},
		{
			name: "orphan outcome",
			drafts: func(t *testing.T) []backend.ThreadMessageDraft {
				return []backend.ThreadMessageDraft{
					userDraft("x", "t1"),
					toolDraft(t, "t1", "call-z", "x", `{}`, "r", nil),
				}
			},
		},
		{
			name: "duplicate id within a group",
			drafts: func(t *testing.T) []backend.ThreadMessageDraft {
				raw, _ := json.Marshal([]backend.AgentThreadCall{
					{ID: "dup", Name: "a", Arguments: `{}`},
					{ID: "dup", Name: "b", Arguments: `{}`},
				})
				return []backend.ThreadMessageDraft{
					userDraft("x", "t1"),
					{Role: apitypes.ThreadRoleAssistant, ToolCalls: raw, TurnID: "t1"},
				}
			},
		},
		{
			name: "non-string result",
			drafts: func(t *testing.T) []backend.ThreadMessageDraft {
				return []backend.ThreadMessageDraft{
					userDraft("x", "t1"),
					groupDraft(t, "t1", "", backend.AgentThreadCall{ID: "call-1", Name: "x", Arguments: `{}`}),
					{Role: apitypes.ThreadRoleTool, Content: `{"name":"x","callId":"call-1","result":{"a":1}}`, TurnID: "t1"},
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, svc := resolverDB(t)
			testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{ID: "corrupt", Name: "Thread", Icon: "globe"})
			// Seeded past the sink: the write path rejects these
			// drafts outright, so the read side is pinned against
			// rows only a foreign writer could create.
			seedDraftsRaw(t, sqlDB, "corrupt", tc.drafts(t))
			if _, err := svc.GetAgentThread(context.Background(), "corrupt"); err == nil {
				t.Fatal("GetAgentThread succeeded, want a corruption error")
			}
		})
	}
}

// seedDraftsRaw inserts drafts as rows directly, bypassing the sink's
// validation — the fixture for read-side corruption pins.
func seedDraftsRaw(t *testing.T, sqlDB *sql.DB, agentID string, drafts []backend.ThreadMessageDraft) {
	t.Helper()
	for i, d := range drafts {
		_, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, created_at, turn_id, tool_calls, thread_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("raw-%s-%d", agentID, i), agentID, string(d.Role), d.Content,
			time.Now().UnixMilli()+int64(i), d.TurnID, string(d.ToolCalls), testdb.ThreadID(agentID),
		)
		if err != nil {
			t.Fatalf("seed raw draft: %v", err)
		}
	}
}

func TestGetAgentThreadUnknownAgentFails(t *testing.T) {
	_, svc := resolverDB(t)
	_, err := svc.GetAgentThread(context.Background(), "ghost")
	if !errors.Is(err, backend.ErrUnknownAgent) {
		t.Fatalf("GetAgentThread error = %v, want ErrUnknownAgent", err)
	}
}
