// GET /api/providers - the settings-page catalog: stored rows with
// their custom models inline.
package providers

import (
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// GetProviders answers the stored connections in creation order.
// models carries custom ids only — system models resolve behind
// GET /api/providers/{id}/models and GET /api/models.
func GetProviders(queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		rows, err := queries.ListProviders(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}
		out := apitypes.GetProvidersResponse{
			Providers: make([]apitypes.Provider, 0, len(rows)),
		}
		for _, row := range rows {
			custom, err := decodeModels(row.Models)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
			hasKey, err := vaultStore.Has(r.Context(), vault.NameRuntime, providerEntryName(row.ID))
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("read key state for provider %q: %w", row.ID, err)
			}
			out.Providers = append(out.Providers, apitypes.Provider{
				ID:      row.ID,
				Name:    row.Name,
				BaseURL: row.BaseUrl,
				HasKey:  hasKey,
				Models:  custom,
			})
		}
		return api.WriteJSON(w, http.StatusOK, out)
	}
}
