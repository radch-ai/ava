// System-model resolution: a provider connection's live model list
// over its OpenAI-compatible /models endpoint, cached briefly, plus the
// save-time connection probe. Shared by the read endpoints (get_models,
// get_provider_models), the save handlers' probe, and the resolver's
// last tier, so it lives here as one helper instead of being
// duplicated per file. System models are never stored as rows, and a
// listing's stated windows are never persisted — the resolver reads
// them live; only limits learned from overflow errors persist.
// Keyed and keyless ride the same path: the key rides the
// Authorization header when there is one; a no-auth endpoint serves
// its list either way, an auth-required one rejects keyless.
package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/radch-ai/ava/backend/apitypes"
)

// modelInfo is one model the endpoint listed: its id and the context
// window it stated (0 = the listing said nothing about capacity).
type modelInfo struct {
	ID     string
	Window int
}

// modelCacheEntry is one cached fetch: the listing on success, the
// failure itself on error — an entry with err set is a miss that
// expires fast.
type modelCacheEntry struct {
	models  []modelInfo
	err     error
	fetched time.Time
}

var (
	modelCacheMu sync.Mutex
	modelCache   = map[string]modelCacheEntry{}
)

// Bounds one listing fetch: a provider answers /models with a few
// hundred small objects, so the cap is generous against a hostile body
// while staying far below a memory risk.
const (
	modelsFetchTimeout = 10 * time.Second
	maxModelsBodyBytes = 8 << 20
)

// resolvedModels returns the live model list, cached for a short
// window so selector, expand, and save-time probes don't re-hit the
// provider; a failed fetch is cached briefly too, so an unreachable
// endpoint cannot stall every resolution with a full-length timeout.
// The key carries a digest of the provider key, never the key itself:
// the cache index holds no spelling of a secret, and a rotated key
// hashes differently so its stale entry just expires.
func resolvedModels(ctx context.Context, baseURL, apiKey string) ([]modelInfo, error) {
	const (
		cacheTTL     = 5 * time.Minute
		cacheFailTTL = 30 * time.Second
	)
	key := baseURL + "\x00" + keyDigest(apiKey)

	modelCacheMu.Lock()
	entry, fresh := modelCache[key]
	modelCacheMu.Unlock()
	if fresh {
		if entry.err != nil && time.Since(entry.fetched) < cacheFailTTL {
			return nil, entry.err
		}
		if entry.err == nil && time.Since(entry.fetched) < cacheTTL {
			return entry.models, nil
		}
	}

	models, err := listModels(ctx, baseURL, apiKey)
	if err != nil {
		modelCacheMu.Lock()
		modelCache[key] = modelCacheEntry{err: err, fetched: time.Now()}
		modelCacheMu.Unlock()
		return nil, err
	}
	modelCacheMu.Lock()
	modelCache[key] = modelCacheEntry{models: models, fetched: time.Now()}
	modelCacheMu.Unlock()
	return models, nil
}

// keyDigest folds the provider key into eight hex bytes.
func keyDigest(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:8])
}

// evictModelCache drops the cached listings for one base URL: a
// deleted provider leaves no cached copy behind.
func evictModelCache(baseURL string) {
	prefix := baseURL + "\x00"
	modelCacheMu.Lock()
	for key := range modelCache {
		if strings.HasPrefix(key, prefix) {
			delete(modelCache, key)
		}
	}
	modelCacheMu.Unlock()
}

// windowFields are the aliases OpenAI-compatible endpoints use for a
// model's context window. A listing that states several is taken at
// its smallest: the request must fit the tightest stated bound.
var windowFields = [...]string{"context_length", "max_context_length", "max_model_len"}

// listModels lists the provider's models over its OpenAI-compatible
// endpoint; failures map to the connection-check reasons (rejection vs
// anything else the provider said vs unreachable). Each entry carries
// the context window the listing stated, when it stated one.
func listModels(ctx context.Context, baseURL, apiKey string) ([]modelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", baseURL, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := (&http.Client{Timeout: modelsFetchTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf(
			"endpoint rejected the request (%d) — key missing or invalid",
			resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}

	var page struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxModelsBodyBytes)).Decode(&page); err != nil {
		return nil, fmt.Errorf("could not read the model list from %s: %w", baseURL, err)
	}
	models := make([]modelInfo, 0, len(page.Data))
	for _, entry := range page.Data {
		var id string
		if raw, ok := entry["id"]; !ok {
			continue
		} else if err := json.Unmarshal(raw, &id); err != nil || id == "" {
			continue
		}
		models = append(models, modelInfo{ID: id, Window: reportedWindow(entry)})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

// reportedWindow is the entry's stated context window: the smallest
// positive value among the aliases, 0 when the listing stated none.
// Fields are read as numbers and nothing else — a string that happens
// to look like a count is not a capacity.
func reportedWindow(entry map[string]json.RawMessage) int {
	smallest := 0
	for _, field := range windowFields {
		raw, ok := entry[field]
		if !ok {
			continue
		}
		var tokens float64
		if err := json.Unmarshal(raw, &tokens); err != nil || tokens <= 0 ||
			tokens > apitypes.MaxModelContextWindow {
			continue
		}
		if window := int(tokens); smallest == 0 || window < smallest {
			smallest = window
		}
	}
	return smallest
}

// systemModels returns a provider's system models: the live list,
// cached briefly. Fetch failures serve nil; read paths never surface
// the error.
func systemModels(ctx context.Context, baseURL, apiKey string) []modelInfo {
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

// ListingWindows answers the context windows a provider's live listing
// stated, one entry per model that named a capacity — the resolver's
// last tier, taken only when every local source missed. Rides the
// same brief cache as the read paths; a fetch failure answers nil.
func ListingWindows(ctx context.Context, baseURL, apiKey string) map[string]int {
	return reportedWindows(systemModels(ctx, baseURL, apiKey))
}

// reportedWindows is the window map a listing stated: one entry per
// model that named a capacity.
func reportedWindows(models []modelInfo) map[string]int {
	reported := make(map[string]int, len(models))
	for _, model := range models {
		if model.Window > 0 {
			reported[model.ID] = model.Window
		}
	}
	return reported
}

// systemModelIDs is the listing's ids in endpoint order — the selector
// and the settings expand speak in ids.
func systemModelIDs(models []modelInfo) []string {
	ids := make([]string, len(models))
	for i, model := range models {
		ids[i] = model.ID
	}
	return ids
}
