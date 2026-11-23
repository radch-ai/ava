package agent

// Command turns: a /new or /reset item executes as one durable thread
// switch. No model, no tools, no persona, and no user row — the work is
// the backend's switch transaction, its two lifecycle events
// (thread_changed then item_finished), and their display record. A
// failure is a message-level failure like any other, so the pause gate
// holds the items queued behind it.

import (
	"context"

	"github.com/radch-ai/ava/backend"
)

// processCommand executes a new_thread item: the durable start, then the
// switch. Cancellation before the switch commits rolls it back and the
// item settles as a quiet stop; a committed switch is always reported,
// even when a Stop raced in behind it.
func (a *UserAgent) processCommand(claim *turnClaim, emitCtx context.Context, msgID string) turnOutcome {
	if a.journal == nil {
		a.logger.Error().Str("agent_id", a.state.agentID).Msg("lane has no lifecycle journal")
		a.failTurn(emitCtx, msgID, "no lifecycle journal configured", "")
		return outcomeMessageFailed
	}
	// The started batch and the running projection land in one
	// publication section, exactly like a prompt's start.
	if err := a.state.startItem(func() (*backend.ItemStart, *backend.JournalBatch, error) {
		return a.journal.StartItem(claim.ctx, a.state.agentID, msgID)
	}); err != nil {
		a.logger.Error().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).Msg("start command")
		a.failTurn(emitCtx, msgID, "persist command start: "+err.Error(), "")
		return outcomeMessageFailed
	}

	err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		_, batch, err := a.journal.ExecuteThreadSwitch(claim.ctx, a.state.agentID, msgID)
		return batch, err
	})
	if err != nil {
		// The backend wraps the cause, so the raw error is the whole
		// message. setupFailure owns the stop check: cancellation leaves
		// the item started, so it settles as a stop and never reappears
		// as an interruption.
		return a.setupFailure(emitCtx, claim, msgID, "switch thread", err)
	}
	a.clearTurnPrefix()
	return outcomeSucceeded
}
