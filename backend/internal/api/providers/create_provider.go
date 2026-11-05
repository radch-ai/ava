// POST /api/providers - add a connection. Models arrive later, via
// PATCH on the created provider.
package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// CreateProvider validates the body (non-empty name, http(s) base
// URL), mints the uuid id and answers 201 with it.
func CreateProvider(queries *db.Queries) api.Handler {
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
		id, err := newUUID()
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("mint provider id: %w", err)
		}

		if err := queries.CreateProvider(r.Context(), db.CreateProviderParams{
			ID:      id,
			Name:    req.Name,
			BaseUrl: req.BaseURL,
			ApiKey:  req.APIKey,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("insert provider: %w", err)
		}

		return api.WriteJSON(w, http.StatusCreated, apitypes.CreateProviderResponse{ID: id})
	}
}
