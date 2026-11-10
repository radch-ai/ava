// GET /api/vault - list user-secret metadata: names, kinds and meta.
// Values never travel back; change means replace.
package vaults

import (
	"encoding/json"
	"net/http"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/vault"
)

func GetVault(vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		entries, err := vaultStore.List(r.Context(), vault.NameSecrets)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}
		secrets := make([]apitypes.SecretMeta, 0, len(entries))
		for _, e := range entries {
			secrets = append(secrets, apitypes.SecretMeta{
				Name: e.Name,
				Kind: e.Kind,
				Meta: jsonRaw(e.Meta),
			})
		}
		return api.WriteJSON(w, http.StatusOK, apitypes.GetVaultResponse{Secrets: secrets})
	}
}

// jsonRaw treats empty stored meta as absent.
func jsonRaw(meta []byte) json.RawMessage {
	if len(meta) == 0 {
		return nil
	}
	return json.RawMessage(meta)
}
