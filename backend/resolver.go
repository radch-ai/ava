package backend

import (
	"context"
	"fmt"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// ResolvedModel is a model selection resolved to a callable endpoint:
// the provider's base URL, its unsealed API key, and the model id.
// In-process only — the key never crosses a wire.
type ResolvedModel struct {
	BaseURL string
	APIKey  string
	Model   string
}

// Store handles ResolveModel runs over; New binds them at boot,
// before the gateway serves.
var (
	resolveQueries    *db.Queries
	resolveVaultStore *vault.Store
)

// bindResolver arms ResolveModel with the handles New already holds.
func bindResolver(queries *db.Queries, vaultStore *vault.Store) {
	resolveQueries = queries
	resolveVaultStore = vaultStore
}

// ResolveModel maps a model selection to its endpoint — the function
// the gateway calls directly on this package. A nil entry asks for
// default routing and fails until a default exists. The pair is
// trusted as given: stored model lists are custom ids only — fetched
// system models are never stored — so membership is not checked.
func ResolveModel(ctx context.Context, entry *apitypes.ModelEntry) (ResolvedModel, error) {
	if entry == nil {
		return ResolvedModel{}, fmt.Errorf("no default model configured")
	}
	row, err := resolveQueries.GetProvider(ctx, entry.ProviderID)
	if err != nil {
		return ResolvedModel{}, fmt.Errorf("read provider %s: %w", entry.ProviderID, err)
	}
	key, _, err := resolveVaultStore.Get(ctx, vault.NameRuntime, providers.ProviderEntryName(entry.ProviderID))
	if err != nil {
		return ResolvedModel{}, fmt.Errorf("read key for provider %s: %w", entry.ProviderID, err)
	}
	return ResolvedModel{BaseURL: row.BaseUrl, APIKey: string(key), Model: entry.ID}, nil
}
