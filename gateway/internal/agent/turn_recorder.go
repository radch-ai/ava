package agent

// Turn recording: Eino's own synchronous extension points carry the
// thread structure. The model handler records each complete native
// assistant call group before its tools can run; the tool wrapper
// (tool_capture.go) records each invocation's original outcome. Both
// writes are required — a failure returns into the graph and stops it,
// surfacing as a message-level turn failure.
//
// All state here is turn-local: the recorder is built per run and
// captured by that run's tool wrappers. Closing it at turn end keeps
// an abandoned parallel invocation from writing a late row into the
// transcript, so no callback can touch a later turn.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/wsconn"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// requiredWriteError marks a transcript write that failed while the
// graph was running. The event loop classifies it as a message-level
// failure before any stop/cancel check, so a write racing a user stop
// still fails the message.
type requiredWriteError struct{ err error }

func (e *requiredWriteError) Error() string { return e.err.Error() }
func (e *requiredWriteError) Unwrap() error { return e.err }

// turnRecorder records one run's replay structure through the native
// assistant-message hook and the tool wrapper's outcome capture.
type turnRecorder struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.Message]

	agentID string
	conns   *wsconn.Broadcaster
	turnID  string
	emitCtx context.Context
	sink    TurnSink

	mu     sync.Mutex
	closed bool
	last   *schema.Message // the group's text and, at the end, the terminal answer
}

// newTurnRecorder builds one turn's recorder. Rows are attributed by
// the sink, which resolves the agent's active thread at write time.
func newTurnRecorder(lane *UserAgent, sink TurnSink, turnID string, emitCtx context.Context) *turnRecorder {
	return &turnRecorder{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.Message]{},
		agentID:                           lane.agentID,
		conns:                             lane.state.conns,
		turnID:                            turnID,
		emitCtx:                           emitCtx,
		sink:                              sink,
	}
}

// AfterModelRewriteState runs inside the model node, after the native
// response is assembled and before the tools node dispatches: the
// complete assistant message, its text, call ids, names, arguments,
// and declared order are all present. An error here aborts the model
// call, so the group's tools never run.
func (r *turnRecorder) AfterModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.Message],
	mc *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.TypedChatModelAgentState[*schema.Message], error) {
	if state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	msg := state.Messages[len(state.Messages)-1]
	if msg == nil || msg.Role != schema.Assistant {
		return ctx, state, nil
	}

	r.mu.Lock()
	r.last = msg
	r.mu.Unlock()

	if len(msg.ToolCalls) == 0 {
		// The terminal answer; settlement owns its row so the text is
		// persisted exactly once.
		return ctx, state, nil
	}

	calls := make([]backend.AgentThreadCall, 0, len(msg.ToolCalls))
	seen := make(map[string]struct{}, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		if tc.ID == "" {
			return ctx, state, errors.New("assistant tool call has no id")
		}
		if tc.Function.Name == "" {
			return ctx, state, fmt.Errorf("assistant tool call %q has no name", tc.ID)
		}
		if _, dup := seen[tc.ID]; dup {
			return ctx, state, fmt.Errorf("assistant message repeats call id %q", tc.ID)
		}
		seen[tc.ID] = struct{}{}
		// Arguments stay the model-authored JSON text, verbatim.
		calls = append(calls, backend.AgentThreadCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	group, err := json.Marshal(calls)
	if err != nil {
		return ctx, state, fmt.Errorf("marshal tool calls: %w", err)
	}
	if err := r.persist([]backend.ThreadMessageDraft{{
		Role:      apitypes.ThreadRoleAssistant,
		Content:   msg.Content,
		TurnID:    r.turnID,
		ToolCalls: group,
	}}); err != nil {
		return ctx, state, err
	}
	return ctx, state, nil
}

// persist writes one required batch. A closed recorder refuses: the
// turn is over, and a late row would land after the next turn's prompt.
func (r *turnRecorder) persist(msgs []backend.ThreadMessageDraft) error {
	if r.sink == nil {
		return nil
	}
	if r.isClosed() {
		return nil
	}
	if err := r.sink(r.emitCtx, r.agentID, msgs); err != nil {
		return &requiredWriteError{err: err}
	}
	return nil
}

// recordOutcome emits the invocation's wire event and persists its
// outcome row. It returns a required-write error when persistence
// fails — the wrapper passes it out of the invocation so the round
// aborts at message level.
func (r *turnRecorder) recordOutcome(callID, name, args, result string, invocationErr error) error {
	var errText *string
	if invocationErr != nil {
		// One rendered cause for both the event and the row, so live
		// and reloaded state never disagree on wording.
		msg := cleanToolError(invocationErr)
		errText = &msg
	}
	_ = r.conns.WriteJSON(r.emitCtx, wstypes.Event{
		Type:    wstypes.EventTypeToolResult,
		AgentID: r.agentID,
		Data:    wstypes.ToolResultEvent{ID: callID, TurnID: r.turnID, Result: result, Error: errText},
	})

	if r.isClosed() {
		return nil
	}
	// The envelope's Result is the exact model-facing string;
	// encoding/json quotes it once, so replay reads it verbatim.
	envelope, err := json.Marshal(backend.ToolEnvelope{
		Name:   name,
		CallID: callID,
		Args:   rawJSON(args),
		Result: result,
		Error:  errText,
	})
	if err != nil {
		return &requiredWriteError{err: fmt.Errorf("marshal tool envelope: %w", err)}
	}
	return r.persist([]backend.ThreadMessageDraft{{
		Role:    apitypes.ThreadRoleTool,
		Content: string(envelope),
		TurnID:  r.turnID,
	}})
}

// emitToolCall tells the lane's connections a tool is about to run.
func (r *turnRecorder) emitToolCall(callID, name, args string) {
	_ = r.conns.WriteJSON(r.emitCtx, wstypes.Event{
		Type:    wstypes.EventTypeToolCall,
		AgentID: r.agentID,
		Data:    wstypes.ToolCallEvent{ID: callID, TurnID: r.turnID, Name: name, Args: args},
	})
}

// close ends the turn's recording window. Late invocations may still
// report their wire event, but never write a durable row.
func (r *turnRecorder) close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
}

func (r *turnRecorder) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// terminalContent returns the terminal assistant round's text — the
// row settlement persists. A stopped turn keeps the accumulated
// streamed text instead.
func (r *turnRecorder) terminalContent(fallback string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last != nil && len(r.last.ToolCalls) == 0 {
		return r.last.Content
	}
	return fallback
}
