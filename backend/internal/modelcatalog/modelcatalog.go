// Package modelcatalog answers a model's context window from the
// vendored models.dev snapshot, refreshed from the network when one is
// configured. It is the last tier of context-window resolution: a model
// the operator did not declare and the endpoint did not size may still
// be known here.
package modelcatalog

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
)

const (
	// fetchTimeout bounds one catalog GET: the snapshot is several MB.
	fetchTimeout = 10 * time.Second
	// catalogMaxBytes caps one catalog body (download or cache file):
	// the snapshot is a few MB, so anything larger is an attack or
	// corruption, not a catalog.
	catalogMaxBytes = 32 << 20
	// refreshInterval spaces background refreshes: a miss is rare, and
	// providers publish models at model-release pace.
	refreshInterval = 24 * time.Hour
	// cacheFileMode keeps the snapshot private to this process.
	cacheFileMode = 0o600
)

//go:embed catalog.json
var embeddedCatalog []byte

// Catalog is a model id → context-window map plus a suffix index for
// bare ids. A nil catalog is usable: it knows nothing.
type Catalog struct {
	mu          sync.Mutex
	exact       map[string]int
	suffix      map[string]int
	url         string
	cachePath   string
	logger      zerolog.Logger
	lastRefresh time.Time
}

// Load builds the catalog from the embedded snapshot. A non-empty url
// fetches a fresh one; success replaces the snapshot and persists it to
// cachePath (empty skips persistence), failure logs and prefers a
// readable cache over the embedded copy. An empty url never fetches.
func Load(url, cachePath string, logger zerolog.Logger) (*Catalog, error) {
	exact, err := decodeFlat(embeddedCatalog)
	if err != nil {
		return nil, fmt.Errorf("parse embedded model catalog: %w", err)
	}
	c := &Catalog{url: url, cachePath: cachePath, logger: logger}
	c.set(exact)
	if url == "" {
		return c, nil
	}

	fetched, err := fetch(url)
	if err != nil {
		logger.Warn().Err(err).Str("url", redactURL(url)).
			Msg("model catalog fetch failed — using the last known snapshot")
		// The attempt counts as this process's refresh: a lookup that
		// misses right after boot must not download a second time. A
		// readable cache with at least one window outranks the embedded
		// snapshot; a windowless one is a miss — the fetch path's own
		// invariant, so drift cannot discard a good snapshot. The next
		// successful fetch replaces both.
		c.stampRefresh()
		if cached, cacheErr := readCache(cachePath); cacheErr == nil && len(cached) > 0 {
			c.set(cached)
		}
		return c, nil
	}
	c.set(fetched)
	c.stampRefresh()
	if err := writeCache(cachePath, fetched); err != nil {
		logger.Warn().Err(err).Str("path", cachePath).
			Msg("model catalog cache write failed")
	}
	return c, nil
}

// NewFromMap builds a catalog from a literal map — tests, no I/O.
func NewFromMap(m map[string]int) *Catalog {
	exact := make(map[string]int, len(m))
	for id, window := range m {
		exact[id] = window
	}
	c := &Catalog{}
	c.set(exact)
	return c
}

// Lookup resolves a model id's context window: an exact hit first, then
// the id's last segment (vendor-prefixed ids differ per provider). A
// miss schedules one background refresh when a url is configured; this
// call never waits on the network.
func (c *Catalog) Lookup(id string) (int, bool) {
	if c == nil || id == "" {
		return 0, false
	}
	c.mu.Lock()
	window, ok := c.exact[id]
	if !ok {
		window, ok = c.suffix[suffixOf(id)]
	}
	refresh := false
	if !ok && c.url != "" && time.Since(c.lastRefresh) >= refreshInterval {
		c.lastRefresh = time.Now()
		refresh = true
	}
	c.mu.Unlock()
	if ok {
		return window, true
	}
	if refresh {
		go c.refresh()
	}
	return 0, false
}

func (c *Catalog) set(exact map[string]int) {
	suffix := buildSuffix(exact)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exact = exact
	c.suffix = suffix
}

// stampRefresh records a fetch attempt as this process's last refresh.
func (c *Catalog) stampRefresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastRefresh = time.Now()
}

// redactURL strips userinfo from a configured catalog URL before it
// reaches a log line.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Redacted()
}

// refresh re-fetches the snapshot once, off the lookup path.
func (c *Catalog) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	fetched, err := fetchContext(ctx, c.url)
	if err != nil {
		c.logger.Warn().Err(err).Str("url", redactURL(c.url)).Msg("model catalog refresh failed")
		return
	}
	c.set(fetched)
	if err := writeCache(c.cachePath, fetched); err != nil {
		c.logger.Warn().Err(err).Str("path", c.cachePath).
			Msg("model catalog cache write failed")
	}
}

// suffixOf is the segment after the last slash — the id vendors
// decorate first.
func suffixOf(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// buildSuffix indexes bare ids. A suffix two ids share with different
// windows is ambiguous and dropped: no match beats a wrong one.
func buildSuffix(exact map[string]int) map[string]int {
	suffix := make(map[string]int, len(exact))
	ambiguous := make(map[string]bool, 0)
	for id, window := range exact {
		key := suffixOf(id)
		if ambiguous[key] {
			continue
		}
		if prev, ok := suffix[key]; ok {
			if prev != window {
				delete(suffix, key)
				ambiguous[key] = true
			}
			continue
		}
		suffix[key] = window
	}
	return suffix
}

// decodeFlat parses the flat map shape: model id → context window.
// Values that cannot be a capacity (non-positive, or past the exported
// ceiling) are dropped so no consumer ever sees them as a hit.
func decodeFlat(body []byte) (map[string]int, error) {
	var flat map[string]int
	if err := json.Unmarshal(body, &flat); err != nil {
		return nil, err
	}
	filtered := make(map[string]int, len(flat))
	for id, window := range flat {
		if window <= 0 || window > apitypes.MaxModelContextWindow {
			continue
		}
		filtered[id] = window
	}
	return filtered, nil
}

// modelsDevProvider is models.dev's per-provider shape, pruned to the
// one field resolution reads.
type modelsDevProvider struct {
	Models map[string]struct {
		Limit struct {
			Context *int `json:"context"`
		} `json:"limit"`
	} `json:"models"`
}

// prune flattens models.dev's provider → models → limit.context tree.
// Values that cannot be a capacity are skipped, and a model id two
// providers disagree about keeps the smallest — the tightest stated
// bound governs, matching the listing rule. A snapshot with no windows
// at all is schema drift, not an empty catalog — refusing it keeps a
// good snapshot in place.
func prune(body []byte) (map[string]int, error) {
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	flat := make(map[string]int, len(providers)*8)
	for _, provider := range providers {
		for id, model := range provider.Models {
			if model.Limit.Context == nil {
				continue
			}
			context := *model.Limit.Context
			if context <= 0 || context > apitypes.MaxModelContextWindow {
				continue
			}
			if prev, ok := flat[id]; !ok || context < prev {
				flat[id] = context
			}
		}
	}
	if len(flat) == 0 {
		return nil, errors.New("catalog snapshot carried no model context windows")
	}
	return flat, nil
}

func fetch(url string) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	return fetchContext(ctx, url)
}

func fetchContext(ctx context.Context, url string) (map[string]int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: fetchTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog endpoint returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > catalogMaxBytes {
		return nil, errors.New("catalog body exceeds the size cap")
	}
	return prune(body)
}

// readCache parses the persisted snapshot; no path is a miss. The
// cache directory is operator-owned, so a symlink here is the
// operator's own file; the read is capped like the download.
func readCache(path string) (map[string]int, error) {
	if path == "" {
		return nil, errors.New("no cache path")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, catalogMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > catalogMaxBytes {
		return nil, errors.New("catalog cache exceeds the size cap")
	}
	return decodeFlat(body)
}

// writeCache persists the snapshot atomically: a reader never sees a
// half-written file, and an empty path skips persistence.
func writeCache(path string, flat map[string]int) error {
	if path == "" {
		return nil
	}
	body, err := json.Marshal(flat)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), cacheFileMode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
