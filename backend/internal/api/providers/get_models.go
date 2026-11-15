// GET /api/models - the selector-facing grouped catalog.
package providers

import (
	"net/http"
	"slices"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// GetModels answers the catalog: one group per stored provider
// (rows in creation order), entries per ModelGroup's rule.
func GetModels(queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		rows, err := queries.ListProviders(r.Context())
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}

		var names []string
		grouped := map[string][]apitypes.ModelEntry{}
		for _, row := range rows {
			custom, err := decodeModels(row.Models)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
			system := systemModels(r.Context(), row.BaseUrl, providerKey(r.Context(), vaultStore, row.ID))
			entries := mergeEntries(row.ID, custom, system)
			if len(entries) == 0 {
				continue
			}
			if existing, ok := grouped[row.Name]; ok {
				existing = append(existing, entries...)
				// Stable: equal ids from different rows keep row
				// creation order and stay adjacent.
				slices.SortStableFunc(existing, func(a, b apitypes.ModelEntry) int {
					return strings.Compare(a.ID, b.ID)
				})
				grouped[row.Name] = existing
			} else {
				names = append(names, row.Name)
				grouped[row.Name] = entries
			}
		}

		out := apitypes.GetModelsResponse{Groups: make([]apitypes.ModelGroup, 0, len(names))}
		for _, name := range names {
			out.Groups = append(out.Groups, apitypes.ModelGroup{
				ProviderName: name,
				Models:       grouped[name],
			})
		}
		return api.WriteJSON(w, http.StatusOK, out)
	}
}

// mergeEntries tags one provider row's custom ∪ system ids with the
// row's id: earlier arguments win the dedupe, result sorted by id.
func mergeEntries(providerID string, custom, system []string) []apitypes.ModelEntry {
	seen := make(map[string]bool, len(custom)+len(system))
	entries := make([]apitypes.ModelEntry, 0, len(custom)+len(system))
	for _, list := range [][]string{custom, system} {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				entries = append(entries, apitypes.ModelEntry{ID: id, ProviderID: providerID})
			}
		}
	}
	slices.SortFunc(entries, func(a, b apitypes.ModelEntry) int {
		return strings.Compare(a.ID, b.ID)
	})
	return entries
}
