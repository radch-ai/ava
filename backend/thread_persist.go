package backend

// The gateway's thread sink: turns land here so a conversation
// survives restarts. One writer, strictly ordered — the read side
// pages by created_at (id only breaks exact ties, and minted ids are
// random), so equal timestamps could scramble a turn's roles.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

// AppendThreadMessages writes drafts to an agent's thread. A draft
// may carry its own id (terminal error rows name theirs on the
// wire); otherwise one is minted. Each row gets a millisecond
// timestamp, strictly rising within the call and across calls — the
// assistant row a turn appends after its user row always sorts after
// it. Unknown agents fail on the foreign key; the caller decides
// whether that matters.
func (s *TurnService) AppendThreadMessages(ctx context.Context, agentID string, msgs []apitypes.ThreadMessageDraft) error {
	if len(msgs) == 0 {
		return nil
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	now := time.Now().UnixMilli()
	if now <= s.lastPersistedMs {
		now = s.lastPersistedMs + 1
	}
	for i, m := range msgs {
		id := m.ID
		if id == "" {
			id = uuid.NewString()
		}
		if err := s.queries.InsertThreadMessage(ctx, db.InsertThreadMessageParams{
			ID:        id,
			AgentID:   agentID,
			Role:      string(m.Role),
			Content:   m.Content,
			CreatedAt: now + int64(i),
		}); err != nil {
			return fmt.Errorf("append thread message: %w", err)
		}
	}
	s.lastPersistedMs = now + int64(len(msgs)) - 1
	return nil
}
