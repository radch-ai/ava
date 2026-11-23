package agent

// Recorder mechanics under the native hook: complete assistant groups
// commit once with declared order and verbatim arguments, malformed groups
// error before dispatch, required-write failures surface as the sentinel,
// a closed recorder refuses late writes, and the invocation registry feeds
// the prefix snapshot — pending calls included.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

func newTestRecorder(journal LifecycleJournal) *turnRecorder {
	// The recorder publishes through its lane's state and reads the
	// lane's journal, so the harness carries a bare worker: no
	// subscribers, no claim loop.
	lane := &UserAgent{
		state:   newAgentState("agent-1", context.Background(), &sync.WaitGroup{}, zerolog.Nop()),
		logger:  zerolog.Nop(),
		journal: journal,
	}
	return newTurnRecorder(lane, "turn-1", context.Background())
}

func assistantState(msgs ...*schema.Message) *adk.TypedChatModelAgentState[*schema.Message] {
	return &adk.TypedChatModelAgentState[*schema.Message]{Messages: msgs}
}

func TestTurnRecorderCapturesNativeGroup(t *testing.T) {
	journal := newFakeJournal()
	rec := newTestRecorder(journal)

	msg := schema.AssistantMessage("looking", nil)
	msg.ToolCalls = []schema.ToolCall{
		{ID: "call-a", Function: schema.FunctionCall{Name: "web_search", Arguments: `{"q":"a"}`}},
		{ID: "call-b", Function: schema.FunctionCall{Name: "web_search", Arguments: `{"q":"b"}`}},
	}
	if _, _, err := rec.AfterModelRewriteState(context.Background(), assistantState(msg), nil); err != nil {
		t.Fatalf("hook: %v", err)
	}
	writes := journal.writesFor("turn-1")
	if len(writes) != 1 || len(writes[0].Drafts) != 1 {
		t.Fatalf("writes = %+v, want one assistant draft", writes)
	}
	draft := writes[0].Drafts[0]
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
	if got := journal.writesFor("turn-1"); len(got) != 1 {
		t.Fatalf("terminal message persisted by the hook: %+v", got)
	}
	if got := rec.terminalContent("fallback"); got != "done" {
		t.Errorf("terminalContent = %q, want the terminal round", got)
	}
}

func TestTurnRecorderRejectsMalformedGroup(t *testing.T) {
	rec := newTestRecorder(newFakeJournal())

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
	journal := newFakeJournal()
	journal.failAppend = errors.New("disk full")
	rec := newTestRecorder(journal)

	msg := schema.AssistantMessage("", nil)
	msg.ToolCalls = []schema.ToolCall{{ID: "c1", Function: schema.FunctionCall{Name: "x"}}}
	_, _, err := rec.AfterModelRewriteState(context.Background(), assistantState(msg), nil)
	var rwe *requiredWriteError
	if !errors.As(err, &rwe) {
		t.Fatalf("hook error = %v, want requiredWriteError", err)
	}
}

func TestTurnRecorderRefusesWritesAfterClose(t *testing.T) {
	journal := newFakeJournal()
	rec := newTestRecorder(journal)
	rec.close()
	if err := rec.persist([]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleTool}}); err != nil {
		t.Fatalf("late persist: %v", err)
	}
	if err := rec.recordStart("inv-1", "call-1", "tool", "{}"); err != nil {
		t.Fatalf("late recordStart: %v", err)
	}
	if err := rec.recordOutcome("inv-1", "done", nil); err != nil {
		t.Fatalf("late recordOutcome: %v", err)
	}
	if writes := journal.writesFor("turn-1"); len(writes) != 0 {
		t.Fatalf("closed recorder wrote %+v, want nothing", writes)
	}
}

// A tool's intent commits before the external call and its outcome after;
// the prefix snapshot reports only what is not yet durable — a committed
// outcome rides the journal as a tool row, so the snapshot drops it
// instead of repeating it.
func TestTurnRecorderTracksInvocations(t *testing.T) {
	journal := newFakeJournal()
	rec := newTestRecorder(journal)

	if err := rec.recordStart("inv-1", "call-1", "web_search", `{"q":"a"}`); err != nil {
		t.Fatalf("recordStart: %v", err)
	}
	pending := rec.snapshotInvocations()
	if len(pending) != 1 || !pending[0].Pending || pending[0].InvocationID != "inv-1" ||
		pending[0].Name != "web_search" || pending[0].Args != `{"q":"a"}` {
		t.Fatalf("pending invocation = %+v, want the recorded intent", pending)
	}

	errText := "boom"
	if err := rec.recordOutcome("inv-1", "partial", errors.New(errText)); err != nil {
		t.Fatalf("recordOutcome: %v", err)
	}
	if got := rec.snapshotInvocations(); len(got) != 0 {
		t.Fatalf("snapshot after commit = %+v, want the committed invocation dropped", got)
	}
	finished := 0
	for _, w := range journal.writesFor("turn-1") {
		if w.Kind == apitypes.LifecycleToolFinished {
			finished++
		}
	}
	if finished != 1 {
		t.Fatalf("tool_finished writes = %d, want the committed outcome", finished)
	}
	// Repeated provider call ids never collide: identity is the
	// invocation id.
	if err := rec.recordStart("inv-2", "call-1", "web_search", `{"q":"b"}`); err != nil {
		t.Fatalf("recordStart second round: %v", err)
	}
	got := rec.snapshotInvocations()
	if len(got) != 1 || got[0].InvocationID != "inv-2" || !got[0].Pending {
		t.Fatalf("invocations = %+v, want only the new pending identity", got)
	}
}

func TestTurnRecorderStartFailureIsRequiredWrite(t *testing.T) {
	journal := newFakeJournal()
	journal.failTool = errors.New("journal down")
	rec := newTestRecorder(journal)

	err := rec.recordStart("inv-1", "call-1", "tool", "{}")
	var rwe *requiredWriteError
	if !errors.As(err, &rwe) {
		t.Fatalf("recordStart error = %v, want requiredWriteError", err)
	}
	if got := rec.snapshotInvocations(); len(got) != 0 {
		t.Fatalf("invocations = %+v, want none after a failed start", got)
	}
}
