package agent

// Turn failure rendering and reporting: how a failed turn reaches
// the wire and the thread.

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// toolFailure, when err is a tool invocation failure, names the
// tool and renders its cause with the invoke-harness frame peeled
// off. ok is false for anything else.
func toolFailure(err error) (tool, cause string, ok bool) {
	var terr *toolError
	if !errors.As(err, &terr) {
		return "", "", false
	}
	return terr.name, cleanToolError(terr), true
}

// turnError renders a turn failure for the wire: a tool failure
// names its tool and shows its clean cause; anything else keeps its
// own text.
func turnError(err error) (text, toolName string) {
	tool, cause, ok := toolFailure(err)
	if !ok {
		return err.Error(), ""
	}
	return cause, tool
}

// invokeHarnessFrame prefixes every error eino's tool adapter wraps
// around a LocalFunc's failure (components/tool/utils/invokable_func.go).
const invokeHarnessFrame = "[LocalFunc] "

// cleanToolError renders a tool failure's cause: the tool's own
// attribution drops (the row already names it) and the invoke
// harness's frame peels off, so the text shows why the tool failed
// rather than how it was invoked. Any other wrapping keeps its
// context — a tool's own "fetch customer record: ..." survives.
func cleanToolError(err error) string {
	var terr *toolError
	if errors.As(err, &terr) {
		err = terr.err
	}
	if inner := errors.Unwrap(err); inner != nil && strings.HasPrefix(err.Error(), invokeHarnessFrame) {
		return inner.Error()
	}
	return err.Error()
}

// failTurn ends a failed turn. A turn-level failure (no tool
// attribution) is persisted as an error row before its event — the
// refetch a client makes on the event cannot outrun the write — and
// the event then names the stored row, so live and reloaded state
// converge on one id. A failed or missing sink leaves the row
// unconfirmed: the event stays transient and carries the original
// failure, never the sink's error. Tool-attributed failures already
// persist in their envelope and stay transient here.
func (a *UserAgent) failTurn(ctx context.Context, msgID, message, toolName string) {
	rowID := ""
	if toolName == "" && a.persistTurn != nil {
		rowID = uuid.NewString()
		if err := a.persistTurn(ctx, a.agentID,
			[]apitypes.ThreadMessageDraft{{ID: rowID, Role: apitypes.ThreadRoleError, Content: message}}); err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.agentID).Msg("persist error message")
			rowID = ""
		}
	}
	a.emitError(ctx, message, toolName, rowID, msgID)
}

// emitError reports a failed turn to the lane's connections. The
// replay prefix retires first: nothing replays after the turn is
// over. rowID names the stored error row when the failure persisted;
// turnID names the failed turn's stream.
func (a *UserAgent) emitError(ctx context.Context, message, tool, rowID, turnID string) {
	a.clearTurnPrefix()
	_ = a.state.conns.WriteJSON(ctx, wstypes.Event{
		Type:    wstypes.EventTypeError,
		AgentID: a.agentID,
		Data: wstypes.ErrorEvent{
			ID:      rowID,
			TurnID:  turnID,
			Message: message,
			Tool:    tool,
		},
	})
}
