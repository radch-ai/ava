package agent

// Shake turns: a /shake item executes as one durable context elision.
// No model, no tools, no persona, and no user row — the work is the
// backend's selection transaction, its one lifecycle event, and its
// completion. The thread does not rotate, so no divider is emitted. A
// failure is a message-level failure like any other, so the pause gate
// holds the items queued behind it. The turn skeleton is runCommandTurn
// in turn_command.go; only the backend operation differs.

import (
	"context"

	"github.com/radch-ai/ava/backend"
)

// processShake executes a shake item: one durable context elision.
func (a *UserAgent) processShake(claim *turnClaim, emitCtx context.Context, msgID string) turnOutcome {
	return a.runCommandTurn(claim, emitCtx, msgID, "shake", "shake context",
		func(ctx context.Context) (*backend.JournalBatch, error) {
			_, batch, err := a.journal.ShakeThreadContext(ctx, a.state.agentID, msgID)
			return batch, err
		})
}
