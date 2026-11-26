package modelcatalog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
)

// snapshot serves models.dev's provider → models → limit.context shape:
// one sized model, one with a null context, one with no limit block.
const snapshot = `{
  "openrouter": {
    "models": {
      "anthropic/claude-sonnet-4": {"limit": {"context": 200000, "output": 64000}},
      "lab/null-context": {"limit": {"context": null}},
      "lab/no-limit": {}
    }
  },
  "openai": {
    "models": {
      "openai/gpt-4o": {"limit": {"context": 128000}}
    }
  }
}`

// counter serves the snapshot (or a custom reply) and counts hits.
type counter struct {
	hits  atomic.Int64
	reply func(w http.ResponseWriter, r *http.Request)
}

func (c *counter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.hits.Add(1)
	if c.reply != nil {
		c.reply(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(snapshot))
}

func TestLookupExactSuffixAndAmbiguity(t *testing.T) {
	c := NewFromMap(map[string]int{
		"vendor-a/shared-model":   1000,
		"vendor-b/shared-model":   2000, // same suffix, different window: dropped
		"vendor-c/sample-model":   4000,
		"vendor-d/sample-model":   4000, // same suffix, same window: kept
		"vendor-e/unique-model":   8000,
		"vendor-f/ambiguous-only": 9000,
		"vendor-g/ambiguous-only": 9001,
	})

	cases := []struct {
		id     string
		window int
		ok     bool
	}{
		{"vendor-a/shared-model", 1000, true},
		{"vendor-c/sample-model", 4000, true},
		{"vendor-e/unique-model", 8000, true},
		{"unique-model", 8000, true},
		{"sample-model", 4000, true},
		{"shared-model", 0, false},
		{"ambiguous-only", 0, false},
		{"nobody/knows-this", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		window, ok := c.Lookup(tc.id)
		if window != tc.window || ok != tc.ok {
			t.Errorf("Lookup(%q) = (%d, %t), want (%d, %t)", tc.id, window, ok, tc.window, tc.ok)
		}
	}
}

func TestNilCatalogLookupMisses(t *testing.T) {
	var c *Catalog
	if window, ok := c.Lookup("openai/gpt-4o"); ok || window != 0 {
		t.Fatalf("nil Lookup = (%d, %t), want (0, false)", window, ok)
	}
}

func TestEmbeddedSnapshotParses(t *testing.T) {
	flat, err := decodeFlat(embeddedCatalog)
	if err != nil {
		t.Fatalf("decode embedded snapshot: %v", err)
	}
	if len(flat) == 0 {
		t.Fatal("embedded snapshot carries no models")
	}
	for id, window := range flat {
		if window <= 0 || window > apitypes.MaxModelContextWindow {
			t.Fatalf("embedded snapshot carries %q with impossible window %d", id, window)
		}
	}

	c, err := Load("", "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c == nil {
		t.Fatal("Load returned a nil catalog")
	}

	// Pin one canonical entry by content: the first anthropic/ id in the
	// snapshot with the window the file states. Re-vendoring may change
	// which model this is; the vendor prefix is the fixed anchor.
	var anchorID string
	var anchorWindow int
	for id, window := range flat {
		if strings.HasPrefix(id, "anthropic/") && (anchorID == "" || id < anchorID) {
			anchorID, anchorWindow = id, window
		}
	}
	if anchorID == "" {
		t.Fatal("embedded snapshot carries no anthropic/ model")
	}
	if got, ok := c.Lookup(anchorID); !ok || got != anchorWindow {
		t.Fatalf("Lookup(%q) = (%d, %t), want (%d, true)", anchorID, got, ok, anchorWindow)
	}
}

func TestLoadFetchesPrunesAndPersists(t *testing.T) {
	srv := httptest.NewServer(&counter{})
	defer srv.Close()
	cachePath := filepath.Join(t.TempDir(), "catalog.json")

	c, err := Load(srv.URL, cachePath, zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, ok := c.Lookup("anthropic/claude-sonnet-4"); !ok || got != 200000 {
		t.Fatalf("fetched snapshot lookup = (%d, %t), want (200000, true)", got, ok)
	}
	if _, ok := c.Lookup("lab/null-context"); ok {
		t.Fatal("null context was kept")
	}
	if _, ok := c.Lookup("lab/no-limit"); ok {
		t.Fatal("model without a limit was kept")
	}

	body, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var persisted map[string]int
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatalf("parse cache: %v", err)
	}
	if len(persisted) != 2 || persisted["openai/gpt-4o"] != 128000 {
		t.Fatalf("cache = %v, want the two sized models", persisted)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if perm := info.Mode().Perm(); perm != cacheFileMode {
		t.Fatalf("cache mode = %o, want %o", perm, cacheFileMode)
	}
}

func TestLoadEmptyURLNeverFetches(t *testing.T) {
	handler := &counter{}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cachePath, []byte(`{"cache-only/model": 1234}`), 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	c, err := Load("", cachePath, zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := handler.hits.Load(); got != 0 {
		t.Fatalf("fetch hits = %d, want 0", got)
	}
	if _, ok := c.Lookup("cache-only/model"); ok {
		t.Fatal("cache was read with an empty url")
	}
	flat, err := decodeFlat(embeddedCatalog)
	if err != nil {
		t.Fatalf("decode embedded snapshot: %v", err)
	}
	if len(flat) == 0 {
		t.Fatal("embedded snapshot carries no models")
	}
	for id, window := range flat {
		if got, ok := c.Lookup(id); !ok || got != window {
			t.Fatalf("embedded lookup(%q) = (%d, %t), want (%d, true)", id, got, ok, window)
		}
		break
	}
}

func TestLoadFallsBackToCacheOnFetchFailure(t *testing.T) {
	srv := httptest.NewServer(&counter{reply: func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}})
	defer srv.Close()
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cachePath, []byte(`{"cached/model": 4321}`), 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	c, err := Load(srv.URL, cachePath, zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, ok := c.Lookup("cached/model"); !ok || got != 4321 {
		t.Fatalf("cache fallback lookup = (%d, %t), want (4321, true)", got, ok)
	}
}

// A windowless cache file is a miss, not a catalog: the embedded
// snapshot stays in place when the boot fetch fails and the cache
// decodes to nothing usable.
func TestLoadCacheFallbackRejectsWindowlessCache(t *testing.T) {
	srv := httptest.NewServer(&counter{reply: func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}})
	defer srv.Close()
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cachePath, []byte(`{"zero/a":0}`), 0o600); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	c, err := Load(srv.URL, cachePath, zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	embedded, err := decodeFlat(embeddedCatalog)
	if err != nil {
		t.Fatalf("decode embedded: %v", err)
	}
	for id, window := range embedded {
		if got, ok := c.Lookup(id); !ok || got != window {
			t.Fatalf("embedded id %s lost to a windowless cache (ok=%t window=%d)", id, ok, got)
		}
		break
	}
}

func TestLoadRejectsWindowlessSnapshots(t *testing.T) {
	srv := httptest.NewServer(&counter{reply: func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"drift": {"models": {"x/y": {}}}}`))
	}})
	defer srv.Close()

	c, err := Load(srv.URL, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := c.Lookup("x/y"); ok {
		t.Fatal("windowless snapshot replaced the catalog")
	}
	flat, err := decodeFlat(embeddedCatalog)
	if err != nil {
		t.Fatalf("decode embedded snapshot: %v", err)
	}
	for id, window := range flat {
		if got, ok := c.Lookup(id); !ok || got != window {
			t.Fatalf("embedded lookup(%q) = (%d, %t), want (%d, true)", id, got, ok, window)
		}
		break
	}
}

func TestLookupMissSchedulesOneRefresh(t *testing.T) {
	handler := &counter{}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, err := Load(srv.URL, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := handler.hits.Load(); got != 1 {
		t.Fatalf("load hits = %d, want 1", got)
	}

	// The boot fetch counts as this process's refresh: a miss right
	// after boot must not download again.
	if _, ok := c.Lookup("nobody/knows-this"); ok {
		t.Fatal("unknown id resolved")
	}
	time.Sleep(100 * time.Millisecond)
	if got := handler.hits.Load(); got != 1 {
		t.Fatalf("post-boot miss hits = %d, want 1 — boot fetch is the refresh", got)
	}

	// Once the interval has aged, a miss schedules exactly one refresh.
	c.mu.Lock()
	c.lastRefresh = time.Time{}
	c.mu.Unlock()
	if _, ok := c.Lookup("nobody/knows-this"); ok {
		t.Fatal("unknown id resolved")
	}
	waitFor(t, func() bool { return handler.hits.Load() == 2 })

	// And the refresh resets the interval: the next miss stays quiet.
	if _, ok := c.Lookup("nobody/knows-this"); ok {
		t.Fatal("unknown id resolved after refresh")
	}
	time.Sleep(100 * time.Millisecond)
	if got := handler.hits.Load(); got != 2 {
		t.Fatalf("refresh hits = %d, want 2 — a miss must not refetch within the interval", got)
	}
}

// Two providers declaring one model id with different windows resolve the
// tightest stated bound, deterministically — never Go's map order.
func TestLoadTakesSmallestOnCrossProviderCollision(t *testing.T) {
	body := `{
		"provider-a": {"models": {"shared/model": {"limit": {"context": 1000000}}}},
		"provider-b": {"models": {"shared/model": {"limit": {"context": 131072}}}}
	}`
	srv := httptest.NewServer(&counter{reply: func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}})
	defer srv.Close()

	for i := 0; i < 5; i++ {
		c, err := Load(srv.URL, "", zerolog.Nop())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got, ok := c.Lookup("shared/model"); !ok || got != 131072 {
			t.Fatalf("collision lookup = (%d, %t), want (131072, true)", got, ok)
		}
	}
}

// A catalog body past the cap is refused, never buffered whole — even
// when it is well-formed and would otherwise parse.
func TestLoadRejectsOversizeBody(t *testing.T) {
	pad := strings.Repeat("a", catalogMaxBytes)
	body := `{"pad":"` + pad + `","p":{"models":{"x/y":{"limit":{"context":100}}}}}`
	srv := httptest.NewServer(&counter{reply: func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}})
	defer srv.Close()

	c, err := Load(srv.URL, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := c.Lookup("x/y"); ok {
		t.Fatal("oversize body became the catalog")
	}
	if flat, err := decodeFlat(embeddedCatalog); err != nil || len(flat) == 0 {
		t.Fatalf("embedded snapshot not served after oversize body: %v", err)
	}
}

// Values that cannot be a capacity never resolve, in either flat shape.
func TestDecodeFlatDropsNonPositive(t *testing.T) {
	flat, err := decodeFlat([]byte(`{"zero/model":0,"neg/model":-5,"big/model":99999999,"ok/model":100}`))
	if err != nil {
		t.Fatalf("decodeFlat: %v", err)
	}
	if len(flat) != 1 || flat["ok/model"] != 100 {
		t.Fatalf("decodeFlat = %v, want only ok/model", flat)
	}
}

// A cache carrying junk values serves only the real ones.
func TestLoadCacheFallbackDropsNonPositive(t *testing.T) {
	srv := httptest.NewServer(&counter{reply: func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}})
	defer srv.Close()
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cachePath,
		[]byte(`{"zero/alpha":0,"ok/beta":500}`), 0o600); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	c, err := Load(srv.URL, cachePath, zerolog.Nop())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := c.Lookup("zero/alpha"); ok {
		t.Fatal("zero window resolved from cache")
	}
	if got, ok := c.Lookup("ok/beta"); !ok || got != 500 {
		t.Fatalf("cache fallback lookup = (%d, %t), want (500, true)", got, ok)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
