package backend

// The gateway's thread sink: turns land here so a conversation
// survives restarts. One writer, strictly ordered — the read side walks
// by created_at (id only breaks exact ties, and minted ids are
// random), so equal timestamps could scramble a turn's roles.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

// AgentThreadCall is one model-authored call inside an assistant
// message's tool-call group. Arguments is the model's JSON text,
// stored and returned verbatim.
type AgentThreadCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AgentThreadMessage is one conversation record, shaped for
// the gateway's model-input mapping. ToolCalls is set on assistant
// rows; ToolCallID/ToolName on tool rows. It is a Go-only type: the
// HTTP wire contract and its generated UI types do not carry it.
type AgentThreadMessage struct {
	Role       apitypes.ThreadRole `json:"role"`
	Content    string              `json:"content"`
	ToolCalls  []AgentThreadCall   `json:"toolCalls,omitempty"`
	ToolCallID string              `json:"toolCallId,omitempty"`
	ToolName   string              `json:"toolName,omitempty"`
}

// ToolEnvelope is the content of a role='tool' row: one completed
// invocation, call and result in one record. Args stays raw JSON for
// display — its shape is the tool's, not the thread's. Result is the
// exact model-facing string: encoding/json quotes it once on the way
// in, replay reads it verbatim. CallID pairs the outcome with its
// originating assistant call; an empty CallID is corruption the write
// path rejects. It is a Go-only type; the wire contract and its
// generated UI types do not carry it.
type ToolEnvelope struct {
	Name   string          `json:"name"`
	CallID string          `json:"callId,omitempty"`
	Args   json.RawMessage `json:"args"`
	Result string          `json:"result"`
	Error  *string         `json:"error,omitempty"`
}

// ThreadMessageDraft is a message queued for the thread. An empty ID
// has the sink mint one; a supplied ID (terminal error rows, so the
// failure's event can name its stored row) is used verbatim. The
// timestamp stays the sink's — it owns ordering. TurnID groups the
// queue item's rows; ToolCalls is the assistant call group's JSON,
// nil for every other row.
type ThreadMessageDraft struct {
	ID        string
	Role      apitypes.ThreadRole
	Content   string
	TurnID    string
	ToolCalls []byte
}

// AppendThreadMessages writes drafts to an agent's thread in one
// transaction: either every row becomes visible or none does. A draft
// may carry its own id (terminal error rows name theirs on the wire);
// otherwise one is minted. Each row gets a millisecond timestamp,
// strictly rising within the call and across calls — the assistant row
// a turn appends after its user row always sorts after it. A rolled-back
// batch leaves nothing visible. Unknown agents fail on the
// foreign key; the caller decides whether that matters.
func (s *TurnService) AppendThreadMessages(ctx context.Context, agentID string, msgs []ThreadMessageDraft) error {
	if len(msgs) == 0 {
		return nil
	}
	// Structure is the read path's contract: a row the projection
	// would treat as corruption is rejected here, before any write.
	for i, m := range msgs {
		if err := validateDraft(m); err != nil {
			return fmt.Errorf("append thread message: draft %d: %w", i, err)
		}
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	now := time.Now().UnixMilli()
	if now <= s.lastPersistedMs {
		now = s.lastPersistedMs + 1
	}

	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("append thread message: %w", err)
	}
	qtx := s.queries.WithTx(tx)
	for i, m := range msgs {
		id := m.ID
		if id == "" {
			id = uuid.NewString()
		}
		var toolCalls sql.NullString
		if len(m.ToolCalls) > 0 {
			toolCalls = sql.NullString{String: string(m.ToolCalls), Valid: true}
		}
		if err := qtx.InsertThreadMessage(ctx, db.InsertThreadMessageParams{
			ID:        id,
			AgentID:   agentID,
			Role:      string(m.Role),
			Content:   m.Content,
			CreatedAt: now + int64(i),
			TurnID:    m.TurnID,
			ToolCalls: toolCalls,
		}); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("append thread message: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("append thread message: %w", err)
	}
	s.lastPersistedMs = now + int64(len(msgs)) - 1
	return nil
}

// validateDraft rejects rows the replay projection would treat as
// corruption, so the write path can never poison a thread: a tool
// row's content must decode as an envelope naming its call (Result's
// string type enforces the JSON-string form), and an assistant call
// group must be well-formed JSON with unique, fully-named calls.
func validateDraft(m ThreadMessageDraft) error {
	if m.Role == apitypes.ThreadRoleTool {
		var env ToolEnvelope
		if err := json.Unmarshal([]byte(m.Content), &env); err != nil {
			return fmt.Errorf("tool content: %w", err)
		}
		if env.CallID == "" {
			return errors.New("tool content: no call id")
		}
	}
	if len(m.ToolCalls) > 0 {
		var calls []AgentThreadCall
		if err := json.Unmarshal(m.ToolCalls, &calls); err != nil {
			return fmt.Errorf("tool calls: %w", err)
		}
		seen := make(map[string]struct{}, len(calls))
		for _, c := range calls {
			if c.ID == "" || c.Name == "" {
				return errors.New("tool calls: call without id or name")
			}
			if _, dup := seen[c.ID]; dup {
				return fmt.Errorf("tool calls: repeated call id %q", c.ID)
			}
			seen[c.ID] = struct{}{}
		}
	}
	return nil
}

// GetAgentThread reads the agent's conversation as model input,
// oldest first, projected from the raw rows ListThreadRows returns —
// the naming family of GetAgentConfig. Error rows are display-only
// and excluded. Each complete native
// assistant call group keeps its text, call ids, names, arguments, and
// declared order; its outcomes are emitted in that declared order. An
// interrupted or crashed group is projected to the calls with actual
// recorded outcomes — never a fabricated result, never a rerun. A
// row that fails to decode, an outcome with no originating call, or a
// duplicate id within one group is an error:
// the model must never see a silently corrupt history.
func (s *TurnService) GetAgentThread(ctx context.Context, agentID string) ([]AgentThreadMessage, error) {
	rows, err := s.queries.ListThreadRows(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("load thread rows: %w", err)
	}
	return projectThread(rows)
}

// threadGroup collects one assistant call group and its outcomes as
// the row walk encounters them.
type threadGroup struct {
	row     int               // index of the assistant row within its turn
	content string            // the assistant's accompanying text
	calls   []AgentThreadCall // declared order
	outcome map[string]string // call id -> model-facing result text
}

// projectThread turns raw rows into the model-facing thread history.
//
// Group pairing is positional within the originating turn: an outcome
// attaches to the latest preceding group that declared its call id.
// Call ids may repeat across groups (providers mint ids per request),
// so a turn-wide id map would conflate rounds. Groups are flushed in
// row order, each preceded only by its own rows; a group whose outcome
// rows arrive after a later row of the same turn still emits them in
// declared order, keeping the projection protocol-valid.
func projectThread(rows []db.ListThreadRowsRow) ([]AgentThreadMessage, error) {
	msgs := make([]AgentThreadMessage, 0, len(rows))
	var groups []*threadGroup
	var pending []db.ListThreadRowsRow // rows of the current turn

	flush := func() error {
		out, err := projectTurn(pending, groups)
		if err != nil {
			return err
		}
		msgs = append(msgs, out...)
		groups = groups[:0]
		pending = pending[:0]
		return nil
	}

	turn := ""
	for _, row := range rows {
		if apitypes.ThreadRole(row.Role) == apitypes.ThreadRoleError {
			continue // display-only: failures never feed the model
		}
		if row.TurnID != turn {
			if err := flush(); err != nil {
				return nil, err
			}
			turn = row.TurnID
		}
		if apitypes.ThreadRole(row.Role) == apitypes.ThreadRoleAssistant && row.ToolCalls.Valid {
			var calls []AgentThreadCall
			if err := json.Unmarshal([]byte(row.ToolCalls.String), &calls); err != nil {
				return nil, fmt.Errorf("decode assistant tool calls (row %s): %w", row.ID, err)
			}
			seen := make(map[string]struct{}, len(calls))
			for _, c := range calls {
				if c.ID == "" || c.Name == "" {
					return nil, fmt.Errorf("assistant row %s has a call without id or name", row.ID)
				}
				if _, dup := seen[c.ID]; dup {
					return nil, fmt.Errorf("assistant row %s repeats call id %q", row.ID, c.ID)
				}
				seen[c.ID] = struct{}{}
			}
			groups = append(groups, &threadGroup{
				row: len(pending), content: row.Content, calls: calls,
				outcome: make(map[string]string, len(calls)),
			})
		}
		pending = append(pending, row)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return msgs, nil
}

// projectTurn projects one turn's rows in order. Each assistant call
// group is emitted with the outcomes actually recorded for it, so a
// late-arriving outcome still lands beside its originating calls.
func projectTurn(rows []db.ListThreadRowsRow, groups []*threadGroup) ([]AgentThreadMessage, error) {
	out := make([]AgentThreadMessage, 0, len(rows))
	groupAt := make(map[int]*threadGroup, len(groups))
	for _, g := range groups {
		groupAt[g.row] = g
	}

	// First pass: decode every outcome and attach it to the latest
	// group of this turn that declared the id.
	for i, row := range rows {
		if apitypes.ThreadRole(row.Role) != apitypes.ThreadRoleTool {
			continue
		}
		var env ToolEnvelope
		if err := json.Unmarshal([]byte(row.Content), &env); err != nil {
			return nil, fmt.Errorf("decode tool envelope (row %s): %w", row.ID, err)
		}
		if env.CallID == "" {
			return nil, fmt.Errorf("thread tool row %s has no call id", row.ID)
		}
		// The envelope's Result is the exact model-facing string; an
		// error always wins.
		text := env.Result
		if env.Error != nil {
			text = *env.Error
		}
		target := -1
		for gi := len(groups) - 1; gi >= 0; gi-- {
			g := groups[gi]
			if g.row >= i {
				continue
			}
			if !declaresCall(g.calls, env.CallID) {
				continue
			}
			target = gi
			break
		}
		if target < 0 {
			return nil, fmt.Errorf("thread tool row %s pairs with no call group", row.ID)
		}
		g := groups[target]
		if _, dup := g.outcome[env.CallID]; dup {
			return nil, fmt.Errorf("thread tool row %s repeats call %q", row.ID, env.CallID)
		}
		g.outcome[env.CallID] = text
	}

	// Second pass: emit rows in order, each group followed by its
	// answered calls in declared order. The tool rows themselves are
	// emitted from the group, never inline, so late outcomes stay
	// adjacent to their calls.
	for i, row := range rows {
		if g, ok := groupAt[i]; ok {
			answered := make([]AgentThreadCall, 0, len(g.calls))
			for _, c := range g.calls {
				if _, ok := g.outcome[c.ID]; ok {
					answered = append(answered, c)
				}
			}
			if len(answered) == 0 && g.content == "" {
				continue // structure-only; display rows carry it
			}
			out = append(out, AgentThreadMessage{
				Role: apitypes.ThreadRoleAssistant, Content: g.content,
				ToolCalls: answered,
			})
			for _, c := range answered {
				out = append(out, AgentThreadMessage{
					Role: apitypes.ThreadRoleTool, Content: g.outcome[c.ID],
					ToolCallID: c.ID, ToolName: c.Name,
				})
			}
			continue
		}
		switch apitypes.ThreadRole(row.Role) {
		case apitypes.ThreadRoleUser:
			out = append(out, AgentThreadMessage{Role: apitypes.ThreadRoleUser, Content: row.Content})
		case apitypes.ThreadRoleAssistant:
			if row.Content != "" {
				out = append(out, AgentThreadMessage{Role: apitypes.ThreadRoleAssistant, Content: row.Content})
			}
		case apitypes.ThreadRoleTool:
			// emitted from its group above
		}
	}
	return out, nil
}

// declaresCall reports whether a group's declared call group contains
// the id.
func declaresCall(calls []AgentThreadCall, id string) bool {
	for _, c := range calls {
		if c.ID == id {
			return true
		}
	}
	return false
}
