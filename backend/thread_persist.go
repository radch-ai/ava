package backend

// The gateway's thread sink: turns land here so a conversation
// survives restarts. One writer, strictly ordered — the read side
// pages by created_at (id only breaks exact ties, and minted ids are
// random), so equal timestamps could scramble a turn's roles.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

var (
	persistMu      sync.Mutex
	persistQueries *db.Queries
	// lastPersistedMs is the newest timestamp this process handed out;
	// the next append never reuses or undercuts it.
	lastPersistedMs int64
)

// bindPersist arms AppendThreadMessages with the handle New already
// holds.
func bindPersist(queries *db.Queries) {
	persistQueries = queries
}

// AppendThreadMessages writes drafts to an agent's thread. It mints
// each row's id and a millisecond timestamp, strictly rising within
// the call and across calls — the assistant row a turn appends after
// its user row always sorts after it. Unknown agents fail on the
// foreign key; the caller decides whether that matters.
func AppendThreadMessages(ctx context.Context, agentID string, msgs []apitypes.ThreadMessageDraft) error {
	if len(msgs) == 0 {
		return nil
	}
	persistMu.Lock()
	defer persistMu.Unlock()

	now := time.Now().UnixMilli()
	if now <= lastPersistedMs {
		now = lastPersistedMs + 1
	}
	for i, m := range msgs {
		id := uuid.NewString()
		if err := persistQueries.InsertThreadMessage(ctx, db.InsertThreadMessageParams{
			ID:        id,
			AgentID:   agentID,
			Role:      string(m.Role),
			Content:   m.Content,
			CreatedAt: now + int64(i),
		}); err != nil {
			return fmt.Errorf("append thread message: %w", err)
		}
	}
	lastPersistedMs = now + int64(len(msgs)) - 1
	return nil
}
