// DELETE /api/providers/{id} - remove a stored connection.
package providers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// DeleteProvider removes the provider's stored row (custom model
// array included). Stored model strings elsewhere survive —
// staleness is caught at call time.
func DeleteProvider(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		if _, err := queries.GetProvider(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("Provider %q not found", id))
		} else if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read provider %q: %w", id, err)
		}

		if err := queries.DeleteProvider(r.Context(), id); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("delete provider %q: %w", id, err)
		}

		return api.WriteJSON(w, http.StatusOK,
			apitypes.DeleteProviderResponse{Message: "Provider deleted"})
	}
}
