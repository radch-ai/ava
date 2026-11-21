// The thread endpoint: GET /agent/{id}/thread — one thread's
// conversation in display order. The page projection lives in
// threadpage (shared with the lifecycle snapshot); this handler owns the
// HTTP surface: query parsing, status mapping, rendering.
package agent

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/threadpage"
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

// GetThread answers one page of one thread.
func GetThread(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		page, err := threadpage.Read(r.Context(), queries, id,
			r.URL.Query().Get("cursor"), parseLimit(r))
		switch {
		case errors.Is(err, threadpage.ErrUnknownAgent):
			return api.WriteError(w, http.StatusNotFound, "Agent "+strconv.Quote(id)+" not found")
		case errors.Is(err, threadpage.ErrInvalidCursor):
			return api.WriteError(w, http.StatusBadRequest, "Invalid cursor")
		case err != nil:
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}
		return api.WriteJSON(w, http.StatusOK, page)
	}
}
