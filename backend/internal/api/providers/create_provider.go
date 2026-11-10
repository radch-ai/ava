// POST /api/providers - add a connection. Custom models arrive
// later, via PATCH on the created provider.
package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// CreateProvider validates the body (non-empty name, http(s) base
// URL), verifies the connection — keyed or keyless — against the
// provider's model list before anything is written, and answers 201
// with the minted uuid id.
func CreateProvider(queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.CreateProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}

		if strings.TrimSpace(req.Name) == "" {
			return api.WriteError(w, http.StatusBadRequest,
				"Provider name is required")
		}
		if !isValidBaseURL(req.BaseURL) {
			return api.WriteError(w, http.StatusBadRequest,
				"Invalid baseUrl: must be an http(s) URL")
		}

		// Every create claims a working connection — keyed or
		// keyless — so every create is verified before saving. A
		// rejected key or URL writes nothing.
		if err := probeConnection(r.Context(), req.BaseURL, req.APIKey); err != nil {
			return api.WriteError(w, http.StatusBadRequest,
				"Connection check failed: "+err.Error())
		}

		id, err := newUUID()
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("mint provider id: %w", err)
		}

		if err := queries.CreateProvider(r.Context(), db.CreateProviderParams{
			ID:      id,
			Name:    req.Name,
			BaseUrl: req.BaseURL,
			Models:  "[]",
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("insert provider: %w", err)
		}

		// The key, when there is one, is sealed as the provider's
		// runtime-vault entry.
		if req.APIKey != "" {
			if err := vaultStore.Put(r.Context(), vault.NameRuntime,
				providerEntryName(id), kindProvider, []byte(req.APIKey), nil); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("seal key for provider %q: %w", id, err)
			}
		}

		return api.WriteJSON(w, http.StatusCreated, apitypes.CreateProviderResponse{ID: id})
	}
}
