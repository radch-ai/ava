package agent

// Turn failure rendering and reporting: how a failed turn reaches
// the wire and the thread.

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend"
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

// failTurn ends a failed turn and settles its item, reporting whether
// a terminal outcome committed. The item always reaches a terminal
// outcome — otherwise a restart would misreport it as interrupted. A
// turn-level failure (no tool attribution) carries one persisted error
// row, committed with the outcome, so live and reloaded state converge
// on the same row: the row rides the lifecycle event and no transient
// notice repeats it. A tool-attributed failure's own row already
// carries the failure, so the durable batch is the whole report. When
// the durable write itself fails, the client gets the transient notice
// and the caller knows the outcome is unrecorded.
func (a *UserAgent) failTurn(ctx context.Context, msgID, message, toolName string) bool {
	if a.journal != nil {
		var drafts []backend.ThreadMessageDraft
		if toolName == "" {
			drafts = []backend.ThreadMessageDraft{{
				ID:      uuid.NewString(),
				Role:    apitypes.ThreadRoleError,
				Content: message,
				TurnID:  msgID,
			}}
		}
		err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
			return a.journal.FinishItem(ctx, a.state.agentID, msgID, apitypes.ItemFailed, "", drafts)
		})
		if err == nil {
			a.clearTurnPrefix()
			return true
		}
		a.logger.Warn().Err(err).Str("agent_id", a.state.agentID).Msg("persist item outcome")
	}
	a.emitError(ctx, message, toolName, msgID)
	return false
}

// emitError reports a transient failure to the lane's connections. The
// replay prefix retires first: nothing replays after the turn is over.
// Durable failures arrive as lifecycle events instead.
func (a *UserAgent) emitError(ctx context.Context, message, tool, turnID string) {
	a.clearTurnPrefix()
	_ = a.state.conns.WriteJSON(ctx, wstypes.Event{
		Type:    wstypes.EventTypeError,
		AgentID: a.state.agentID,
		Data: wstypes.ErrorEvent{
			TurnID:  turnID,
			Message: message,
			Tool:    tool,
		},
	})
}
