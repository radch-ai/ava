package agent

// Tool capture: every enabled tool rides a wrapper so the lane sees
// invocations as they happen. Eino ships callbacks and tool-call
// middleware, but its tool adapter drops the result when an
// invocation returns an error, so the wrapper is the one place where
// a call's arguments, its original result and its error are all in
// hand — the wire's call/result pair and the thread's tool row both
// hang off it. The turn id rides the wrapper, captured when the
// turn's tools were built: a late result reports its own turn, not
// whichever turn is active when it lands.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// wrapTools rides every fetched implementation on the capture
// wrapper, once per turn — the fetch already applied permissions and
// global switches. Metadata is read here, not per invocation, and a
// selected tool that cannot describe itself fails the turn: a tool
// the model was promised must never vanish silently.
func (a *UserAgent) wrapTools(ctx context.Context, emitCtx context.Context, turnID string, impls []tool.InvokableTool) ([]tool.BaseTool, error) {
	if len(impls) == 0 {
		return nil, nil
	}
	wrapped := make([]tool.BaseTool, 0, len(impls))
	for _, impl := range impls {
		info, err := impl.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("tool metadata: %w", err)
		}
		if info == nil || info.Name == "" {
			return nil, errors.New("tool metadata: missing name")
		}
		wrapped = append(wrapped, &capturingTool{inner: impl, info: info, lane: a, turnID: turnID, emitCtx: emitCtx})
	}
	return wrapped, nil
}

// capturingTool delegates to the wrapped implementation and reports
// each invocation to the lane: the call before it runs, the outcome
// after. info is the turn's snapshot, so the runner's schema and the
// wire's name agree without re-reading the implementation. Both
// reports go out on emitCtx, which outlives the turn's cancel — a
// failed or stopped turn still reports its last tool's outcome.
type capturingTool struct {
	inner   tool.InvokableTool
	info    *schema.ToolInfo
	lane    *UserAgent
	turnID  string
	emitCtx context.Context
}

func (t *capturingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *capturingTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	// The model's own call id when the runner threaded it through;
	// a fresh one otherwise, so the pair still correlates.
	callID := compose.GetToolCallID(ctx)
	if callID == "" {
		callID = uuid.NewString()
	}
	t.lane.emitToolCall(t.emitCtx, callID, t.turnID, t.info.Name, args)
	result, err := t.inner.InvokableRun(ctx, args, opts...)
	t.lane.recordToolResult(t.emitCtx, callID, t.turnID, t.info.Name, args, result, err)
	if err != nil {
		// Named on the way out so the turn's error event can say
		// which tool failed without the runner's internal framing.
		err = &toolError{name: t.info.Name, err: err}
	}
	return result, err
}

// toolError carries the name of the tool an invocation failed in.
type toolError struct {
	name string
	err  error
}

func (e *toolError) Error() string { return e.err.Error() }
func (e *toolError) Unwrap() error { return e.err }

// emitToolCall tells the lane's connections a tool is about to run.
func (a *UserAgent) emitToolCall(ctx context.Context, id, turnID, name, args string) {
	_ = a.state.conns.WriteJSON(ctx, wstypes.Event{
		Type:    wstypes.EventTypeToolCall,
		AgentID: a.agentID,
		Data:    wstypes.ToolCallEvent{ID: id, TurnID: turnID, Name: name, Args: args},
	})
}

// recordToolResult reports a completed invocation: the wire event,
// then the thread row — best-effort as every persistence site, so a
// sick sink logs and the turn runs on. The row lands between the
// claim-written user row and the settle-written assistant row, so
// the thread records what a turn did even when it later fails.
func (a *UserAgent) recordToolResult(ctx context.Context, id, turnID, name, args, result string, invocationErr error) {
	var errText *string
	if invocationErr != nil {
		// One rendered cause for both the event and the row, so live
		// and reloaded state never disagree on wording.
		msg := cleanToolError(invocationErr)
		errText = &msg
	}
	_ = a.state.conns.WriteJSON(ctx, wstypes.Event{
		Type:    wstypes.EventTypeToolResult,
		AgentID: a.agentID,
		Data:    wstypes.ToolResultEvent{ID: id, TurnID: turnID, Result: result, Error: errText},
	})
	if a.persistTurn == nil {
		return
	}
	envelope, err := json.Marshal(apitypes.ToolEnvelope{
		Name:   name,
		Args:   rawJSON(args),
		Result: rawJSON(result),
		Error:  errText,
	})
	if err != nil {
		a.logger.Warn().Err(err).Str("agent_id", a.agentID).Msg("marshal tool envelope")
		return
	}
	if err := a.persistTurn(ctx, a.agentID, []apitypes.ThreadMessageDraft{
		{Role: apitypes.ThreadRoleTool, Content: string(envelope)},
	}); err != nil {
		a.logger.Warn().Err(err).Str("agent_id", a.agentID).Msg("persist tool message")
	}
}

// rawJSON keeps JSON verbatim and quotes anything else — a tool's
// plain-text answer stays a valid envelope member.
func rawJSON(s string) json.RawMessage {
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	quoted, _ := json.Marshal(s) // cannot fail for a string
	return quoted
}
