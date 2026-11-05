// DELETE /api/providers/{id} - remove a connection and its models.
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

// DeleteProvider removes the provider; the DB handles the rest
// (model cascade, reference SET NULL).
func DeleteProvider(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		if _, err := queries.GetProvider(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("Provider %q not found", id))
		} else if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read provider: %w", err)
		}

		if err := queries.DeleteProvider(r.Context(), id); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("delete provider: %w", err)
		}

		return api.WriteJSON(w, http.StatusOK,
			apitypes.DeleteProviderResponse{Message: "Provider deleted"})
	}
}
