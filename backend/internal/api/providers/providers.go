// Package providers serves the provider catalog: stored connections
// with their custom model ids, plus the fetched system lists. Rows
// are user-created; keys are sealed in the runtime vault and never
// echo back (responses carry hasKey); provider ids are server-minted
// uuids.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/modelcatalog"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// isValidBaseURL: an absolute http(s) URL.
func isValidBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// buildProvider renders one provider response from its stored row;
// hasKey is runtime-vault entry existence.
func buildProvider(ctx context.Context, queries *db.Queries, vaultStore *vault.Store, id string) (apitypes.Provider, error) {
	row, err := queries.GetProvider(ctx, id)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("read provider: %w", err)
	}
	custom, err := DecodeModels(row.Models)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("decode models for provider %q: %w", id, err)
	}
	hasKey, err := vaultStore.Has(ctx, vault.NameRuntime, ProviderEntryName(id))
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("read key state for provider %q: %w", id, err)
	}
	return apitypes.Provider{
		ID:      row.ID,
		Name:    row.Name,
		BaseURL: row.BaseUrl,
		HasKey:  hasKey,
		Models:  custom,
	}, nil
}

// ProviderEntryName is the runtime-vault name of a provider's key
// entry; kindProvider is its kind.
func ProviderEntryName(id string) string { return "provider:" + id }

const kindProvider = "provider"

// providerKey reads the provider's stored key; "" when none is stored
// or the entry does not open — fail-closed at this read.
func providerKey(ctx context.Context, vaultStore *vault.Store, id string) string {
	plaintext, _, err := vaultStore.Get(ctx, vault.NameRuntime, ProviderEntryName(id))
	if err != nil {
		return ""
	}
	return string(plaintext)
}

// DecodeModels parses the providers.models JSON array column: the
// operator's declared entries. Exported for package backend's
// resolver, which reads the column directly.
func DecodeModels(s string) ([]apitypes.ProviderModel, error) {
	if s == "" {
		return []apitypes.ProviderModel{}, nil
	}
	var models []apitypes.ProviderModel
	if err := json.Unmarshal([]byte(s), &models); err != nil {
		return nil, err
	}
	if models == nil {
		models = []apitypes.ProviderModel{}
	}
	return models, nil
}

// encodeModels writes the declared entries back to the column shape,
// canonically sorted by id (a slice marshal cannot fail).
func encodeModels(models []apitypes.ProviderModel) string {
	sorted := append([]apitypes.ProviderModel(nil), models...)
	slices.SortFunc(sorted, func(a, b apitypes.ProviderModel) int {
		return strings.Compare(a.ID, b.ID)
	})
	b, _ := json.Marshal(sorted)
	return string(b)
}

// DecodeDiscoveredWindows parses the providers.discovered_context_windows
// JSON object column: model id to the window its endpoint reported.
// Exported for package backend's resolver, which reads the column
// directly.
func DecodeDiscoveredWindows(s string) (map[string]int, error) {
	if s == "" {
		return map[string]int{}, nil
	}
	var windows map[string]int
	if err := json.Unmarshal([]byte(s), &windows); err != nil {
		return nil, err
	}
	if windows == nil {
		windows = map[string]int{}
	}
	return windows, nil
}

// encodeDiscoveredWindows writes the learned map back to the column
// shape; a map[string]int marshal cannot fail and sorts its keys.
func encodeDiscoveredWindows(windows map[string]int) string {
	b, _ := json.Marshal(windows)
	return string(b)
}

// Window sources, in resolution order: the operator's declaration
// outranks the endpoint's report, which outranks the catalog. "" means
// nothing knows.
const (
	WindowSourceDeclared   = "declared"
	WindowSourceDiscovered = "discovered"
	WindowSourceCatalog    = "catalog"
)

// ResolveWindow answers a model's context window and the tier that
// supplied it, in the order the operator controls: the model entry's
// declared window, then the endpoint-discovered map, then the catalog.
// A zero window means nothing knows — callers decide whether that is a
// failure or a report.
func ResolveWindow(declared []apitypes.ProviderModel, discovered map[string]int, catalog *modelcatalog.Catalog, id string) (int, string) {
	for _, model := range declared {
		if model.ID == id && model.ContextWindow > 0 {
			return model.ContextWindow, WindowSourceDeclared
		}
	}
	if window := discovered[id]; window > 0 && window <= apitypes.MaxModelContextWindow {
		return window, WindowSourceDiscovered
	}
	if window, ok := catalog.Lookup(id); ok {
		return window, WindowSourceCatalog
	}
	return 0, ""
}
