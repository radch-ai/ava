// The activity endpoint: GET /agent/{id}/activity — the agent's
// scheduler executions (heartbeat, cron), paged newest-first. The
// cursor names the previous page's oldest row; the next page is
// strictly older.
package agent

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// activityItem assembles one wire row from its columns; message is
// NULL unless the run has something to say.
func activityItem(id string, createdAt int64, triggeredBy, action, status string, message sql.NullString) apitypes.AgentActivityItem {
	item := apitypes.AgentActivityItem{
		ID:          id,
		CreatedAt:   isoTime(createdAt),
		TriggeredBy: apitypes.ActivityTrigger(triggeredBy),
		Action:      action,
		Status:      apitypes.ActivityStatus(status),
	}
	if message.Valid {
		item.Message = &message.String
	}
	return item
}

// GetAgentActivity answers one page of the agent's activity feed.
func GetAgentActivity(queries *db.Queries) api.Handler {
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
		var items []apitypes.AgentActivityItem
		var next *string

		cursor := r.URL.Query().Get("cursor")
		if cursor == "" {
			rows, err := queries.ListAgentActivityFirstPage(r.Context(),
				db.ListAgentActivityFirstPageParams{AgentID: id, Limit: fetch})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("list activity for %q: %w", id, err)
			}
			if int64(len(rows)) > limit {
				rows = rows[:limit]
				last := rows[len(rows)-1].ID
				next = &last
			}
			for _, row := range rows {
				items = append(items, activityItem(row.ID, row.CreatedAt,
					row.TriggeredBy, row.Action, row.Status, row.Message))
			}
		} else {
			// The cursor must name one of this agent's rows.
			anchor, err := queries.GetAgentActivityItem(r.Context(),
				db.GetAgentActivityItemParams{ID: cursor, AgentID: id})
			if errors.Is(err, sql.ErrNoRows) {
				return api.WriteError(w, http.StatusBadRequest, "Invalid cursor")
			} else if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("get cursor %q: %w", cursor, err)
			}
			rows, err := queries.ListAgentActivityAfterCursor(r.Context(),
				db.ListAgentActivityAfterCursorParams{
					AgentID: id, CreatedAt: anchor.CreatedAt, CreatedAt_2: anchor.CreatedAt,
					ID: anchor.ID, Limit: fetch,
				})
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("list activity for %q after %q: %w", id, cursor, err)
			}
			if int64(len(rows)) > limit {
				rows = rows[:limit]
				last := rows[len(rows)-1].ID
				next = &last
			}
			for _, row := range rows {
				items = append(items, activityItem(row.ID, row.CreatedAt,
					row.TriggeredBy, row.Action, row.Status, row.Message))
			}
		}

		if items == nil {
			items = []apitypes.AgentActivityItem{}
		}
		return api.WriteJSON(w, http.StatusOK, apitypes.ActivityPage{Items: items, NextCursor: next})
	}
}
