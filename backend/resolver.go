package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api/providers"
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

// ResolveModel maps a model selection to its endpoint — the call the
// gateway makes each turn. A nil entry follows the global default
// (the default_model row); none set fails closed. The pair is
// trusted as given: stored model lists are custom ids only — fetched
// system models are never stored — so membership is not checked.
func (s *TurnService) ResolveModel(ctx context.Context, entry *apitypes.ModelEntry) (ResolvedModel, error) {
	if entry == nil {
		def, err := s.queries.GetDefaultModel(ctx)
		if err != nil {
			// No row set is a configuration state, not a failure to
			// read one — it reaches users verbatim.
			if errors.Is(err, sql.ErrNoRows) {
				return ResolvedModel{}, errors.New("no default model set — pick a model in the composer")
			}
			return ResolvedModel{}, fmt.Errorf("read default model: %w", err)
		}
		entry = &apitypes.ModelEntry{ProviderID: def.ProviderID, ID: def.Model}
	}
	row, err := s.queries.GetProvider(ctx, entry.ProviderID)
	if err != nil {
		return ResolvedModel{}, fmt.Errorf("read provider %s: %w", entry.ProviderID, err)
	}
	key, _, err := s.vaultStore.Get(ctx, vault.NameRuntime, providers.ProviderEntryName(entry.ProviderID))
	if err != nil {
		return ResolvedModel{}, fmt.Errorf("read key for provider %s: %w", entry.ProviderID, err)
	}
	return ResolvedModel{BaseURL: row.BaseUrl, APIKey: string(key), Model: entry.ID}, nil
}
