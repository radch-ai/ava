// Package threadpage builds one display page of one conversation
// thread: the projection both the thread endpoint and the lifecycle
// snapshot serve. A page is thread-scoped — the no-cursor read starts at
// the agent's active thread, and each page names the next one through an
// opaque cursor. Rows come from ListThreadPageRows; the gateway's replay
// projection reads the active thread separately (ListActiveThreadRows).
// The limit applies to visible rows: structure-only rows (empty
// assistant records that carry only tool-call structure) never consume a
// page slot or hide older visible history.
//
// Each page is answered oldest-to-newest. The cursor names a position
// inside one thread — `<thread_id>:<message_id>` for a message anchor,
// `<thread_id>:` for that thread's newest page. Walking past a thread's
// oldest visible row crosses into its predecessor: the page that
// exhausts a thread carries the divider its kind names — "New session"
// or "Compacted" — generated from the thread record whose
// previous_thread_id names the thread before it.
package threadpage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

// Divider labels. A boundary tells the reader which of the two chain
// growth ways produced it: a plain /new or a compaction successor.
const (
	DividerText   = "New session"
	CompactedText = "Compacted"
)

// CompactedKind is the threads.kind value a compaction successor row
// carries; everything else is a plain boundary.
const CompactedKind = "compact"

// Page read classifications. A missing agent and an unusable cursor are
// the caller's two recoverable answers; anything else is a storage
// failure. This package is backend-internal, so the backend package maps
// these to its own error surface rather than importing it back.
var (
	ErrUnknownAgent  = errors.New("agent not found")
	ErrInvalidCursor = errors.New("invalid cursor")
)

// ISOTime renders a stored ms epoch as the wire's ISO string.
func ISOTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// dividerLabel maps a thread's stored kind to its boundary label.
func dividerLabel(kind string) string {
	if kind == CompactedKind {
		return CompactedText
	}
	return DividerText
}

// Divider is the display record of one thread boundary: the thread's own
// id keys it, its creation time dates it, and the caller's label names
// it (dividerLabel derives it from the thread's kind).
func Divider(threadID string, createdAt int64, label string) apitypes.ThreadMessage {
	return apitypes.ThreadMessage{
		ID:        threadID,
		Role:      apitypes.ThreadRoleDivider,
		Content:   label,
		CreatedAt: ISOTime(createdAt),
	}
}

// cursor is a parsed page anchor: the thread to read and, when
// messageID is non-empty, the message inside it to read strictly older
// than.
type cursor struct {
	threadID  string
	messageID string
}

// parseCursor splits an opaque cursor into its two halves. The colon is
// required — an empty thread id is invalid.
func parseCursor(raw string) (cursor, bool) {
	threadID, messageID, ok := strings.Cut(raw, ":")
	if !ok || threadID == "" {
		return cursor{}, false
	}
	return cursor{threadID: threadID, messageID: messageID}, true
}

// Read answers one page of one thread. limit is the caller's already
// clamped visible-row budget.
func Read(ctx context.Context, queries *db.Queries, agentID, rawCursor string, limit int64) (*apitypes.ThreadPage, error) {
	threadID := ""
	previous := ""
	threadCreatedAt := int64(0)
	threadKind := ""
	beforeCreatedAt := int64(0)
	beforeBand := int64(0)
	beforeMessageID := sql.NullString{}

	if rawCursor == "" {
		// Every agent has an active thread (create-agent seeds it
		// transactionally), so the pointer read answers both existence
		// and the entry thread in one round trip.
		meta, err := queries.GetActiveThreadMeta(ctx, agentID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownAgent, agentID)
		} else if err != nil {
			return nil, fmt.Errorf("resolve active thread for %q: %w", agentID, err)
		}
		threadID = meta.ID
		previous = meta.PreviousThreadID.String
		threadCreatedAt = meta.CreatedAt
		threadKind = meta.Kind
	} else {
		// The cursor path distinguishes an unknown agent from a foreign
		// thread or message.
		if _, err := queries.GetAgentID(ctx, agentID); errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownAgent, agentID)
		} else if err != nil {
			return nil, fmt.Errorf("get agent %q: %w", agentID, err)
		}
		anchor, ok := parseCursor(rawCursor)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrInvalidCursor, rawCursor)
		}
		meta, err := queries.GetThreadMeta(ctx, db.GetThreadMetaParams{
			AgentID: agentID, ID: anchor.threadID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidCursor, rawCursor)
		} else if err != nil {
			return nil, fmt.Errorf("resolve thread for %q: %w", agentID, err)
		}
		threadID = anchor.threadID
		previous = meta.PreviousThreadID.String
		threadCreatedAt = meta.CreatedAt
		threadKind = meta.Kind
		if anchor.messageID != "" {
			// The anchor must be a row this thread displays -- its
			// own or one its references carry: a foreign id is an
			// invalid cursor, not an empty page. The placement key
			// (sort triple) is what the keyset read binds.
			row, err := queries.GetThreadMessageMeta(ctx, db.GetThreadMessageMetaParams{
				ID: agentID, AgentID: agentID, ThreadID: threadID, AnchorID: anchor.messageID,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: %q", ErrInvalidCursor, rawCursor)
			} else if err != nil {
				return nil, fmt.Errorf("resolve cursor for %q: %w", agentID, err)
			}
			beforeCreatedAt = row.CreatedAt
			beforeBand = row.Band
			beforeMessageID = sql.NullString{String: row.ID, Valid: true}
		}
	}

	// One row beyond the limit proves older visible rows exist — the
	// visibility filter runs in SQL, so hidden structure rows never
	// consume a slot or fake a continuation.
	var rows []db.ListThreadPageRowsRow
	hasOlder := false
	if beforeMessageID.Valid {
		anchored, err := queries.ListThreadPageRowsBefore(ctx, db.ListThreadPageRowsBeforeParams{
			ID:              agentID,
			AgentID:         agentID,
			ThreadID:        threadID,
			BeforeCreatedAt: beforeCreatedAt,
			BeforeBand:      beforeBand,
			BeforeID:        beforeMessageID.String,
			RowLimit:        limit + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("load thread for %q: %w", agentID, err)
		}
		if int64(len(anchored)) > limit {
			anchored = anchored[:limit]
			hasOlder = true
		}
		rows = make([]db.ListThreadPageRowsRow, len(anchored))
		for i, r := range anchored {
			rows[i] = db.ListThreadPageRowsRow(r)
		}
	} else {
		newest, err := queries.ListThreadPageRows(ctx, db.ListThreadPageRowsParams{
			ID:       agentID,
			AgentID:  agentID,
			ThreadID: threadID,
			RowLimit: limit + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("load thread for %q: %w", agentID, err)
		}
		if int64(len(newest)) > limit {
			newest = newest[:limit]
			hasOlder = true
		}
		rows = newest
	}

	// The read is newest-first; the page answers oldest-first. The
	// boundary sits above the thread's oldest rows.
	items := make([]apitypes.ThreadMessage, 0, len(rows)+1)
	if !hasOlder && previous != "" {
		items = append(items, Divider(threadID, threadCreatedAt, dividerLabel(threadKind)))
	}
	oldestRowID := ""
	for i := len(rows) - 1; i >= 0; i-- {
		items = append(items, apitypes.ThreadMessage{
			ID:        rows[i].ID,
			Role:      apitypes.ThreadRole(rows[i].Role),
			Content:   rows[i].Content,
			CreatedAt: ISOTime(rows[i].CreatedAt),
		})
	}
	if len(rows) > 0 {
		oldestRowID = rows[len(rows)-1].ID
	}

	var next *string
	switch {
	case hasOlder:
		// Continue inside this thread, strictly older than the oldest
		// row answered.
		older := threadID + ":" + oldestRowID
		next = &older
	case previous != "":
		// The thread is exhausted: its boundary is already at the
		// bottom of what the reader has, and the predecessor's newest
		// page follows.
		older := previous + ":"
		next = &older
	}

	return &apitypes.ThreadPage{
		Items:      items,
		NextCursor: next,
		ThreadID:   threadID,
	}, nil
}
