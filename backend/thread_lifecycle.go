package backend

// Thread lifecycle: the agent's active-thread pointer is the only
// routing state; the conversation read/write paths resolve it
// themselves. A new thread is created and activated only at a
// settled turn boundary. There is no repair path — a pointer that
// cannot be read is an error.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/internal/db"
)

// ActivatedThread is the committed outcome of CreateAndActivateThread:
// everything the completion event needs, read after commit.
type ActivatedThread struct {
	ID         string
	PreviousID string // formerly active thread; empty only if the pointer read found none
	CreatedAt  int64  // DB-stamped ms
}

// CreateAndActivateThread inserts a fresh empty thread owned by
// agentID — linked to the formerly active thread as its display
// predecessor — and swaps the active pointer in one transaction. The
// new identity is returned only after commit. Caller precondition:
// the agent's preceding turn has fully settled — storage only, no
// model, interruption, config change, or event.
func (s *TurnService) CreateAndActivateThread(ctx context.Context, agentID string) (*ActivatedThread, error) {
	// Swaps and message batches share one writer, so a batch's pointer
	// resolve can never race a swap mid-transaction.
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	threadID := uuid.NewString()
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	qtx := s.queries.WithTx(tx)
	// The predecessor comes back from the insert itself: the thread
	// record names exactly the pointer the same statement read, and
	// the composite FK keeps it inside this agent.
	row, err := qtx.CreateThreadFromActive(ctx, db.CreateThreadFromActiveParams{
		ThreadID: threadID,
		AgentID:  agentID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return nil, fmt.Errorf("activate thread: unknown agent %s", agentID)
	} else if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("create thread: %w", err)
	}
	n, err := qtx.UpdateActiveThread(ctx, db.UpdateActiveThreadParams{
		ActiveThreadID: threadID,
		ID:             agentID,
	})
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("activate thread: %w", err)
	}
	if n == 0 {
		_ = tx.Rollback()
		return nil, fmt.Errorf("activate thread: unknown agent %s", agentID)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}
	return &ActivatedThread{
		ID:         threadID,
		PreviousID: row.PreviousThreadID.String,
		CreatedAt:  row.CreatedAt,
	}, nil
}
