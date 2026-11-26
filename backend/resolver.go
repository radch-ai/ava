package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// ResolvedModel is a model selection resolved to a callable endpoint:
// the provider's base URL, its unsealed API key, the model id, and
// that model's context window in tokens — the capacity budget every
// window-aware decision measures against. ProviderID names the row the
// pair resolved through, so a lane can report a window it learns back
// to discovery. In-process only — the key never crosses a wire.
type ResolvedModel struct {
	ProviderID          string
	BaseURL             string
	APIKey              string
	Model               string
	ContextWindowTokens int
}

// ResolveModel maps a model selection to its endpoint — the call the
// gateway makes each turn. A nil entry follows the global default
// (the default_model row); none set fails closed. The pair is
// trusted as given: stored model lists are custom ids only — fetched
// system models are never stored — so membership is not checked.
//
// The context window is resolved, never assumed: a declared entry,
// then a limit the endpoint stated in an overflow, then the catalog,
// then the endpoint's own live listing — the only tier that costs a
// fetch, taken last so local answers win. Nothing known answers zero
// rather than guessing, because prompt turns consume no window; the
// compact lane names the failure.
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
	declared, err := providers.DecodeModels(row.Models)
	if err != nil {
		return ResolvedModel{}, fmt.Errorf("decode models for provider %s: %w", entry.ProviderID, err)
	}
	discovered, err := providers.DecodeDiscoveredWindows(row.DiscoveredContextWindows)
	if err != nil {
		return ResolvedModel{}, fmt.Errorf("decode discovered windows for provider %s: %w", entry.ProviderID, err)
	}
	window, _ := providers.ResolveWindow(declared, discovered, s.catalog, entry.ID)
	if window <= 0 {
		// Last tier: the endpoint's own listing, fetched live and
		// cached briefly. A failed or windowless fetch is a miss —
		// the compact lane names the model, prompt turns run on zero.
		window, _ = providers.ResolveWindow(declared,
			providers.ListingWindows(ctx, row.BaseUrl, string(key)), s.catalog, entry.ID)
	}
	return ResolvedModel{
		ProviderID:          entry.ProviderID,
		BaseURL:             row.BaseUrl,
		APIKey:              string(key),
		Model:               entry.ID,
		ContextWindowTokens: window,
	}, nil
}

// RecordDiscoveredWindow merges one endpoint-stated window into the
// provider's discovered map — the reactive half of discovery: a request
// that failed against a smaller window than anything known teaches the
// row, so the next turn sizes itself correctly. One statement per
// write, so concurrent discovery writers cannot drop each other's
// entries. Best-effort; the caller is already handling a failure.
func (s *TurnService) RecordDiscoveredWindow(ctx context.Context, providerID, model string, window int) error {
	if providerID == "" || model == "" || window <= 0 ||
		window > apitypes.MaxModelContextWindow {
		return nil
	}
	if err := s.queries.SetDiscoveredContextWindow(ctx, db.SetDiscoveredContextWindowParams{
		Path:   "$." + strconv.Quote(model),
		Window: int64(window),
		ID:     providerID,
	}); err != nil {
		return fmt.Errorf("persist discovered window for provider %s: %w", providerID, err)
	}
	return nil
}
