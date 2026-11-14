package agent

// Tool capture: every enabled tool rides a wrapper so the lane sees
// invocations as they happen. The wrapper is the only place where a
// call's arguments and its outcome meet — the runner's event stream
// reports tool results but never the arguments — so the wire's
// call/result pair and the thread's tool row both hang off it.

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// capturingTool delegates to the wrapped tool and reports each
// invocation to the lane: the call before it runs, the outcome after.
// Both reports go out on emitCtx, which outlives the turn's cancel —
// a failed or stopped turn still reports its last tool's outcome.
type capturingTool struct {
	inner   tool.InvokableTool
	name    string
	lane    *UserAgent
	emitCtx context.Context
}

func (t *capturingTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *capturingTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	// The model's own call id when the runner threaded it through;
	// a fresh one otherwise, so the pair still correlates.
	callID := compose.GetToolCallID(ctx)
	if callID == "" {
		callID = uuid.NewString()
	}
	t.lane.emitToolCall(t.emitCtx, callID, t.name, args)
	result, err := t.inner.InvokableRun(ctx, args, opts...)
	t.lane.recordToolResult(t.emitCtx, callID, t.name, args, result, err)
	return result, err
}

// emitToolCall tells the lane's connections a tool is about to run.
func (a *UserAgent) emitToolCall(ctx context.Context, id, name, args string) {
	_ = a.conns.WriteJSON(ctx, wstypes.Event{
		Type:    wstypes.EventTypeToolCall,
		AgentID: a.agentID,
		Data:    wstypes.ToolCallEvent{ID: id, Name: name, Args: args},
	})
}

// recordToolResult reports a completed invocation: the wire event,
// then the thread row — best-effort as every persistence site, so a
// sick sink logs and the turn runs on. The row lands between the
// drain-written user row and the settle-written assistant row, so
// the thread records what a turn did even when it later fails.
func (a *UserAgent) recordToolResult(ctx context.Context, id, name, args, result string, invocationErr error) {
	var errText *string
	if invocationErr != nil {
		msg := invocationErr.Error()
		errText = &msg
	}
	_ = a.conns.WriteJSON(ctx, wstypes.Event{
		Type:    wstypes.EventTypeToolResult,
		AgentID: a.agentID,
		Data:    wstypes.ToolResultEvent{ID: id, Result: result, Error: errText},
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
	quoted, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return quoted
}
