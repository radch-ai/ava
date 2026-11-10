// DELETE /api/vault/{name} - remove one user secret.
package vaults

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/vault"
)

func DeleteSecret(vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		name := r.PathValue("name")
		if err := vaultStore.Delete(r.Context(), vault.NameSecrets,
			name); err != nil && !errors.Is(err, vault.ErrNotFound) {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("delete secret %q: %w", name, err)
		} else if err != nil {
			return api.WriteError(w, http.StatusNotFound,
				fmt.Sprintf("Secret %q not found", name))
		}

		return api.WriteJSON(w, http.StatusOK, apitypes.DeleteSecretResponse{Message: "Secret deleted"})
	}
}
