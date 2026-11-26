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
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// UpdateProvider applies absent-keeps patch semantics: name,
// baseUrl, apiKey, and the wholesale models replace. Every check
// (shape, ownership, connection) runs before any write, so a
// rejected patch changes nothing.
func UpdateProvider(queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		r.Body = http.MaxBytesReader(w, r.Body, maxProviderBodyBytes)
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
		name, baseURL, modelsJSON := row.Name, row.BaseUrl, row.Models
		apiKey := providerKey(r.Context(), vaultStore, id)
		custom, err := DecodeModels(modelsJSON)
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
		probed := req.BaseURL != nil || req.APIKey != nil
		if probed {
			if err := probeConnection(r.Context(), baseURL, apiKey); err != nil {
				return api.WriteError(w, http.StatusBadRequest,
					"Connection check failed: "+err.Error())
			}
		}

		if err := queries.UpdateProvider(r.Context(), db.UpdateProviderParams{
			Name: name, BaseUrl: baseURL,
			Models: encodeModels(custom), ID: id,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("update provider %q: %w", id, err)
		}

		// A repointed baseUrl orphans the limits the departed endpoint
		// stated in overflow errors: clear them so nothing stale speaks
		// for the row. Best-effort, like the overflow write itself.
		if req.BaseURL != nil && *req.BaseURL != row.BaseUrl {
			_ = queries.SetDiscoveredContextWindows(r.Context(), db.SetDiscoveredContextWindowsParams{
				DiscoveredContextWindows: encodeDiscoveredWindows(map[string]int{}),
				ID:                       id,
			})
		}

		// The key entry follows the row: replace on a supplied key,
		// remove on an explicit empty one.
		if req.APIKey != nil {
			if *req.APIKey == "" {
				if err := vaultStore.Delete(r.Context(), vault.NameRuntime,
					ProviderEntryName(id)); err != nil && !errors.Is(err, vault.ErrNotFound) {
					_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
					return fmt.Errorf("remove key for provider %q: %w", id, err)
				}
			} else if err := vaultStore.Put(r.Context(), vault.NameRuntime,
				ProviderEntryName(id), kindProvider, []byte(*req.APIKey), nil); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("seal key for provider %q: %w", id, err)
			}
		}

		provider, err := buildProvider(r.Context(), queries, vaultStore, id)
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
func ownershipProblem(ctx context.Context, queries *db.Queries, id string, models []apitypes.ProviderModel) (string, error) {
	rows, err := queries.ListProviders(ctx)
	if err != nil {
		return "", fmt.Errorf("list providers: %w", err)
	}
	for _, row := range rows {
		if row.ID == id {
			continue
		}
		custom, err := DecodeModels(row.Models)
		if err != nil {
			return "", fmt.Errorf("decode models for provider %q: %w", row.ID, err)
		}
		for _, candidate := range models {
			for _, existing := range custom {
				if existing.ID == candidate.ID {
					return fmt.Sprintf("Model %q already belongs to another provider", candidate.ID), nil
				}
			}
		}
	}
	return "", nil
}

// modelListProblem returns the 400 message for a models list that
// is not unique, non-empty ids with sane windows; "" when well formed.
func modelListProblem(models []apitypes.ProviderModel) string {
	if len(models) > maxProviderModels {
		return fmt.Sprintf("Too many model entries (max %d)", maxProviderModels)
	}
	seen := make(map[string]bool, len(models))
	for _, m := range models {
		if strings.TrimSpace(m.ID) == "" {
			return "Model id cannot be empty"
		}
		if seen[m.ID] {
			return fmt.Sprintf("Duplicate model id %q", m.ID)
		}
		seen[m.ID] = true
		if m.ContextWindow < 0 {
			return "Context window must be a positive token count"
		}
		if m.ContextWindow > apitypes.MaxModelContextWindow {
			return fmt.Sprintf("Context window must be at most %d tokens", apitypes.MaxModelContextWindow)
		}
	}
	return ""
}

// Provider patch bodies stay small: one row is a handful of fields and
// a bounded model list, so a fast oversized upload fails the decode
// instead of costing memory.
const maxProviderBodyBytes = 1 << 20

// One wholesale models replace stays cheap to validate and store:
// entries are bounded, and a declared window is a real token count
// under apitypes.MaxModelContextWindow.
const maxProviderModels = 512
