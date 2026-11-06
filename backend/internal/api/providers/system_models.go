// System-model resolution: a provider connection's live model list
// via the official openai-go SDK, cached briefly, plus the save-time
// connection probe. Shared by the read endpoints (get_models,
// get_provider_models) and the save handlers (create, update), so it
// lives here as one helper instead of being duplicated per file.
// System models are never stored — they exist only once fetched.
// Keyed and keyless ride the same path: the SDK attaches the key
// when there is one; a no-auth endpoint serves its list either way,
// an auth-required one rejects the keyless request. When model calls
// land they ride the same client.
package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// modelCacheEntry is one cached live fetch.
type modelCacheEntry struct {
	models  []string
	fetched time.Time
}

var (
	modelCacheMu sync.Mutex
	modelCache   = map[string]modelCacheEntry{}
)

// resolvedModels returns the live model list, cached for a short
// window so selector, expand, and save-time probes don't re-hit the
// provider.
func resolvedModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	const cacheTTL = 5 * time.Minute
	key := baseURL + "\x00" + apiKey

	modelCacheMu.Lock()
	entry, fresh := modelCache[key]
	modelCacheMu.Unlock()
	if fresh && time.Since(entry.fetched) < cacheTTL {
		return entry.models, nil
	}

	models, err := listModels(ctx, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	modelCacheMu.Lock()
	modelCache[key] = modelCacheEntry{models: models, fetched: time.Now()}
	modelCacheMu.Unlock()
	return models, nil
}

// listModels lists the provider's models over its OpenAI-compatible
// endpoint via the SDK client; SDK failures map to the
// connection-check reasons (rejection vs anything else the provider
// said vs unreachable).
func listModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(strings.TrimSuffix(baseURL, "/")),
		option.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)

	page, err := client.Models.List(ctx)
	if err != nil {
		var apiErr *openai.Error
		if errors.As(err, &apiErr) {
			if apiErr.StatusCode == http.StatusUnauthorized ||
				apiErr.StatusCode == http.StatusForbidden {
				return nil, fmt.Errorf(
					"endpoint rejected the request (%d) — key missing or invalid",
					apiErr.StatusCode)
			}
			return nil, fmt.Errorf("provider returned HTTP %d", apiErr.StatusCode)
		}
		return nil, fmt.Errorf("could not reach %s: %w", baseURL, err)
	}
	models := make([]string, 0, len(page.Data))
	for _, m := range page.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

// systemModels returns a provider's system model ids: the live list,
// cached briefly. Fetch failures serve nil; read paths never surface
// the error.
func systemModels(ctx context.Context, baseURL, apiKey string) []string {
	models, _ := resolvedModels(ctx, baseURL, apiKey)
	return models
}

// probeConnection verifies a connection: the live model list must be
// reachable and parseable. Save paths run it before writing; a
// non-nil error means the save must not happen.
func probeConnection(ctx context.Context, baseURL, apiKey string) error {
	_, err := resolvedModels(ctx, baseURL, apiKey)
	return err
}
