// GET /api/models - the selector-facing grouped catalog.
package providers

import (
	"net/http"
	"slices"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// GetModels answers one group per stored provider (custom ∪ fetched
// system ids, deduped with custom winning, sorted), rows in creation
// order. Providers sharing a display name merge into one group;
// providers with no models at all are dropped. No connection config
// rides along.
func GetModels(queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		rows, err := queries.ListProviders(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}

		var names []string
		grouped := map[string][]string{}
		for _, row := range rows {
			custom, err := decodeModels(row.Models)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
			system := systemModels(r.Context(), row.BaseUrl, providerKey(r.Context(), vaultStore, row.ID))
			ids := mergeIDs(custom, system)
			if len(ids) == 0 {
				continue
			}
			if existing, ok := grouped[row.Name]; ok {
				grouped[row.Name] = mergeIDs(existing, ids)
			} else {
				names = append(names, row.Name)
				grouped[row.Name] = ids
			}
		}

		out := apitypes.GetModelsResponse{Groups: make([]apitypes.ModelGroup, 0, len(names))}
		for _, name := range names {
			out.Groups = append(out.Groups, apitypes.ModelGroup{
				Provider: name,
				Models:   grouped[name],
			})
		}
		return api.WriteJSON(w, http.StatusOK, out)
	}
}

// mergeIDs unions two id lists: earlier arguments win the dedupe,
// result sorted.
func mergeIDs(custom, system []string) []string {
	seen := make(map[string]bool, len(custom)+len(system))
	ids := make([]string, 0, len(custom)+len(system))
	for _, list := range [][]string{custom, system} {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids
}
