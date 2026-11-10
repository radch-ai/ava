// GET /api/providers/{id}/models - one provider's system models.
package providers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// GetProviderModels answers the provider's fetched system model ids:
// the live list, keyed or keyless — empty when nothing is known or
// the endpoint rejects us. Custom ids are not repeated. Unknown id
// is a 404.
func GetProviderModels(queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		row, err := queries.GetProvider(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("Provider %q not found", id))
		} else if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read provider %q: %w", id, err)
		}

		system := systemModels(r.Context(), row.BaseUrl, providerKey(r.Context(), vaultStore, id))
		if system == nil {
			system = []string{}
		}
		return api.WriteJSON(w, http.StatusOK,
			apitypes.ProviderModelsResponse{SystemModels: system})
	}
}
