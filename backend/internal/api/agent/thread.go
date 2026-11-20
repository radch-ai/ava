// The thread endpoint: GET /agent/{id}/thread — one thread's
// conversation in display order. A page is thread-scoped: the
// no-cursor read starts at the agent's active thread, and each page
// names the next one through an opaque cursor. The page reads
// ListThreadPageRows; the gateway's replay projection reads the
// active thread (ListActiveThreadRows via the resolved pointer). The
// limit applies to visible rows: structure-only rows (empty assistant
// records that carry only tool-call structure) never consume a page
// slot or hide older visible history.
//
// Each page is answered oldest-to-newest. The cursor names a position
// inside one thread — `<thread_id>:<message_id>` for a message
// anchor, `<thread_id>:` for that thread's newest page. Walking past
// a thread's oldest visible row crosses into its predecessor: the
// page that exhausts a thread carries the "New session" divider,
// generated from the thread record whose previous_thread_id names the
// thread before it.
package agent

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// dividerText is the label a thread boundary renders.
const dividerText = "New session"

// parseLimit reads ?limit: garbage falls back to the default, the
// floor is 1, the cap 100.
func parseLimit(r *http.Request) int64 {
	limit := int64(20)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			limit = n
		}
	}
	switch {
	case limit < 1:
		limit = 1
	case limit > 100:
		limit = 100
	}
	return limit
}

// isoTime renders the DB's ms epoch as the wire's ISO string.
func isoTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// divider is the display record of one thread boundary: the thread's
// own id keys it, its creation time dates it.
func divider(threadID string, createdAt int64, text string) apitypes.ThreadMessage {
	return apitypes.ThreadMessage{
		ID:        threadID,
		Role:      apitypes.ThreadRoleDivider,
		Content:   text,
		CreatedAt: isoTime(createdAt),
	}
}

// threadCursor is a parsed page anchor: the thread to read and, when
// messageID is non-empty, the message inside it to read strictly
// older than.
type threadCursor struct {
	threadID  string
	messageID string
}

// parseThreadCursor splits an opaque cursor into its two halves. The
// colon is required — an empty thread id is invalid.
func parseThreadCursor(raw string) (threadCursor, bool) {
	threadID, messageID, ok := strings.Cut(raw, ":")
	if !ok || threadID == "" {
		return threadCursor{}, false
	}
	return threadCursor{threadID: threadID, messageID: messageID}, true
}

// GetThread answers one page of one thread.
func GetThread(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		ctx := r.Context()

		threadID := ""
		previous := ""
		threadCreatedAt := int64(0)
		beforeCreatedAt := int64(0)
		beforeMessageID := sql.NullString{}

		raw := r.URL.Query().Get("cursor")
		if raw == "" {
			// Every agent has an active thread (create-agent seeds it
			// transactionally), so the pointer read answers both
			// existence and the entry thread in one round trip.
			meta, err := queries.GetActiveThreadMeta(ctx, id)
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
			} else if err != nil {
				return fmt.Errorf("resolve active thread for %q: %w", id, err)
			}
			threadID = meta.ID
			previous = meta.PreviousThreadID.String
			threadCreatedAt = meta.CreatedAt
		} else {
			// The cursor path distinguishes an unknown agent (404)
			// from a foreign thread or message (400).
			if _, err := queries.GetAgentID(ctx, id); errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
			} else if err != nil {
				return fmt.Errorf("get agent %q: %w", id, err)
			}
			cursor, ok := parseThreadCursor(raw)
			if !ok {
				return api.WriteError(w, http.StatusBadRequest, "Invalid cursor")
			}
			meta, err := queries.GetThreadMeta(ctx, db.GetThreadMetaParams{
				AgentID: id, ID: cursor.threadID,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusBadRequest, "Invalid cursor")
			} else if err != nil {
				return fmt.Errorf("resolve thread for %q: %w", id, err)
			}
			threadID = cursor.threadID
			previous = meta.PreviousThreadID.String
			threadCreatedAt = meta.CreatedAt
			if cursor.messageID != "" {
				// The anchor must be a visible row of this thread:
				// a foreign id is an invalid cursor, not an empty page.
				anchor, err := queries.GetThreadMessageMeta(ctx, db.GetThreadMessageMetaParams{
					AgentID: id, ThreadID: threadID, ID: cursor.messageID,
				})
				if errors.Is(err, sql.ErrNoRows) {
					return api.WriteError(w, http.StatusBadRequest, "Invalid cursor")
				} else if err != nil {
					return fmt.Errorf("resolve cursor for %q: %w", id, err)
				}
				beforeCreatedAt = anchor.CreatedAt
				beforeMessageID = sql.NullString{String: anchor.ID, Valid: true}
			}
		}

		// One row beyond the limit proves older visible rows exist —
		// the visibility filter runs in SQL, so hidden structure rows
		// never consume a slot or fake a continuation.
		limit := parseLimit(r)
		var items []apitypes.ThreadMessage
		var oldestRowID string
		hasOlder := false
		if beforeMessageID.Valid {
			anchored, err := queries.ListThreadPageRowsBefore(ctx, db.ListThreadPageRowsBeforeParams{
				AgentID:         id,
				ThreadID:        threadID,
				BeforeCreatedAt: beforeCreatedAt,
				BeforeID:        beforeMessageID.String,
				Limit:           limit + 1,
			})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("load thread for %q: %w", id, err)
			}
			if int64(len(anchored)) > limit {
				anchored = anchored[:limit]
				hasOlder = true
			}
			items = make([]apitypes.ThreadMessage, 0, len(anchored)+1)
			// The read is newest-first; the page answers oldest-first.
			// The boundary sits above the thread's oldest rows.
			if !hasOlder && previous != "" {
				items = append(items, divider(threadID, threadCreatedAt, dividerText))
			}
			for i := len(anchored) - 1; i >= 0; i-- {
				items = append(items, apitypes.ThreadMessage{
					ID:        anchored[i].ID,
					Role:      apitypes.ThreadRole(anchored[i].Role),
					Content:   anchored[i].Content,
					CreatedAt: isoTime(anchored[i].CreatedAt),
				})
			}
			if len(anchored) > 0 {
				oldestRowID = anchored[len(anchored)-1].ID
			}
		} else {
			newest, err := queries.ListThreadPageRows(ctx, db.ListThreadPageRowsParams{
				AgentID:  id,
				ThreadID: threadID,
				Limit:    limit + 1,
			})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("load thread for %q: %w", id, err)
			}
			if int64(len(newest)) > limit {
				newest = newest[:limit]
				hasOlder = true
			}
			items = make([]apitypes.ThreadMessage, 0, len(newest)+1)
			if !hasOlder && previous != "" {
				items = append(items, divider(threadID, threadCreatedAt, dividerText))
			}
			for i := len(newest) - 1; i >= 0; i-- {
				items = append(items, apitypes.ThreadMessage{
					ID:        newest[i].ID,
					Role:      apitypes.ThreadRole(newest[i].Role),
					Content:   newest[i].Content,
					CreatedAt: isoTime(newest[i].CreatedAt),
				})
			}
			if len(newest) > 0 {
				oldestRowID = newest[len(newest)-1].ID
			}
		}

		var next *string
		switch {
		case hasOlder:
			// Continue inside this thread, strictly older than the
			// oldest row answered.
			older := threadID + ":" + oldestRowID
			next = &older
		case previous != "":
			// The thread is exhausted: its boundary is already at the
			// bottom of what the reader has, and the predecessor's
			// newest page follows.
			older := previous + ":"
			next = &older
		}

		return api.WriteJSON(w, http.StatusOK, apitypes.ThreadPage{
			Items:      items,
			NextCursor: next,
			ThreadID:   threadID,
		})
	}
}
