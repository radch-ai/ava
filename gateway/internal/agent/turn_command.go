package agent

// Command turns: a /new or /reset item executes as one durable thread
// switch. No model, no tools, no persona, and no user row — the work
// is the backend's create-and-activate transaction plus its display
// record. A failure is a message-level failure like any other, so the
// pause gate holds the items queued behind it.

import (
	"context"
	"time"

	"github.com/radch-ai/ava/gateway/wstypes"
)

// dividerText is the label the thread boundary renders. It matches
// the backend display endpoint's own divider text.
const dividerText = "New session"

// eventTimeFormat is the display endpoint's ISO shape (isoTime in
// backend/internal/api/agent): one format string shared by contract,
// not an exported cross-package helper.
const eventTimeFormat = "2006-01-02T15:04:05.000Z"

// processCommand executes a new_thread item: the durable switch, then
// the completion event. Cancellation before the commit rolls the
// transaction back and settles as a quiet stop; a committed switch is
// always reported, even when a Stop raced in behind it.
func (a *UserAgent) processCommand(claim *turnClaim, emitCtx context.Context, msgID string) turnOutcome {
	if a.createThread == nil {
		a.logger.Error().Str("agent_id", a.agentID).Msg("lane has no thread activator")
		a.failTurn(emitCtx, msgID, "no thread activator configured", "")
		return outcomeMessageFailed
	}
	res, err := a.createThread(claim.ctx, a.agentID)
	if err != nil {
		// The backend already wraps the cause ("create thread: %w"),
		// so the raw error is the whole message. Cancellation stays a
		// quiet stop: nothing committed, nothing to report.
		return a.setupFailure(emitCtx, claim, msgID, "create thread", err)
	}
	a.clearTurnPrefix()
	_ = a.state.conns.WriteJSON(emitCtx, wstypes.Event{
		Type:    wstypes.EventTypeCommandCompleted,
		AgentID: a.agentID,
		Data: wstypes.CommandCompletedEvent{
			ItemID:           msgID,
			ThreadID:         res.ID,
			PreviousThreadID: res.PreviousID,
			Divider: wstypes.ThreadDivider{
				ID:        res.ID,
				Content:   dividerText,
				CreatedAt: time.UnixMilli(res.CreatedAt).UTC().Format(eventTimeFormat),
			},
		},
	})
	return outcomeSucceeded
}
