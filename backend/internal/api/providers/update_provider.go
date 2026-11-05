// PATCH /api/providers/{id} - edit a connection and/or replace its
// model list wholesale.
package providers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// UpdateProvider applies absent-keeps patch semantics and replaces
// the models list wholesale. Ownership rules live in the schema:
// SET NULL releases dropped references, UNIQUE (model_id) rejects a
// claimed id (mapped to 409 below).
func UpdateProvider(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		var req apitypes.UpdateProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}

		// Validate the patch before touching any row, so a bad body
		// changes nothing.
		if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
			return api.WriteError(w, http.StatusBadRequest,
				"Provider name is required")
		}
		if req.BaseURL != nil && !isValidBaseURL(*req.BaseURL) {
			return api.WriteError(w, http.StatusBadRequest,
				"Invalid baseUrl: must be an http(s) URL")
		}
		if req.Models != nil {
			if msg := modelListProblem(*req.Models); msg != "" {
				return api.WriteError(w, http.StatusBadRequest, msg)
			}
		}

		row, err := queries.GetProvider(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("Provider %q not found", id))
		} else if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read provider: %w", err)
		}

		// Apply the patch over the stored row: absent fields keep
		// their values.
		name, baseURL, apiKey := row.Name, row.BaseUrl, row.ApiKey
		if req.Name != nil {
			name = *req.Name
		}
		if req.BaseURL != nil {
			baseURL = *req.BaseURL
		}
		if req.APIKey != nil {
			apiKey = *req.APIKey
		}
		if err := queries.UpdateProvider(r.Context(), db.UpdateProviderParams{
			Name: name, BaseUrl: baseURL, ApiKey: apiKey, ID: id,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("update provider: %w", err)
		}

		if req.Models != nil {
			if err := queries.DeleteProviderModels(r.Context(), id); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("delete provider models: %w", err)
			}
			for _, m := range *req.Models {
				if err := queries.CreateModel(r.Context(), db.CreateModelParams{
					ModelID: m, ProviderID: id,
				}); err != nil {
					// UNIQUE (model_id): another provider owns the id.
					if strings.Contains(err.Error(),
						"UNIQUE constraint failed: models.model_id") {
						return api.WriteError(w, http.StatusConflict,
							fmt.Sprintf("Model %q already belongs to another provider", m))
					}
					_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
					return fmt.Errorf("insert model %s: %w", m, err)
				}
			}
		}

		provider, err := buildProvider(r.Context(), queries, id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}
		return api.WriteJSON(w, http.StatusOK, provider)
	}
}

// modelListProblem returns the 400 message for a models list that
// is not non-empty unique strings; "" when well formed.
func modelListProblem(models []string) string {
	seen := make(map[string]bool, len(models))
	for _, m := range models {
		if strings.TrimSpace(m) == "" {
			return "Model id cannot be empty"
		}
		if seen[m] {
			return fmt.Sprintf("Duplicate model id %q", m)
		}
		seen[m] = true
	}
	return ""
}
