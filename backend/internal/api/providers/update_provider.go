// PATCH /api/providers/{id} - edit a connection and/or replace its
// custom model list wholesale. Patching a system provider's slug
// upserts its attachment row.
package providers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// UpdateProvider applies absent-keeps patch semantics: name,
// baseUrl, apiKey, and the wholesale models replace. Every check
// (shape, ownership, connection) runs before the one atomic
// full-row write, so a rejected patch changes nothing.
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
			return api.WriteError(w, http.StatusBadRequest, "Provider name is required")
		}
		if req.BaseURL != nil && !isValidBaseURL(*req.BaseURL) {
			return api.WriteError(w, http.StatusBadRequest,
				"Invalid baseUrl: must be an http(s) URL")
		}
		if req.Models != nil {
			if problem := modelListProblem(*req.Models); problem != "" {
				return api.WriteError(w, http.StatusBadRequest, problem)
			}
		}

		row, err := queries.GetProvider(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("Provider %q not found", id))
		} else if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read provider %q: %w", id, err)
		}
		name, baseURL, apiKey, modelsJSON := row.Name, row.BaseUrl, row.ApiKey, row.Models
		custom, err := decodeModels(modelsJSON)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("decode models for provider %q: %w", id, err)
		}

		// Apply the patch over the base: absent fields keep their
		// values.
		if req.Name != nil {
			name = *req.Name
		}
		if req.BaseURL != nil {
			baseURL = *req.BaseURL
		}
		if req.APIKey != nil {
			apiKey = *req.APIKey
		}
		if req.Models != nil {
			problem, err := ownershipProblem(r.Context(), queries, id, *req.Models)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
			if problem != "" {
				return api.WriteError(w, http.StatusConflict, problem)
			}
			custom = *req.Models
		}

		// A patch touching the connection re-verifies the effective
		// configuration — keyed or keyless — before saving.
		if req.BaseURL != nil || req.APIKey != nil {
			if err := probeConnection(r.Context(), baseURL, apiKey); err != nil {
				return api.WriteError(w, http.StatusBadRequest,
					"Connection check failed: "+err.Error())
			}
		}

		if err := queries.UpdateProvider(r.Context(), db.UpdateProviderParams{
			Name: name, BaseUrl: baseURL, ApiKey: apiKey,
			Models: encodeModels(custom), ID: id,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("update provider %q: %w", id, err)
		}

		provider, err := buildProvider(r.Context(), queries, id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}
		return api.WriteJSON(w, http.StatusOK, provider)
	}
}

// ownershipProblem returns the 409 message when a candidate id is
// already claimed by another provider's custom list; "" when the
// list is claimable.
func ownershipProblem(ctx context.Context, queries *db.Queries, id string, models []string) (string, error) {
	rows, err := queries.ListProviders(ctx)
	if err != nil {
		return "", fmt.Errorf("list providers: %w", err)
	}
	for _, row := range rows {
		if row.ID == id {
			continue
		}
		custom, err := decodeModels(row.Models)
		if err != nil {
			return "", fmt.Errorf("decode models for provider %q: %w", row.ID, err)
		}
		for _, m := range models {
			if slices.Contains(custom, m) {
				return fmt.Sprintf("Model %q already belongs to another provider", m), nil
			}
		}
	}
	return "", nil
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
