// The thread endpoint: GET /agent/{id}/thread — the agent's
// conversation in display order. The page and the gateway's replay
// projection read the same raw-row query (ListThreadRows), so the
// limit applies to visible rows: structure-only rows (empty assistant
// records that carry only tool-call structure) never consume a page
// slot or hide older visible history. Each page is answered
// oldest-to-newest; the cursor names the answered page's oldest
// message, and the next page is strictly older.
package agent

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

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

// threadMessage assembles one wire message from a row's columns.
func threadMessage(row db.ListThreadRowsRow) apitypes.ThreadMessage {
	return apitypes.ThreadMessage{
		ID:        row.ID,
		Role:      apitypes.ThreadRole(row.Role),
		Content:   row.Content,
		CreatedAt: isoTime(row.CreatedAt),
	}
}

// visible reports whether a row renders: an assistant record with no
// text is working-context structure (a tool-call group's record or an
// empty settle), already shown by its adjacent rows.
func visible(row db.ListThreadRowsRow) bool {
	return apitypes.ThreadRole(row.Role) != apitypes.ThreadRoleAssistant || row.Content != ""
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

		rows, err := queries.ListThreadRows(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("load thread for %q: %w", id, err)
		}

		// The cursor must name one of this agent's rows — the read
		// is agent-scoped, so a foreign cursor is invalid by
		// construction.
		end := len(rows)
		cursor := r.URL.Query().Get("cursor")
		if cursor != "" {
			anchor := -1
			for i := len(rows) - 1; i >= 0; i-- {
				if rows[i].ID == cursor {
					anchor = i
					break
				}
			}
			if anchor < 0 {
				return api.WriteError(w, http.StatusBadRequest, "Invalid cursor")
			}
			end = anchor
		}

		// Take up to limit visible rows ending at `end`: the page is
		// the newest visible window older than the cursor (or the
		// newest window of all).
		limit := parseLimit(r)
		start := end
		count := int64(0)
		for start > 0 && count < limit {
			start--
			if visible(rows[start]) {
				count++
			}
		}

		items := make([]apitypes.ThreadMessage, 0, count)
		for _, row := range rows[start:end] {
			if !visible(row) {
				continue
			}
			items = append(items, threadMessage(row))
		}
		// A cursor exists only when an older visible row does.
		var next *string
		if len(items) > 0 && start > 0 && slices.ContainsFunc(rows[:start], visible) {
			oldest := items[0].ID
			next = &oldest
		}
		return api.WriteJSON(w, http.StatusOK, apitypes.ThreadPage{Items: items, NextCursor: next})
	}
}
