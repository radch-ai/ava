package backend

// /shake's durable half plus the reference retrieval that restores what
// it elided. The stored row is never rewritten: elision flips one flag,
// and the projection swaps the payload for a placeholder naming the row.
// The same file owns the model-facing text of both ends — the
// placeholder and the restored original — so they can never drift.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/threadpage"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// Elision bound. Below the minimum, the placeholder costs nearly as
// much as the result it replaces. The restore side needs no bound of
// its own: the delivered-output ceiling in tooladmission governs it.
const shakeMinResultBytes = 2048

// errUnknownReference is the retrieval answer for a reference that names
// no tool row of the calling agent. A cross-agent id reads the same.
var errUnknownReference = errors.New("unknown tool output reference")

// ShakeResult reports what one /shake committed: the number of rows
// whose payload left the model-facing projection.
type ShakeResult struct {
	Elided int
}

// elidedReference is the placeholder the projection emits instead of an
// elided tool result. It names the row so the agent can restore the
// exact original with read_tool_output.
func elidedReference(rowID string, size int) string {
	return fmt.Sprintf("[elided tool output: %d bytes. Restore it with the read_tool_output tool: {\"reference\":%q}]", size, rowID)
}

// ShakeThreadContext runs one shake item: it marks this agent's active
// thread's old tool rows as elided, settles the item completed, and
// journals exactly one event. The stored rows keep their content — only
// the replay projection changes. The newest turn is protected: its
// exchanges are the ones the model is still reasoning about. The anchor
// is the thread's newest row's turn, so a newest turn without tool rows
// of its own still leaves every older payload elidable. Zero eligible
// rows is a successful no-op.
func (s *TurnService) ShakeThreadContext(ctx context.Context, agentID, itemID string) (*ShakeResult, *JournalBatch, error) {
	var result *ShakeResult
	batch, err := journalTx(ctx, s, agentID, "shake context",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			item, err := qtx.GetQueueItem(ctx, db.GetQueueItemParams{AgentID: agentID, ID: itemID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errItemNotStarted
			} else if err != nil {
				return nil, err
			}
			if item.State != "started" {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotStarted)
			}
			if item.Kind != itemKindShake {
				return nil, fmt.Errorf("shake context: item %s has kind %q", itemID, item.Kind)
			}
			threadID, err := qtx.GetActiveThreadID(ctx, agentID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrUnknownAgent
			} else if err != nil {
				return nil, fmt.Errorf("resolve active thread: %w", err)
			}
			rows, err := qtx.ListShakeEligibleToolRows(ctx, db.ListShakeEligibleToolRowsParams{
				AgentID:  agentID,
				ThreadID: threadID,
				MinBytes: shakeMinResultBytes,
			})
			if err != nil {
				return nil, fmt.Errorf("load shake candidates: %w", err)
			}
			// The active exchange is the thread's newest turn, whatever
			// rows it carries; an empty thread has nothing to protect.
			protected := ""
			if latest, err := qtx.GetThreadNewestTurn(ctx, db.GetThreadNewestTurnParams{
				AgentID: agentID, ThreadID: threadID,
			}); err == nil {
				protected = latest
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("resolve newest turn: %w", err)
			}
			elided := 0
			for _, row := range rows {
				if row.TurnID == protected {
					continue
				}
				var env ToolEnvelope
				if err := json.Unmarshal([]byte(row.Content), &env); err != nil {
					return nil, fmt.Errorf("decode tool envelope (row %s): %w", row.ID, err)
				}
				text := env.Result
				if env.Error != nil {
					text = *env.Error
				}
				if len(text) < shakeMinResultBytes {
					continue
				}
				n, err := qtx.MarkThreadRowElided(ctx, db.MarkThreadRowElidedParams{
					AgentID:  agentID,
					ThreadID: threadID,
					ID:       row.ID,
				})
				if err != nil {
					return nil, fmt.Errorf("mark row %s elided: %w", row.ID, err)
				}
				if n == 1 {
					elided++
				}
			}
			if n, err := qtx.SetQueueItemTerminal(ctx, db.SetQueueItemTerminalParams{
				State:      apitypes.ItemCompleted,
				FinishedMs: sql.NullInt64{Int64: stamp, Valid: true},
				AgentID:    agentID,
				ID:         itemID,
			}); err != nil {
				return nil, err
			} else if n != 1 {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotStarted)
			}
			result = &ShakeResult{Elided: elided}
			return s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{{
				Kind:      apitypes.LifecycleItemFinished,
				ItemID:    itemID,
				ItemKind:  item.Kind,
				ThreadID:  threadID,
				Outcome:   apitypes.ItemCompleted,
				CreatedAt: threadpage.ISOTime(stamp),
			}})
		})
	return result, batch, err
}

// ReadToolOutput restores one elided tool result: the exact model-facing
// text the row carried before elision, bounded on redelivery by the same
// delivered-output ceiling a live tool's outcome faces. The read is
// agent-scoped, so a reference from another agent's conversation — like
// every reference naming no elided row of this agent, empty included —
// reads as unknown.
func (s *TurnService) ReadToolOutput(ctx context.Context, agentID, reference string) (string, error) {
	content, err := s.queries.GetThreadToolRow(ctx, db.GetThreadToolRowParams{
		AgentID: agentID,
		ID:      reference,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", errUnknownReference
	} else if err != nil {
		return "", fmt.Errorf("read tool output: %w", err)
	}
	var env ToolEnvelope
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		return "", fmt.Errorf("decode tool envelope (row %s): %w", reference, err)
	}
	text := env.Result
	if env.Error != nil {
		text = *env.Error
	}
	return tooladmission.DeliveredOutcome(text), nil
}
