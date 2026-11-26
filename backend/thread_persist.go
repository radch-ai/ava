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

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

// ErrUnknownAgent is the pointer resolve's answer for an agent that
// does not exist — surfaced verbatim so callers can tell a missing
// agent from a storage failure.
var ErrUnknownAgent = errors.New("agent not found")

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
// rows; ToolCallID/ToolName on tool rows. ID is the stored row id —
// the reference an elision placeholder names. It is a Go-only type:
// the HTTP wire contract and its generated UI types do not carry it.
type AgentThreadMessage struct {
	ID         string              `json:"id,omitempty"`
	Role       apitypes.ThreadRole `json:"role"`
	Content    string              `json:"content"`
	ToolCalls  []AgentThreadCall   `json:"toolCalls,omitempty"`
	ToolCallID string              `json:"toolCallId,omitempty"`
	ToolName   string              `json:"toolName,omitempty"`
	// Elided marks a tool message whose Content is the placeholder
	// naming the stored row, not the original result. Go-only, like the
	// rest of this type: the wire contract never carries it.
	Elided bool `json:"elided,omitempty"`
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

// validateDraft rejects rows the replay projection would treat as
// corruption, so the write path can never poison a thread: a tool
// row's content must decode as an envelope naming its call (Result's
// string type enforces the JSON-string form), and an assistant call
// group must be well-formed JSON with unique, fully-named calls. Tool
// calls attached to any other role are rejected: replay only reads
// them off assistant rows.
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
		if m.Role != apitypes.ThreadRoleAssistant {
			return errors.New("tool calls on non-assistant row")
		}
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

// GetAgentThread reads the agent's active thread as model input,
// oldest first, projected from the raw rows ListActiveThreadRows
// returns — the naming family of GetAgentConfig. The display page
// reads one thread per request instead (ListThreadPageRows). Error
// rows are display-only and excluded. Each complete native
// assistant call group keeps its text, call ids, names, arguments, and
// declared order; its outcomes are emitted in that declared order. An
// interrupted or crashed group is projected to the calls with actual
// recorded outcomes — never a fabricated result, never a rerun. A
// row that fails to decode, an outcome with no originating call, or a
// duplicate id within one group is an error:
// the model must never see a silently corrupt history.
func (s *TurnService) GetAgentThread(ctx context.Context, agentID string) ([]AgentThreadMessage, error) {
	threadID, err := s.queries.GetActiveThreadID(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrUnknownAgent
		}
		return nil, fmt.Errorf("resolve active thread: %w", err)
	}
	rows, err := s.queries.ListActiveThreadRows(ctx, db.ListActiveThreadRowsParams{
		AgentID:  agentID,
		ThreadID: threadID,
	})
	if err != nil {
		return nil, fmt.Errorf("load thread rows: %w", err)
	}
	return projectThread(rows)
}

// threadGroup collects one assistant call group and its outcomes as
// the row walk encounters them.
type threadGroup struct {
	row     int                     // index of the assistant row within its turn
	content string                  // the assistant's accompanying text
	calls   []AgentThreadCall       // declared order
	outcome map[string]groupOutcome // call id -> outcome, projected
}

// groupOutcome is one tool row as the model sees it: the text the
// projection emits, whether that text is an elision placeholder, and
// the stored row id it came from.
type groupOutcome struct {
	text   string
	elided bool
	rowID  string
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
func projectThread(rows []db.ListActiveThreadRowsRow) ([]AgentThreadMessage, error) {
	msgs := make([]AgentThreadMessage, 0, len(rows))
	var groups []*threadGroup
	var pending []db.ListActiveThreadRowsRow // rows of the current turn

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
				outcome: make(map[string]groupOutcome, len(calls)),
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
func projectTurn(rows []db.ListActiveThreadRowsRow, groups []*threadGroup) ([]AgentThreadMessage, error) {
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
		// error always wins. An elided row keeps its payload on disk and
		// projects to the placeholder naming it.
		text := env.Result
		if env.Error != nil {
			text = *env.Error
		}
		elided := row.Elided != 0
		if elided {
			text = ElidedReference(row.ID, len(text))
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
		g.outcome[env.CallID] = groupOutcome{text: text, elided: elided, rowID: row.ID}
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
				ID:   row.ID,
				Role: apitypes.ThreadRoleAssistant, Content: g.content,
				ToolCalls: answered,
			})
			for _, c := range answered {
				outcome := g.outcome[c.ID]
				out = append(out, AgentThreadMessage{
					ID:   outcome.rowID,
					Role: apitypes.ThreadRoleTool, Content: outcome.text,
					ToolCallID: c.ID, ToolName: c.Name, Elided: outcome.elided,
				})
			}
			continue
		}
		switch apitypes.ThreadRole(row.Role) {
		case apitypes.ThreadRoleUser:
			out = append(out, AgentThreadMessage{ID: row.ID, Role: apitypes.ThreadRoleUser, Content: row.Content})
		case apitypes.ThreadRoleAssistant:
			if row.Content != "" {
				out = append(out, AgentThreadMessage{ID: row.ID, Role: apitypes.ThreadRoleAssistant, Content: row.Content})
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
