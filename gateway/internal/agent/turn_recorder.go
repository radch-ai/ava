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
	"github.com/radch-ai/ava/gateway/wstypes"
)

// requiredWriteError marks a transcript write that failed while the
// graph was running. The event loop classifies it as a message-level
// failure before any stop/cancel check, so a write racing a user stop
// still fails the message.
type requiredWriteError struct{ err error }

func (e *requiredWriteError) Error() string { return e.err.Error() }
func (e *requiredWriteError) Unwrap() error { return e.err }

// invState is one invocation of the running turn as the prefix snapshot
// reports it: the recorded intent plus, once it finishes, its outcome.
// The map key is the invocation id, so the state need not repeat it.
type invState struct {
	name      string
	args      string
	result    string
	errText   string
	pending   bool
	committed bool // outcome row is durable; the journal carries it now
}

// turnRecorder records one run's replay structure through the
// assistant-message hook and the tool wrapper's outcome capture, and
// keeps the turn's invocation registry for the prefix snapshot.
type turnRecorder struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.Message]

	turnID  string
	emitCtx context.Context
	lane    *UserAgent

	mu     sync.Mutex
	closed bool
	last   *schema.Message // the group's text and, at the end, the terminal answer
	// invocations is the registry; order records first-start order so
	// the prefix snapshot is stable rather than map-ordered.
	invocations map[string]*invState
	order       []string
}

// newTurnRecorder builds one turn's recorder. The lane owns the agent
// identity and the journal the backend attributes rows through; the
// item's execution thread was resolved when the item started.
func newTurnRecorder(lane *UserAgent, turnID string, emitCtx context.Context) *turnRecorder {
	return &turnRecorder{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.Message]{},
		turnID:                            turnID,
		emitCtx:                           emitCtx,
		lane:                              lane,
		invocations:                       make(map[string]*invState),
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

// persist writes one required batch and publishes the event that carries
// it. A closed recorder refuses: the turn is over, and a late row would
// land after the next turn's prompt.
func (r *turnRecorder) persist(msgs []backend.ThreadMessageDraft) error {
	if r.lane.journal == nil || r.isClosed() {
		return nil
	}
	err := r.lane.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		batch, err := r.lane.journal.AppendItemMessages(r.emitCtx, r.lane.state.agentID, r.turnID, msgs)
		if err != nil {
			return nil, err
		}
		// The committed rows ride the journal from here: drop their
		// text from the replay prefix inside the same publication
		// boundary, so a mid-turn snapshot never repeats what replay
		// already delivers.
		r.lane.dropCommittedPrefix()
		return batch, nil
	})
	if err != nil {
		return &requiredWriteError{err: err}
	}
	return nil
}

// recordStart records one invocation's intent before it runs, publishes
// the event that carries it, and registers the invocation for the prefix
// snapshot. A required-write error reaches the wrapper, which then never
// invokes the tool: intent the journal cannot record must not run.
func (r *turnRecorder) recordStart(invocationID, callID, name, args string) error {
	if r.isClosed() {
		return nil
	}
	if r.lane.journal == nil {
		return nil
	}
	err := r.lane.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		batch, err := r.lane.journal.RecordToolStarted(r.emitCtx, r.lane.state.agentID, r.turnID,
			invocationID, callID, name, args)
		if err != nil {
			return nil, err
		}
		// Registration rides the publication boundary: a prefix cut
		// taken after the frame went out always names this call.
		r.mu.Lock()
		r.invocations[invocationID] = &invState{
			name:    name,
			args:    args,
			pending: true,
		}
		r.order = append(r.order, invocationID)
		r.mu.Unlock()
		return batch, nil
	})
	if err != nil {
		return &requiredWriteError{err: err}
	}
	return nil
}

// recordOutcome commits the invocation's outcome row and publishes the
// event that carries it. It returns a required-write error when the
// write fails — the wrapper passes it out of the invocation so the round
// aborts at message level.
func (r *turnRecorder) recordOutcome(invocationID, callID, name, result string, invocationErr error) error {
	errText := ""
	if invocationErr != nil {
		// One rendered cause for the row and the live bubble, so live
		// and reloaded state never disagree on wording.
		errText = cleanToolError(invocationErr)
	}
	r.mu.Lock()
	closed := r.closed
	if !closed {
		if inv, ok := r.invocations[invocationID]; ok {
			inv.result = result
			inv.errText = errText
			inv.pending = false
		}
	}
	r.mu.Unlock()
	if closed || r.lane.journal == nil {
		return nil
	}
	// The journal's error half is a nil-or-value pointer, so an empty
	// rendered cause stays absent from the stored envelope.
	var errRef *string
	if errText != "" {
		errRef = &errText
	}
	err := r.lane.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		batch, err := r.lane.journal.RecordToolFinished(r.emitCtx, r.lane.state.agentID, r.turnID,
			invocationID, result, errRef)
		if err != nil {
			return nil, err
		}
		// The committed outcome rides the journal as a tool row: mark
		// the invocation inside the publication boundary so a prefix
		// cut racing the publish cannot still report it as snapshot
		// state.
		r.mu.Lock()
		if inv, ok := r.invocations[invocationID]; ok {
			inv.committed = true
		}
		r.mu.Unlock()
		return batch, nil
	})
	if err != nil {
		return &requiredWriteError{err: err}
	}
	return nil
}

// snapshotInvocations renders the turn's invocations for a prefix
// snapshot, pending ones included: a device that reconnects mid-call
// sees the call that began while it was away. Committed invocations
// ride the journal as tool rows; the snapshot carries only what is not
// yet durable.
func (r *turnRecorder) snapshotInvocations() []wstypes.ToolInvocationState {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]wstypes.ToolInvocationState, 0, len(r.order))
	for _, id := range r.order {
		inv, ok := r.invocations[id]
		if !ok || inv.committed {
			continue
		}
		state := wstypes.ToolInvocationState{
			InvocationID: id,
			Name:         inv.name,
			Args:         inv.args,
			Result:       inv.result,
			Error:        inv.errText,
			Pending:      inv.pending,
		}
		out = append(out, state)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
