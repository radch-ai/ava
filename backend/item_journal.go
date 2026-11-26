package backend

// The lifecycle journal: durable item bookkeeping plus the ordered feed
// every subscribed device replays. The message and thread tables stay
// the conversation's source of truth; this file records which admitted
// item produced which committed rows, in which order, so a device that
// missed frames — or missed a whole process lifetime — is told what
// happened instead of inferring it.
//
// Every mutator here owns one transaction: the conversation rows, the
// item transition, the journal append, and the coordinates update commit
// together or not at all. Item identity is stable from admission
// (`AcceptItem`) through settlement (`FinishItem`), and the transition
// guards live in the SQL predicates, so a second terminal write is a
// no-op by construction and recovery is idempotent.
//
// Publication is the gateway's half: these methods return the events
// they committed, in position order, and the gateway enqueues them after
// the commit.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/threadpage"
)

// Cursor shape: a version tag, the database's lifecycle-history
// identity, and the last applied journal position. Clients treat it as
// opaque; the gateway validates shape, history, and retention bounds.
const cursorVersion = "v1"

// FormatCursor renders the opaque cursor for one history position.
func FormatCursor(historyID string, pos int64) string {
	return fmt.Sprintf("%s:%s:%d", cursorVersion, historyID, pos)
}

// ParseCursor checks a cursor's shape and returns its two coordinates.
// It says nothing about whether this database retains the position —
// ReplayAfter owns that check.
func ParseCursor(raw string) (historyID string, pos int64, ok bool) {
	rest, found := strings.CutPrefix(raw, cursorVersion+":")
	if !found {
		return "", 0, false
	}
	historyID, posText, found := strings.Cut(rest, ":")
	if !found || historyID == "" {
		return "", 0, false
	}
	pos, err := strconv.ParseInt(posText, 10, 64)
	if err != nil || pos < 0 {
		return "", 0, false
	}
	return historyID, pos, true
}

// Replay classifications: a cursor this database can never resume from.
// All three send the client to the snapshot path.
var (
	ErrReplayExpired = errors.New("cursor predates retained history")
	ErrReplayAhead   = errors.New("cursor is ahead of committed history")
	ErrReplayForeign = errors.New("cursor belongs to another journal history")
)

// Item state-machine violations: the caller asked for a transition the
// item's recorded state does not allow.
var (
	errItemNotAccepted   = errors.New("item is not accepted")
	errItemNotStarted    = errors.New("item is not started")
	errInvocationNotOpen = errors.New("tool invocation is not open")
)

// Retention knobs. Package variables so tests can shrink them; the
// production budget is 10,000 recent journal entries per agent, pruned
// after every 100 appended events.
var (
	journalRetainPerAgent int64 = 10_000
	journalPruneEvery           = 100
)

// maxReplayPageRows bounds one replay read. Real batches hold one or two
// events and the replay limit is a hundred, so a page that grows past this
// without meeting a batch boundary means the journal's batch markers are
// corrupt — fail instead of extending forever.
const maxReplayPageRows = 1000

// Admission kinds, as stored in queue_items.kind.
const (
	itemKindPrompt    = "prompt"
	itemKindNewThread = "new_thread"
	itemKindShake     = "shake"
	itemKindCompact   = "compact"
)

// threadKindNew is the threads.kind value a plain /new boundary writes;
// a compaction successor writes threadpage.CompactedKind instead.
const threadKindNew = "new"

// restartReason explains every outcome recovery assigns.
const restartReason = "service_restart"

// interruptedNotice is the display row a started item gets when the
// service died mid-turn: the outcome is unknown, so nothing is
// fabricated beyond the notice itself.
const interruptedNotice = "interrupted by service restart"

// JournalBatch is one committed transaction's lifecycle events, in
// position order, with the cursor a client checkpoints after applying
// all of them.
type JournalBatch struct {
	Cursor string
	Events []apitypes.LifecycleEvent
}

// ItemStart is what starting an item committed: for a prompt, the user
// row's id ("" for commands).
type ItemStart struct {
	UserMessageID string
}

// JournalPage is one bounded replay answer: complete transaction batches
// after the requested position, payloads verbatim from the journal — the
// same bytes that were committed. A client asks again from LastPos, the
// page's final position; the page itself does not promise what remains.
type JournalPage struct {
	Events  []json.RawMessage
	LastPos int64
}

// SyncSnapshot is the authoritative baseline a fresh or unusable cursor
// recovers from: the cursor at the committed head and the active
// thread's newest display page, read in one transaction.
type SyncSnapshot struct {
	Cursor string
	Page   *apitypes.ThreadPage
}

// nextStamp mints a row timestamp strictly above every row written so
// far. Caller holds persistMu.
func (s *TurnService) nextStamp() int64 {
	now := time.Now().UnixMilli()
	if now <= s.lastPersistedMs {
		now = s.lastPersistedMs + 1
	}
	return now
}

// commitTx commits the caller's transaction and, on a failed commit that
// the store nevertheless applied, resyncs the writer's watermark from
// storage so later batches never mint timestamps at or below committed
// rows.
func (s *TurnService) commitTx(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		if max, rerr := s.queries.MaxThreadCreatedAt(context.Background()); rerr == nil && max > s.lastPersistedMs {
			s.lastPersistedMs = max
		}
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// journalTx runs one journaling transaction: it owns the writer lock,
// the stamp, the transaction, rollback on error, the ambiguous-commit
// readback, and retention accounting. body performs the domain writes
// and returns the batch appendEvents committed — or nil to abandon the
// transaction as a no-op (an idempotent repeat). It may advance
// s.lastPersistedMs directly: a watermark ahead of a rolled-back commit
// is harmless, because stamps must rise, not stay dense.
func journalTx(ctx context.Context, s *TurnService, agentID, label string,
	body func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error)) (*JournalBatch, error) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	stamp := s.nextStamp()
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.queries.WithTx(tx)
	preHead, _, err := s.agentCoords(ctx, qtx, agentID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	batch, err := body(qtx, stamp, preHead)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if batch == nil {
		// Nothing committed: the deferred rollback discards the
		// transaction and the caller sees the no-op it asked for.
		return nil, nil
	}
	if err := s.commitTx(tx); err != nil {
		// An ambiguous commit is re-read before classification: events
		// past the pre-call head mean the transaction applied, so the
		// result is rebuilt from the recorded state and reported as the
		// success it became.
		applied := s.readbackBatch(ctx, agentID, preHead)
		if applied == nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		batch = applied
	}

	s.afterJournalCommit(ctx, agentID, len(batch.Events))
	return batch, nil
}

// rawJSONValue keeps JSON verbatim and quotes anything else — a tool's
// plain-text answer stays a valid envelope member (display only; the
// model-facing result is always encoded as a JSON string).
func rawJSONValue(s string) []byte {
	if json.Valid([]byte(s)) {
		return []byte(s)
	}
	encoded, _ := json.Marshal(s)
	return encoded
}

// insertRows writes drafts into one thread inside the caller's
// transaction: either every row becomes visible or none does. Empty ids
// are minted here; a supplied id is the caller's claim about a stored row
// and is used verbatim. Each row gets a strictly rising millisecond
// timestamp from the batch's stamp, so a turn's rows always sort in the
// order they were written. The committed rows are returned in display
// shape, the identity events carry.
func (s *TurnService) insertRows(ctx context.Context, qtx *db.Queries, agentID, threadID string, stampMs int64, drafts []ThreadMessageDraft) ([]apitypes.ThreadMessage, error) {
	// Structure is the read path's contract: a row the projection would
	// treat as corruption is rejected here, before any write.
	seen := make(map[string]struct{}, len(drafts))
	rows := make([]apitypes.ThreadMessage, 0, len(drafts))
	for i, m := range drafts {
		if err := validateDraft(m); err != nil {
			return nil, fmt.Errorf("draft %d: %w", i, err)
		}
		id := m.ID
		if id == "" {
			id = uuid.NewString()
		} else if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("draft %d: duplicate row id %q", i, id)
		}
		seen[id] = struct{}{}
		var toolCalls sql.NullString
		if len(m.ToolCalls) > 0 {
			toolCalls = sql.NullString{String: string(m.ToolCalls), Valid: true}
		}
		createdMs := stampMs + int64(i)
		if err := qtx.InsertThreadMessage(ctx, db.InsertThreadMessageParams{
			ID:        id,
			AgentID:   agentID,
			Role:      string(m.Role),
			Content:   m.Content,
			CreatedAt: createdMs,
			TurnID:    m.TurnID,
			ToolCalls: toolCalls,
			ThreadID:  threadID,
		}); err != nil {
			return nil, fmt.Errorf("insert row %d: %w", i, err)
		}
		rows = append(rows, apitypes.ThreadMessage{
			ID:        id,
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: threadpage.ISOTime(createdMs),
		})
	}
	return rows, nil
}

// appendEvents writes one transaction's lifecycle events: positions are
// allocated from the agent's committed head, the payload is the fully
// hydrated wire event, and the last event carries the batch boundary.
// Caller holds persistMu and runs the same transaction.
func (s *TurnService) appendEvents(ctx context.Context, qtx *db.Queries, agentID, itemID string, head, createdMs int64, events []apitypes.LifecycleEvent) (*JournalBatch, error) {
	pos := head
	for i := range events {
		pos++
		events[i].Pos = pos
		events[i].BatchEnd = i == len(events)-1
		payload, err := json.Marshal(events[i])
		if err != nil {
			return nil, fmt.Errorf("marshal event %d: %w", i, err)
		}
		var outcome sql.NullString
		if events[i].Outcome != "" {
			outcome = sql.NullString{String: events[i].Outcome, Valid: true}
		}
		if err := qtx.InsertLifecycleEvent(ctx, db.InsertLifecycleEventParams{
			AgentID:   agentID,
			Pos:       pos,
			ItemID:    itemID,
			Kind:      events[i].Kind,
			BatchEnd:  boolInt(events[i].BatchEnd),
			Payload:   string(payload),
			CreatedMs: createdMs,
			Outcome:   outcome,
		}); err != nil {
			return nil, fmt.Errorf("insert event %d: %w", i, err)
		}
	}
	if err := qtx.ReplaceAgentSyncHead(ctx, db.ReplaceAgentSyncHeadParams{
		HeadPos: pos,
		AgentID: agentID,
	}); err != nil {
		return nil, fmt.Errorf("advance head: %w", err)
	}
	return &JournalBatch{Cursor: FormatCursor(s.historyID, pos), Events: events}, nil
}

// boolInt renders a boolean for the journal's 0/1 column.
func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// afterJournalCommit counts appended events toward retention and, at the
// threshold, prunes this agent's journal. Caller holds persistMu; a
// pruning failure is maintenance noise, never the caller's failure.
func (s *TurnService) afterJournalCommit(ctx context.Context, agentID string, events int) {
	if s.appendCounts == nil {
		s.appendCounts = make(map[string]int)
	}
	s.appendCounts[agentID] += events
	if s.appendCounts[agentID] < journalPruneEvery {
		return
	}
	s.appendCounts[agentID] = 0
	if err := s.pruneJournalLocked(ctx, agentID); err != nil {
		s.logger.Warn().Err(err).Str("agent_id", agentID).
			Msg("prune lifecycle journal")
	}
}

// AcceptItem records one admitted submission under its stable item id and
// journals the acceptance. Admission is durable before the gateway
// exposes the item, so a crash here leaves a known item recovery cancels.
func (s *TurnService) AcceptItem(ctx context.Context, agentID, itemID, content, kind string, model *apitypes.ModelEntry) (*JournalBatch, error) {
	switch kind {
	case itemKindPrompt, itemKindNewThread, itemKindShake, itemKindCompact:
	default:
		return nil, fmt.Errorf("accept item: unknown kind %q", kind)
	}
	var modelJSON sql.NullString
	if model != nil {
		encoded, err := json.Marshal(model)
		if err != nil {
			return nil, fmt.Errorf("accept item: marshal model: %w", err)
		}
		modelJSON = sql.NullString{String: string(encoded), Valid: true}
	}

	batch, err := journalTx(ctx, s, agentID, "accept item",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			if err := qtx.InsertQueueItem(ctx, db.InsertQueueItemParams{
				ID:         itemID,
				AgentID:    agentID,
				Kind:       kind,
				Content:    content,
				Model:      modelJSON,
				AcceptedMs: stamp,
			}); err != nil {
				return nil, err
			}
			return s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{{
				Kind:      apitypes.LifecycleItemAccepted,
				ItemID:    itemID,
				ItemKind:  kind,
				Content:   content,
				Model:     model,
				CreatedAt: threadpage.ISOTime(stamp),
			}})
		})
	return batch, err
}

// StartItem moves an accepted item to execution: the execution thread is
// resolved, a prompt's user row commits with the transition, and
// item_started is journaled. A command records the pre-switch thread and
// no row. The caller publishes the batch before doing model or tool work.
func (s *TurnService) StartItem(ctx context.Context, agentID, itemID string) (*ItemStart, *JournalBatch, error) {
	var start *ItemStart
	batch, err := journalTx(ctx, s, agentID, "start item",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			item, err := qtx.GetQueueItem(ctx, db.GetQueueItemParams{AgentID: agentID, ID: itemID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errItemNotAccepted
			} else if err != nil {
				return nil, err
			}
			if item.State != "accepted" {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotAccepted)
			}
			threadID, err := qtx.GetActiveThreadID(ctx, agentID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrUnknownAgent
			} else if err != nil {
				return nil, fmt.Errorf("resolve active thread: %w", err)
			}

			start = &ItemStart{}
			event := apitypes.LifecycleEvent{
				Kind:      apitypes.LifecycleItemStarted,
				ItemID:    itemID,
				ItemKind:  item.Kind,
				ThreadID:  threadID,
				CreatedAt: threadpage.ISOTime(stamp),
			}
			if item.Kind == itemKindPrompt {
				userMsgID := uuid.NewString()
				rows, err := s.insertRows(ctx, qtx, agentID, threadID, stamp, []ThreadMessageDraft{{
					ID:      userMsgID,
					Role:    apitypes.ThreadRoleUser,
					Content: item.Content,
					TurnID:  itemID,
				}})
				if err != nil {
					return nil, fmt.Errorf("user row: %w", err)
				}
				start.UserMessageID = userMsgID
				event.UserMessageID = userMsgID
				event.Rows = rows
			}
			if n, err := qtx.SetQueueItemStarted(ctx, db.SetQueueItemStartedParams{
				ThreadID:      sql.NullString{String: threadID, Valid: true},
				UserMessageID: sql.NullString{String: start.UserMessageID, Valid: start.UserMessageID != ""},
				StartedMs:     sql.NullInt64{Int64: stamp, Valid: true},
				AgentID:       agentID,
				ID:            itemID,
			}); err != nil {
				return nil, err
			} else if n != 1 {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotAccepted)
			}
			batch, err := s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{event})
			if err != nil {
				return nil, err
			}
			if start.UserMessageID != "" {
				s.lastPersistedMs = stamp
			}
			return batch, nil
		})
	return start, batch, err
}

// RecordToolStarted records one invocation's intent, keyed by the
// server-minted invocation id: what was authorized and where. It is
// intent, not proof an external effect happened.
func (s *TurnService) RecordToolStarted(ctx context.Context, agentID, itemID, invocationID, callID, name, args string) (*JournalBatch, error) {
	batch, err := journalTx(ctx, s, agentID, "record tool start",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			item, err := qtx.GetQueueItem(ctx, db.GetQueueItemParams{AgentID: agentID, ID: itemID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errItemNotStarted
			} else if err != nil {
				return nil, err
			}
			if item.State != "started" || !item.ThreadID.Valid {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotStarted)
			}
			if err := qtx.InsertToolInvocation(ctx, db.InsertToolInvocationParams{
				ID:        invocationID,
				AgentID:   agentID,
				ItemID:    itemID,
				CallID:    callID,
				Name:      name,
				Args:      args,
				StartedMs: stamp,
			}); err != nil {
				return nil, err
			}
			return s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{{
				Kind:         apitypes.LifecycleToolStarted,
				ItemID:       itemID,
				ItemKind:     item.Kind,
				ThreadID:     item.ThreadID.String,
				InvocationID: invocationID,
				CallID:       callID,
				ToolName:     name,
				ToolArgs:     args,
				CreatedAt:    threadpage.ISOTime(stamp),
			}})
		})
	return batch, err
}

// RecordToolFinished commits one invocation's actual outcome as a tool
// row in the item's execution thread and journals the result. The
// envelope marshal lives here — the gateway hands over the raw strings.
func (s *TurnService) RecordToolFinished(ctx context.Context, agentID, itemID, invocationID, result string, errText *string) (*JournalBatch, error) {
	batch, err := journalTx(ctx, s, agentID, "record tool finish",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			invocation, err := qtx.GetToolInvocation(ctx, db.GetToolInvocationParams{
				AgentID: agentID, ItemID: itemID, ID: invocationID,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errInvocationNotOpen
			} else if err != nil {
				return nil, err
			}
			if invocation.State != "open" {
				return nil, fmt.Errorf("%s: %w", invocationID, errInvocationNotOpen)
			}
			item, err := qtx.GetQueueItem(ctx, db.GetQueueItemParams{AgentID: agentID, ID: itemID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errItemNotStarted
			} else if err != nil {
				return nil, err
			}
			if item.State != "started" || !item.ThreadID.Valid {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotStarted)
			}
			// The envelope's Result is the exact model-facing string;
			// encoding/json quotes it once, so replay reads it verbatim.
			envelope, err := json.Marshal(ToolEnvelope{
				Name:   invocation.Name,
				CallID: invocation.CallID,
				Args:   rawJSONValue(invocation.Args),
				Result: result,
				Error:  errText,
			})
			if err != nil {
				return nil, fmt.Errorf("marshal tool envelope: %w", err)
			}
			rows, err := s.insertRows(ctx, qtx, agentID, item.ThreadID.String, stamp, []ThreadMessageDraft{{
				Role:    apitypes.ThreadRoleTool,
				Content: string(envelope),
				TurnID:  itemID,
			}})
			if err != nil {
				return nil, err
			}
			if n, err := qtx.FinishToolInvocation(ctx, db.FinishToolInvocationParams{
				AgentID: agentID, ItemID: itemID, ID: invocationID,
			}); err != nil {
				return nil, err
			} else if n != 1 {
				return nil, fmt.Errorf("%s: %w", invocationID, errInvocationNotOpen)
			}
			batch, err := s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{{
				Kind:         apitypes.LifecycleToolFinished,
				ItemID:       itemID,
				ItemKind:     item.Kind,
				ThreadID:     item.ThreadID.String,
				InvocationID: invocationID,
				CallID:       invocation.CallID,
				ToolName:     invocation.Name,
				ToolResult:   result,
				ToolError:    errText,
				Rows:         rows,
				CreatedAt:    threadpage.ISOTime(stamp),
			}})
			if err != nil {
				return nil, err
			}
			s.lastPersistedMs = stamp + int64(len(rows)) - 1
			return batch, nil
		})
	return batch, err
}

// AppendItemMessages commits a started item's nonterminal rows — the
// assistant call groups a turn writes mid-run — in the item's execution
// thread, journaling them as one message_committed event. The row
// identities ride the event, so no second record describes the same row.
func (s *TurnService) AppendItemMessages(ctx context.Context, agentID, itemID string, msgs []ThreadMessageDraft) (*JournalBatch, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	batch, err := journalTx(ctx, s, agentID, "append item messages",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			item, err := qtx.GetQueueItem(ctx, db.GetQueueItemParams{AgentID: agentID, ID: itemID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errItemNotStarted
			} else if err != nil {
				return nil, err
			}
			if item.State != "started" || !item.ThreadID.Valid {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotStarted)
			}
			rows, err := s.insertRows(ctx, qtx, agentID, item.ThreadID.String, stamp, msgs)
			if err != nil {
				return nil, err
			}
			batch, err := s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{{
				Kind:      apitypes.LifecycleMessageCommitted,
				ItemID:    itemID,
				ItemKind:  item.Kind,
				ThreadID:  item.ThreadID.String,
				Rows:      rows,
				CreatedAt: threadpage.ISOTime(stamp),
			}})
			if err != nil {
				return nil, err
			}
			s.lastPersistedMs = stamp + int64(len(rows)) - 1
			return batch, nil
		})
	return batch, err
}

// FinishItem settles a started item: its final rows (an answer, a
// persisted error, or nothing for a quiet stop) commit in the item's
// execution thread and the item reaches exactly one terminal outcome.
func (s *TurnService) FinishItem(ctx context.Context, agentID, itemID, outcome, reason string, msgs []ThreadMessageDraft) (*JournalBatch, error) {
	switch outcome {
	case apitypes.ItemCompleted, apitypes.ItemFailed, apitypes.ItemStopped,
		apitypes.ItemCancelled, apitypes.ItemInterrupted:
	default:
		return nil, fmt.Errorf("finish item: unknown outcome %q", outcome)
	}
	batch, err := journalTx(ctx, s, agentID, "finish item",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			item, err := qtx.GetQueueItem(ctx, db.GetQueueItemParams{AgentID: agentID, ID: itemID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errItemNotStarted
			} else if err != nil {
				return nil, err
			}
			if item.State != "started" || !item.ThreadID.Valid {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotStarted)
			}
			rows, err := s.insertRows(ctx, qtx, agentID, item.ThreadID.String, stamp, msgs)
			if err != nil {
				return nil, err
			}
			if n, err := qtx.SetQueueItemTerminal(ctx, db.SetQueueItemTerminalParams{
				State:      outcome,
				FinishedMs: sql.NullInt64{Int64: stamp, Valid: true},
				Reason:     sql.NullString{String: reason, Valid: reason != ""},
				AgentID:    agentID,
				ID:         itemID,
			}); err != nil {
				return nil, err
			} else if n != 1 {
				return nil, fmt.Errorf("%s: %w", itemID, errItemNotStarted)
			}
			// A terminal outcome closes every invocation the item still
			// had open: the turn is over, so nothing finishes them later.
			if _, err := qtx.InterruptOpenToolInvocations(ctx, db.InterruptOpenToolInvocationsParams{
				AgentID: agentID,
				ItemID:  itemID,
			}); err != nil {
				return nil, err
			}
			batch, err := s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{{
				Kind:      apitypes.LifecycleItemFinished,
				ItemID:    itemID,
				ItemKind:  item.Kind,
				ThreadID:  item.ThreadID.String,
				Rows:      rows,
				Outcome:   outcome,
				Reason:    reason,
				CreatedAt: threadpage.ISOTime(stamp),
			}})
			if err != nil {
				return nil, err
			}
			if len(rows) > 0 {
				s.lastPersistedMs = stamp + int64(len(rows)) - 1
			}
			return batch, nil
		})
	return batch, err
}

// ActivatedThread is the committed outcome of ExecuteThreadSwitch:
// the identity the display record keys on, carried by the batch's
// thread_changed event.
type ActivatedThread struct {
	ID         string
	PreviousID string // formerly active thread; empty only if the pointer read found none
}

// ExecuteThreadSwitch runs a command item's durable thread switch: a fresh
// thread linked to the formerly active one, the pointer swap, the item's
// completion, and a two-event batch — thread_changed then
// item_finished(completed) — so a committed switch can never be
// misclassified after a restart. The new identity is returned only after
// commit.
func (s *TurnService) ExecuteThreadSwitch(ctx context.Context, agentID, itemID string) (*ActivatedThread, *JournalBatch, error) {
	var activated *ActivatedThread
	batch, err := journalTx(ctx, s, agentID, "switch thread",
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
			threadID := uuid.NewString()
			// The predecessor comes back from the insert itself: the
			// thread record names exactly the pointer the same statement
			// read, and the composite FK keeps it inside this agent.
			row, err := qtx.CreateThreadFromActive(ctx, db.CreateThreadFromActiveParams{
				ThreadID:  threadID,
				CreatedMs: stamp,
				AgentID:   agentID,
				Kind:      threadKindNew,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrUnknownAgent
			} else if err != nil {
				return nil, err
			}
			if n, err := qtx.UpdateActiveThread(ctx, db.UpdateActiveThreadParams{
				ActiveThreadID: threadID,
				ID:             agentID,
			}); err != nil {
				return nil, err
			} else if n == 0 {
				return nil, ErrUnknownAgent
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
			createdAt := threadpage.ISOTime(row.CreatedAt)
			divider := threadpage.Divider(threadID, row.CreatedAt, threadpage.DividerText)
			// The divider carries this transaction's stamp, so the
			// watermark must cover it: a later batch may not mint a
			// timestamp at or below rows this one wrote.
			s.lastPersistedMs = stamp
			return s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{
				{
					Kind:             apitypes.LifecycleThreadChanged,
					ItemID:           itemID,
					ItemKind:         item.Kind,
					ThreadID:         threadID,
					PreviousThreadID: row.PreviousThreadID.String,
					Divider:          &divider,
					CreatedAt:        createdAt,
				},
				{
					Kind:      apitypes.LifecycleItemFinished,
					ItemID:    itemID,
					ItemKind:  item.Kind,
					ThreadID:  threadID,
					Outcome:   apitypes.ItemCompleted,
					CreatedAt: createdAt,
				},
			})
		})
	if err != nil {
		return nil, batch, err
	}
	if batch != nil {
		for i := range batch.Events {
			if ev := batch.Events[i]; ev.Kind == apitypes.LifecycleThreadChanged {
				activated = &ActivatedThread{
					ID:         ev.ThreadID,
					PreviousID: ev.PreviousThreadID,
				}
				break
			}
		}
	}
	return activated, batch, nil
}

// ExecuteCompaction runs a compact item's durable rotation: a successor
// thread linked to the formerly active one, the summary seeded as its
// first assistant message, the pointer swap, and the item's completion —
// one transaction, so a half-seeded successor can never be observed.
// The two-event batch mirrors a thread switch, with the seeded row
// riding item_finished so live and replayed clients render the summary
// in the new segment. The summarization itself is the gateway's model
// call; this is only its committed effect.
func (s *TurnService) ExecuteCompaction(ctx context.Context, agentID, itemID, summary string) (*ActivatedThread, *JournalBatch, error) {
	var activated *ActivatedThread
	batch, err := journalTx(ctx, s, agentID, "compact thread",
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
			if item.Kind != itemKindCompact {
				return nil, fmt.Errorf("compact thread: item %s has kind %q", itemID, item.Kind)
			}
			if summary == "" {
				return nil, errors.New("compact thread: empty summary")
			}
			threadID := uuid.NewString()
			row, err := qtx.CreateThreadFromActive(ctx, db.CreateThreadFromActiveParams{
				ThreadID:  threadID,
				CreatedMs: stamp,
				AgentID:   agentID,
				Kind:      threadpage.CompactedKind,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrUnknownAgent
			} else if err != nil {
				return nil, err
			}
			// The seeded row carries the item's id as its turn: the
			// convention every row an item mints follows, and a lone
			// assistant row projects as a plain message.
			rows, err := s.insertRows(ctx, qtx, agentID, threadID, stamp, []ThreadMessageDraft{{
				ID:      uuid.NewString(),
				Role:    apitypes.ThreadRoleAssistant,
				Content: summary,
				TurnID:  itemID,
			}})
			if err != nil {
				return nil, err
			}
			if n, err := qtx.UpdateActiveThread(ctx, db.UpdateActiveThreadParams{
				ActiveThreadID: threadID,
				ID:             agentID,
			}); err != nil {
				return nil, err
			} else if n == 0 {
				return nil, ErrUnknownAgent
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
			createdAt := threadpage.ISOTime(row.CreatedAt)
			divider := threadpage.Divider(threadID, row.CreatedAt, threadpage.CompactedText)
			// The divider and the seeded row share this transaction's
			// stamp span; the watermark covers both, as FinishItem's
			// row-carrying batches do.
			s.lastPersistedMs = stamp + int64(len(rows)) - 1
			return s.appendEvents(ctx, qtx, agentID, itemID, head, stamp, []apitypes.LifecycleEvent{
				{
					Kind:             apitypes.LifecycleThreadChanged,
					ItemID:           itemID,
					ItemKind:         item.Kind,
					ThreadID:         threadID,
					PreviousThreadID: row.PreviousThreadID.String,
					Divider:          &divider,
					CreatedAt:        createdAt,
				},
				{
					Kind:      apitypes.LifecycleItemFinished,
					ItemID:    itemID,
					ItemKind:  item.Kind,
					ThreadID:  threadID,
					Rows:      rows,
					Outcome:   apitypes.ItemCompleted,
					CreatedAt: createdAt,
				},
			})
		})
	if err != nil {
		return nil, batch, err
	}
	if batch != nil {
		for i := range batch.Events {
			if ev := batch.Events[i]; ev.Kind == apitypes.LifecycleThreadChanged {
				activated = &ActivatedThread{
					ID:         ev.ThreadID,
					PreviousID: ev.PreviousThreadID,
				}
				break
			}
		}
	}
	return activated, batch, nil
}

// RotateThreadCompacted is the pre-prompt automation's rotation: the
// committed half of ExecuteCompaction without its item settlement —
// the tagged prompt item is accepted, not started, and settles when
// its own turn finishes. tagItemID must name an accepted queue item
// of this agent (the journal's item FK requires it).
func (s *TurnService) RotateThreadCompacted(ctx context.Context, agentID, tagItemID, summary string) (*ActivatedThread, *JournalBatch, error) {
	var activated *ActivatedThread
	batch, err := journalTx(ctx, s, agentID, "rotate thread compacted",
		func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
			if summary == "" {
				return nil, errors.New("rotate thread compacted: empty summary")
			}
			// The tag names the prompt the checker measured — still
			// accepted, because the rotation runs before its claim.
			// Enforced, not assumed: every sibling mutation re-reads
			// its item, and the journal's FK proves only existence.
			item, err := qtx.GetQueueItem(ctx, db.GetQueueItemParams{AgentID: agentID, ID: tagItemID})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errItemNotAccepted
			} else if err != nil {
				return nil, err
			}
			if item.State != "accepted" {
				return nil, fmt.Errorf("%s: %w", tagItemID, errItemNotAccepted)
			}
			if item.Kind != itemKindPrompt {
				return nil, fmt.Errorf("rotate thread compacted: item %s has kind %q", tagItemID, item.Kind)
			}
			threadID := uuid.NewString()
			row, err := qtx.CreateThreadFromActive(ctx, db.CreateThreadFromActiveParams{
				ThreadID:  threadID,
				CreatedMs: stamp,
				AgentID:   agentID,
				Kind:      threadpage.CompactedKind,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrUnknownAgent
			} else if err != nil {
				return nil, err
			}
			// The seeded row carries the tagged item's id as its turn:
			// the convention every row an item mints follows, and a
			// lone assistant row projects as a plain message.
			rows, err := s.insertRows(ctx, qtx, agentID, threadID, stamp, []ThreadMessageDraft{{
				ID:      uuid.NewString(),
				Role:    apitypes.ThreadRoleAssistant,
				Content: summary,
				TurnID:  tagItemID,
			}})
			if err != nil {
				return nil, err
			}
			if n, err := qtx.UpdateActiveThread(ctx, db.UpdateActiveThreadParams{
				ActiveThreadID: threadID,
				ID:             agentID,
			}); err != nil {
				return nil, err
			} else if n == 0 {
				return nil, ErrUnknownAgent
			}
			createdAt := threadpage.ISOTime(row.CreatedAt)
			divider := threadpage.Divider(threadID, row.CreatedAt, threadpage.CompactedText)
			// The divider and the seeded row share this transaction's
			// stamp span; the watermark covers both, as FinishItem's
			// row-carrying batches do. The seeded row rides the
			// thread_changed event itself — there is no item_finished
			// to carry it, so live and replayed clients render the
			// summary in the new segment.
			s.lastPersistedMs = stamp + int64(len(rows)) - 1
			return s.appendEvents(ctx, qtx, agentID, tagItemID, head, stamp, []apitypes.LifecycleEvent{{
				Kind:             apitypes.LifecycleThreadChanged,
				ItemID:           tagItemID,
				ItemKind:         "prompt",
				ThreadID:         threadID,
				PreviousThreadID: row.PreviousThreadID.String,
				Divider:          &divider,
				Rows:             rows,
				CreatedAt:        createdAt,
			}})
		})
	if err != nil {
		return nil, batch, err
	}
	if batch != nil {
		for i := range batch.Events {
			if ev := batch.Events[i]; ev.Kind == apitypes.LifecycleThreadChanged {
				activated = &ActivatedThread{
					ID:         ev.ThreadID,
					PreviousID: ev.PreviousThreadID,
				}
				break
			}
		}
	}
	return activated, batch, nil
}

// agentCoords reads one agent's journal coordinates on the caller's
// transaction — the shared head of every cursor validation, replay page,
// and snapshot read, so classification and the events it classifies can
// never come from different snapshots. An agent whose journal is empty
// reports head and floor zero.
func (s *TurnService) agentCoords(ctx context.Context, q *db.Queries, agentID string) (head, floor int64, err error) {
	state, err := q.GetAgentSyncState(ctx, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	} else if err != nil {
		return 0, 0, fmt.Errorf("journal coords: %w", err)
	}
	return state.HeadPos, state.FloorPos, nil
}

// JournalStatus reports the database's history identity and one agent's
// committed journal coordinates; historyID is immutable, so the single
// SELECT is atomic by itself.
func (s *TurnService) JournalStatus(ctx context.Context, agentID string) (string, int64, int64, error) {
	head, floor, err := s.agentCoords(ctx, s.queries, agentID)
	if err != nil {
		return "", 0, 0, fmt.Errorf("journal status: %w", err)
	}
	return s.historyID, head, floor, nil
}

// ReplayAfter answers one bounded replay page: complete transaction
// batches after afterPos, never a partial one. Scope validation, the
// coordinates it classifies against, and the events themselves are one
// read transaction, so a concurrent append can never split the answer.
// A cursor from another history, below the retention floor, or above
// the committed head cannot resume — the caller recovers from a
// snapshot instead.
func (s *TurnService) ReplayAfter(ctx context.Context, agentID, historyID string, afterPos int64, limit int) (*JournalPage, error) {
	if limit < 1 {
		limit = 1
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("replay journal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.queries.WithTx(tx)

	head, floor, err := s.agentCoords(ctx, qtx, agentID)
	if err != nil {
		return nil, fmt.Errorf("replay journal: %w", err)
	}
	if s.historyID != historyID {
		return nil, fmt.Errorf("replay journal: %w", ErrReplayForeign)
	}
	if afterPos > head {
		return nil, fmt.Errorf("replay journal: %w", ErrReplayAhead)
	}
	// A cursor at the head is caught up even when retention emptied the
	// journal. The floor names the oldest retained position, so a cursor
	// one below it has lost nothing — the page starts exactly at the
	// floor — while anything older has lost events.
	if afterPos < floor-1 && afterPos != head {
		return nil, fmt.Errorf("replay journal: %w", ErrReplayExpired)
	}

	page, err := s.readEventsAfter(ctx, qtx, agentID, afterPos, limit)
	if err != nil {
		return nil, fmt.Errorf("replay journal: %w", err)
	}
	return page, nil
}

// readEventsAfter reads complete transaction batches after afterPos on
// the caller's transaction, journal payloads verbatim. A page never
// splits a transaction: reading continues until a batch boundary lands
// inside the answer, and a journal whose tail ends inside a batch is
// corrupt — the read fails rather than return a partial page.
func (s *TurnService) readEventsAfter(ctx context.Context, qtx *db.Queries, agentID string, afterPos int64, limit int) (*JournalPage, error) {
	rows, err := qtx.ListLifecycleEventsAfter(ctx, db.ListLifecycleEventsAfterParams{
		AgentID: agentID,
		Pos:     afterPos,
		Limit:   int64(limit),
	})
	if err != nil {
		return nil, err
	}
	for len(rows) > 0 && rows[len(rows)-1].BatchEnd == 0 {
		if len(rows) > maxReplayPageRows {
			return nil, fmt.Errorf("replay page exceeded %d rows without a batch boundary", maxReplayPageRows)
		}
		next, err := qtx.ListLifecycleEventsAfter(ctx, db.ListLifecycleEventsAfterParams{
			AgentID: agentID,
			Pos:     rows[len(rows)-1].Pos,
			Limit:   int64(limit),
		})
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			return nil, fmt.Errorf("journal tail ends inside a batch at position %d", rows[len(rows)-1].Pos)
		}
		rows = append(rows, next...)
	}
	if len(rows) > maxReplayPageRows {
		return nil, fmt.Errorf("replay page exceeded %d rows without a batch boundary", maxReplayPageRows)
	}
	page := &JournalPage{Events: make([]json.RawMessage, 0, len(rows))}
	for _, row := range rows {
		page.Events = append(page.Events, json.RawMessage(row.Payload))
	}
	if len(rows) > 0 {
		page.LastPos = rows[len(rows)-1].Pos
	}
	return page, nil
}

// readbackBatch resolves an ambiguous commit: the store reported the
// transaction failed, so the outcome is re-read from storage before the
// caller classifies it. Events past preHead mean the transaction
// applied — the committed batch is reconstructed and returned — while
// an unmoved head means nothing landed. nil always means "not applied".
func (s *TurnService) readbackBatch(ctx context.Context, agentID string, preHead int64) *JournalBatch {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.queries.WithTx(tx)
	head, _, err := s.agentCoords(ctx, qtx, agentID)
	if err != nil || head <= preHead {
		return nil
	}
	page, err := s.readEventsAfter(ctx, qtx, agentID, preHead, 64)
	if err != nil || len(page.Events) == 0 {
		return nil
	}
	// Live publication needs the typed events; replay serves the raw
	// bytes, so the readback decodes its own copy.
	events := make([]apitypes.LifecycleEvent, 0, len(page.Events))
	for _, raw := range page.Events {
		var ev apitypes.LifecycleEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil
		}
		events = append(events, ev)
	}
	return &JournalBatch{
		Cursor: FormatCursor(s.historyID, page.LastPos),
		Events: events,
	}
}

// SyncSnapshot answers the authoritative baseline: the cursor at the
// committed head and the active thread's newest display page, read in
// one transaction so a separately fetched page can never be paired
// with a newer head.
func (s *TurnService) SyncSnapshot(ctx context.Context, agentID string, pageLimit int64) (*SyncSnapshot, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sync snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.queries.WithTx(tx)

	head, _, err := s.agentCoords(ctx, qtx, agentID)
	if err != nil {
		return nil, fmt.Errorf("sync snapshot: %w", err)
	}
	page, err := threadpage.Read(ctx, qtx, agentID, "", pageLimit)
	if err != nil {
		if errors.Is(err, threadpage.ErrUnknownAgent) {
			return nil, fmt.Errorf("sync snapshot: %w", ErrUnknownAgent)
		}
		return nil, fmt.Errorf("sync snapshot: %w", err)
	}
	// A cancellation mints no transcript row, so the baseline carries the
	// retained cancellation notices itself: a fresh projection learns
	// which submissions never ran without replaying the whole window.
	// The notice ids match the ones the live reducer mints, so a client
	// that later replays the same event never renders it twice.
	cancelled, err := qtx.ListRecentCancelledEvents(ctx, db.ListRecentCancelledEventsParams{
		AgentID: agentID,
		Limit:   20,
	})
	if err != nil {
		return nil, fmt.Errorf("sync snapshot: %w", err)
	}
	for i := len(cancelled) - 1; i >= 0; i-- {
		var ev apitypes.LifecycleEvent
		if err := json.Unmarshal([]byte(cancelled[i]), &ev); err != nil {
			return nil, fmt.Errorf("sync snapshot: decode cancelled notice: %w", err)
		}
		if ev.ItemID == "" {
			continue
		}
		page.Items = append(page.Items, apitypes.ThreadMessage{
			ID:        "cancelled-" + ev.ItemID,
			Role:      apitypes.ThreadRoleError,
			Content:   "cancelled (" + ev.Reason + "): " + ev.Content,
			CreatedAt: ev.CreatedAt,
		})
	}
	// Notices are timestamped display rows like any other: sorting them
	// into the page keeps the baseline in transcript order. A notice is
	// always minted after its thread's creation, so the head divider
	// never sorts below one.
	sort.SliceStable(page.Items, func(i, j int) bool {
		return page.Items[i].CreatedAt < page.Items[j].CreatedAt
	})
	return &SyncSnapshot{Cursor: FormatCursor(s.historyID, head), Page: page}, nil
}

// recoverUnfinished settles every item the previous process left without a
// terminal outcome: an accepted item becomes cancelled (it never ran) —
// its submission rides the outcome so a recovering client can name what
// was cancelled, and no transcript row exists or is created — while a
// started one is interrupted with one display-only notice in its
// recorded thread and no assistant output synthesized. Nothing is
// re-executed and nothing is guessed — the second run of this function
// is a no-op because the transitions are guarded. An item that cannot
// be settled is logged and skipped — it retries next boot, and it never
// blocks the remaining items. Once every item is settled, retention
// runs: a journal that filled its budget before shutdown is trimmed at
// boot, not after the next hundred appends.
func (s *TurnService) recoverUnfinished(ctx context.Context) error {
	items, err := s.queries.ListUnfinishedItems(ctx)
	if err != nil {
		return fmt.Errorf("recover unfinished items: %w", err)
	}
	for _, item := range items {
		var err error
		switch item.State {
		case "accepted":
			_, err = journalTx(ctx, s, item.AgentID, "recover accepted item",
				func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
					n, err := qtx.CancelAcceptedQueueItem(ctx, db.CancelAcceptedQueueItemParams{
						FinishedMs: sql.NullInt64{Int64: stamp, Valid: true},
						Reason:     sql.NullString{String: restartReason, Valid: true},
						AgentID:    item.AgentID,
						ID:         item.ID,
					})
					if err != nil {
						return nil, err
					}
					if n == 0 {
						// Another recovery pass settled it first:
						// nothing to write.
						return nil, nil
					}
					var model *apitypes.ModelEntry
					if item.Model.Valid {
						var entry apitypes.ModelEntry
						if err := json.Unmarshal([]byte(item.Model.String), &entry); err == nil {
							model = &entry
						}
					}
					return s.appendEvents(ctx, qtx, item.AgentID, item.ID, head, stamp, []apitypes.LifecycleEvent{{
						Kind:      apitypes.LifecycleItemFinished,
						ItemID:    item.ID,
						ItemKind:  item.Kind,
						Content:   item.Content,
						Model:     model,
						Outcome:   apitypes.ItemCancelled,
						Reason:    restartReason,
						CreatedAt: threadpage.ISOTime(stamp),
					}})
				})
		case "started":
			_, err = journalTx(ctx, s, item.AgentID, "recover started item",
				func(qtx *db.Queries, stamp, head int64) (*JournalBatch, error) {
					var rows []apitypes.ThreadMessage
					if item.ThreadID.Valid {
						inserted, err := s.insertRows(ctx, qtx, item.AgentID, item.ThreadID.String, stamp, []ThreadMessageDraft{{
							Role:    apitypes.ThreadRoleError,
							Content: interruptedNotice,
							TurnID:  item.ID,
						}})
						if err != nil {
							return nil, err
						}
						rows = inserted
					}
					n, err := qtx.SetQueueItemTerminal(ctx, db.SetQueueItemTerminalParams{
						State:      apitypes.ItemInterrupted,
						FinishedMs: sql.NullInt64{Int64: stamp, Valid: true},
						Reason:     sql.NullString{String: restartReason, Valid: true},
						AgentID:    item.AgentID,
						ID:         item.ID,
					})
					if err != nil {
						return nil, err
					}
					if n == 0 {
						return nil, nil
					}
					// Still-open invocations settle here: 'finished'
					// with no recorded result is the honest
					// bookkeeping for an outcome nobody observed.
					if _, err := qtx.InterruptOpenToolInvocations(ctx, db.InterruptOpenToolInvocationsParams{
						AgentID: item.AgentID,
						ItemID:  item.ID,
					}); err != nil {
						return nil, err
					}
					batch, err := s.appendEvents(ctx, qtx, item.AgentID, item.ID, head, stamp, []apitypes.LifecycleEvent{{
						Kind:      apitypes.LifecycleItemFinished,
						ItemID:    item.ID,
						ItemKind:  item.Kind,
						ThreadID:  item.ThreadID.String,
						Rows:      rows,
						Outcome:   apitypes.ItemInterrupted,
						Reason:    restartReason,
						CreatedAt: threadpage.ISOTime(stamp),
					}})
					if err != nil {
						return nil, err
					}
					if len(rows) > 0 {
						s.lastPersistedMs = stamp + int64(len(rows)) - 1
					}
					return batch, nil
				})
		}
		if err != nil {
			// One unsettleable item must not strand the rest: it stays
			// unresolved and the next boot retries it.
			s.logger.Warn().Err(err).Str("agent_id", item.AgentID).Str("item_id", item.ID).
				Msg("recover unfinished item")
		}
	}
	agents, err := s.queries.ListJournalAgents(ctx)
	if err != nil {
		return fmt.Errorf("recover unfinished items: list agents: %w", err)
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	for _, agentID := range agents {
		if err := s.pruneJournalLocked(ctx, agentID); err != nil {
			s.logger.Warn().Err(err).Str("agent_id", agentID).
				Msg("prune lifecycle journal after recovery")
		}
	}
	return nil
}

// pruneJournalLocked applies retention for one agent: delete the oldest
// journal prefix that ends on a transaction boundary, advance the replay
// floor, then drop terminal item bookkeeping no retained event needs.
// Unresolved items survive — restart recovery still depends on them.
// Caller holds persistMu.
func (s *TurnService) pruneJournalLocked(ctx context.Context, agentID string) error {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	qtx := s.queries.WithTx(tx)
	state, err := qtx.GetAgentSyncState(ctx, agentID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	cutoff, err := qtx.MaxLifecycleBatchEnd(ctx, db.MaxLifecycleBatchEndParams{
		AgentID: agentID,
		Pos:     state.HeadPos - journalRetainPerAgent,
	})
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if cutoff > 0 {
		if err := qtx.DeleteLifecycleEventsUpTo(ctx, db.DeleteLifecycleEventsUpToParams{
			AgentID: agentID,
			Pos:     cutoff,
		}); err != nil {
			_ = tx.Rollback()
			return err
		}
		// Positions are dense and deletes are prefix-only, so the
		// oldest retained position is exactly the one past the cutoff
		// — including the emptied journal, whose cutoff is the head.
		floor := cutoff + 1
		if err := qtx.SetAgentSyncFloor(ctx, db.SetAgentSyncFloorParams{
			FloorPos: floor,
			AgentID:  agentID,
		}); err != nil {
			_ = tx.Rollback()
			return err
		}
		// The deleted prefix may hold an item's only events: terminal
		// bookkeeping nothing retained names goes with them.
		if _, err := qtx.DeleteOrphanTerminalItems(ctx, agentID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := s.commitTx(tx); err != nil {
		return err
	}
	return nil
}
