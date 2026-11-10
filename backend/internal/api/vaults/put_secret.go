// PUT /api/vault/{name} - create or replace one user secret. The body
// is write-only: no endpoint ever returns the value.
package vaults

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/vault"
)

func PutSecret(vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.PutSecretRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}

		name := strings.TrimSpace(r.PathValue("name"))
		if strings.TrimSpace(req.Kind) == "" {
			return api.WriteError(w, http.StatusBadRequest, "Secret kind is required")
		}
		if req.Value == "" {
			return api.WriteError(w, http.StatusBadRequest, "Secret value is required")
		}

		if err := vaultStore.Put(r.Context(), vault.NameSecrets,
			name, req.Kind, []byte(req.Value), req.Meta); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("seal secret %q: %w", name, err)
		}

		return api.WriteJSON(w, http.StatusOK, apitypes.PutSecretResponse{Message: "Secret saved"})
	}
}
