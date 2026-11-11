// The thread endpoint: GET /agent/{id}/thread — the agent's
// conversation, paged newest-first. The cursor names the previous
// page's oldest message; the next page is strictly older.
package agent

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// parseLimit reads ?limit with the mock's rules: garbage falls back
// to the default, the floor is 1, the cap 100.
func parseLimit(r *http.Request) int64 {
	limit := int64(20)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			limit = n
		}
	}
	switch {
	case limit < 1:
		return 1
	case limit > 100:
		return 100
	}
	return limit
}

// isoTime renders the DB's ms epoch as the wire's ISO string.
func isoTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// threadMessage assembles one wire message from a row's columns.
func threadMessage(id, role, content string, createdAt int64) apitypes.ThreadMessage {
	return apitypes.ThreadMessage{
		ID:        id,
		Role:      apitypes.ThreadRole(role),
		Content:   content,
		CreatedAt: isoTime(createdAt),
	}
}

// GetThread answers one page of the agent's conversation.
func GetThread(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		if _, err := queries.GetAgentID(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		} else if err != nil {
			return fmt.Errorf("get agent %q: %w", id, err)
		}

		limit := parseLimit(r)
		// Fetch one extra row to decide the cursor without a count.
		fetch := limit + 1
		var items []apitypes.ThreadMessage
		var next *string

		cursor := r.URL.Query().Get("cursor")
		if cursor == "" {
			rows, err := queries.ListThreadFirstPage(r.Context(),
				db.ListThreadFirstPageParams{AgentID: id, Limit: fetch})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("list thread for %q: %w", id, err)
			}
			if int64(len(rows)) > limit {
				rows = rows[:limit]
				last := rows[len(rows)-1].ID
				next = &last
			}
			for _, row := range rows {
				items = append(items, threadMessage(row.ID, row.Role, row.Content, row.CreatedAt))
			}
		} else {
			// The cursor must name one of this agent's messages.
			anchor, err := queries.GetThreadMessage(r.Context(),
				db.GetThreadMessageParams{ID: cursor, AgentID: id})
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusBadRequest, "Invalid cursor")
			} else if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("get cursor %q: %w", cursor, err)
			}
			rows, err := queries.ListThreadAfterCursor(r.Context(),
				db.ListThreadAfterCursorParams{
					AgentID: id, CreatedAt: anchor.CreatedAt, CreatedAt_2: anchor.CreatedAt,
					ID: anchor.ID, Limit: fetch,
				})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("list thread for %q after %q: %w", id, cursor, err)
			}
			if int64(len(rows)) > limit {
				rows = rows[:limit]
				last := rows[len(rows)-1].ID
				next = &last
			}
			for _, row := range rows {
				items = append(items, threadMessage(row.ID, row.Role, row.Content, row.CreatedAt))
			}
		}

		if items == nil {
			items = []apitypes.ThreadMessage{}
		}
		return api.WriteJSON(w, http.StatusOK, apitypes.ThreadPage{Items: items, NextCursor: next})
	}
}
