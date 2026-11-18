package agent

// Recorder mechanics under the native hook: complete assistant groups
// persist once with declared order and verbatim arguments, malformed
// groups error before dispatch, required-write failures surface as the
// sentinel, and a closed recorder refuses late writes.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

func newTestRecorder(sink TurnSink) *turnRecorder {
	return &turnRecorder{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.Message]{},
		agentID:                           "agent-1",
		turnID:                            "turn-1",
		emitCtx:                           context.Background(),
		sink:                              sink,
	}
}

func assistantState(msgs ...*schema.Message) *adk.TypedChatModelAgentState[*schema.Message] {
	return &adk.TypedChatModelAgentState[*schema.Message]{Messages: msgs}
}

func TestTurnRecorderCapturesNativeGroup(t *testing.T) {
	var got [][]backend.ThreadMessageDraft
	rec := newTestRecorder(func(_ context.Context, _ string, msgs []backend.ThreadMessageDraft) error {
		got = append(got, msgs)
		return nil
	})

	msg := schema.AssistantMessage("looking", nil)
	msg.ToolCalls = []schema.ToolCall{
		{ID: "call-a", Function: schema.FunctionCall{Name: "web_search", Arguments: `{"q":"a"}`}},
		{ID: "call-b", Function: schema.FunctionCall{Name: "web_search", Arguments: `{"q":"b"}`}},
	}
	if _, _, err := rec.AfterModelRewriteState(context.Background(), assistantState(msg), nil); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("writes = %+v, want one assistant draft", got)
	}
	draft := got[0][0]
	if draft.Role != apitypes.ThreadRoleAssistant || draft.Content != "looking" ||
		draft.TurnID != "turn-1" {
		t.Fatalf("draft = %+v", draft)
	}
	var calls []backend.AgentThreadCall
	if err := json.Unmarshal(draft.ToolCalls, &calls); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if len(calls) != 2 || calls[0].ID != "call-a" || calls[1].ID != "call-b" {
		t.Fatalf("calls = %+v, want declared order", calls)
	}
	if calls[0].Arguments != `{"q":"a"}` {
		t.Errorf("arguments = %q, want verbatim", calls[0].Arguments)
	}

	// The terminal answer is settlement's row, not the recorder's.
	final := schema.AssistantMessage("done", nil)
	if _, _, err := rec.AfterModelRewriteState(context.Background(), assistantState(final), nil); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("terminal message persisted by the hook: %+v", got)
	}
	if got := rec.terminalContent("fallback"); got != "done" {
		t.Errorf("terminalContent = %q, want the terminal round", got)
	}
}

func TestTurnRecorderRejectsMalformedGroup(t *testing.T) {
	rec := newTestRecorder(nil)

	noID := schema.AssistantMessage("", nil)
	noID.ToolCalls = []schema.ToolCall{{Function: schema.FunctionCall{Name: "x"}}}
	if _, _, err := rec.AfterModelRewriteState(context.Background(), assistantState(noID), nil); err == nil {
		t.Fatal("empty call id accepted")
	}

	dup := schema.AssistantMessage("", nil)
	dup.ToolCalls = []schema.ToolCall{
		{ID: "same", Function: schema.FunctionCall{Name: "a"}},
		{ID: "same", Function: schema.FunctionCall{Name: "b"}},
	}
	if _, _, err := rec.AfterModelRewriteState(context.Background(), assistantState(dup), nil); err == nil {
		t.Fatal("duplicate call id accepted")
	}
}

func TestTurnRecorderRequiredWriteError(t *testing.T) {
	rec := newTestRecorder(func(context.Context, string, []backend.ThreadMessageDraft) error {
		return errors.New("disk full")
	})
	msg := schema.AssistantMessage("", nil)
	msg.ToolCalls = []schema.ToolCall{{ID: "c1", Function: schema.FunctionCall{Name: "x"}}}
	_, _, err := rec.AfterModelRewriteState(context.Background(), assistantState(msg), nil)
	var rwe *requiredWriteError
	if !errors.As(err, &rwe) {
		t.Fatalf("hook error = %v, want requiredWriteError", err)
	}
}

func TestTurnRecorderRefusesWritesAfterClose(t *testing.T) {
	writes := 0
	rec := newTestRecorder(func(context.Context, string, []backend.ThreadMessageDraft) error {
		writes++
		return nil
	})
	rec.close()
	if err := rec.persist([]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleTool}}); err != nil {
		t.Fatalf("late persist: %v", err)
	}
	if writes != 0 {
		t.Fatalf("closed recorder wrote %d batches, want 0", writes)
	}
}
