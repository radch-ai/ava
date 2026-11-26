package agent

// Command turns: a /new or /reset item executes as one durable thread
// switch. No model, no tools, no persona, and no user row — the work is
// the backend's switch transaction, its two lifecycle events
// (thread_changed then item_finished), and their display record. A
// failure is a message-level failure like any other, so the pause gate
// holds the items queued behind it. The shared command-turn skeleton
// (runCommandTurn below) also carries /shake.

import (
	"context"

	"github.com/radch-ai/ava/backend"
)

// processCommand executes a new_thread item: one durable thread switch.
func (a *UserAgent) processCommand(claim *turnClaim, emitCtx context.Context, msgID string) turnOutcome {
	return a.runCommandTurn(claim, emitCtx, msgID, "command", "switch thread",
		func(ctx context.Context) (*backend.JournalBatch, error) {
			_, batch, err := a.journal.ExecuteThreadSwitch(ctx, a.state.agentID, msgID)
			return batch, err
		})
}

// runCommandTurn runs one command item's shared skeleton: the journal
// guard, the durable start, the backend operation, and the completion
// publication. name rides the start diagnostics; site rides the
// operation's failure log. Cancellation before the operation commits
// rolls it back and the item settles as a quiet stop; a committed
// operation is always reported, even when a Stop raced in behind it.
func (a *UserAgent) runCommandTurn(claim *turnClaim, emitCtx context.Context, msgID, name, site string,
	op func(ctx context.Context) (*backend.JournalBatch, error)) turnOutcome {
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
		a.logger.Error().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).Msg("start " + name)
		a.failTurn(emitCtx, msgID, "persist "+name+" start: "+err.Error(), "")
		return outcomeMessageFailed
	}
	// The backend wraps the cause, so the raw error is the whole
	// message. setupFailure owns the stop check: cancellation leaves
	// the item started, so it settles as a stop and never reappears
	// as an interruption.
	if err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		return op(claim.ctx)
	}); err != nil {
		return a.setupFailure(emitCtx, claim, msgID, site, err)
	}
	a.clearTurnPrefix()
	return outcomeSucceeded
}
