// GET /api/providers - the whole catalog in one response.
package providers

import (
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// GetProviders answers the UI's getProviders(): the reserved default
// model plus every provider (creation order) with its model-id
// strings (id order).
func GetProviders(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		rows, err := queries.ListProviders(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("list providers: %w", err)
		}

		out := apitypes.GetProvidersResponse{
			DefaultModel: DefaultModel,
			Providers:    make([]apitypes.Provider, len(rows)),
		}
		index := make(map[string]int, len(rows))
		for i, row := range rows {
			out.Providers[i] = apitypes.Provider{
				ID:      row.ID,
				Name:    row.Name,
				BaseURL: row.BaseUrl,
				HasKey:  row.ApiKey != "",
				Models:  []string{},
			}
			index[row.ID] = i
		}

		models, err := queries.ListModels(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("list models: %w", err)
		}
		for _, m := range models {
			if i, found := index[m.ProviderID]; found {
				out.Providers[i].Models = append(out.Providers[i].Models, m.ModelID)
			}
		}

		return api.WriteJSON(w, http.StatusOK, out)
	}
}
