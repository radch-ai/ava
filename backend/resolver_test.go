package backend_test

// Resolver pins: an explicit pair — or a set default — resolves to
// the provider's endpoint; a missing default, unknown provider, or
// missing key fails closed. The tests call the TurnService methods
// directly — the same method values the gateway binds.

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api/providers"
	"github.com/radch-ai/ava/backend/internal/modelcatalog"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// resolverDB boots a fresh backend over its own seeded database and
// hands back its turn service; catalog is the resolver's last tier.
func resolverDB(t *testing.T, catalog *modelcatalog.Catalog) (*sql.DB, *backend.TurnService) {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), catalog)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	return sqlDB, svc
}

func seedProvider(t *testing.T, sqlDB *sql.DB, id, baseURL string) {
	t.Helper()
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO providers (id, name, base_url) VALUES (?, ?, ?)`,
		id, id+"-name", baseURL); err != nil {
		t.Fatalf("seed provider %s: %v", id, err)
	}
}

// seedProviderModels writes the provider's declared model entries.
func seedProviderModels(t *testing.T, sqlDB *sql.DB, id, models string) {
	t.Helper()
	if _, err := sqlDB.ExecContext(context.Background(),
		`UPDATE providers SET models = ? WHERE id = ?`, models, id); err != nil {
		t.Fatalf("seed models for %s: %v", id, err)
	}
}

func seedProviderKey(t *testing.T, sqlDB *sql.DB, id, key string) {
	t.Helper()
	master, err := vault.NewMasterAEAD(testMasterKey)
	if err != nil {
		t.Fatalf("master AEAD: %v", err)
	}
	store, err := vault.NewStore(sqlDB, master)
	if err != nil {
		t.Fatalf("vault store: %v", err)
	}
	// "provider" mirrors kindProvider in the providers package.
	if err := store.Put(context.Background(), vault.NameRuntime,
		providers.ProviderEntryName(id), "provider", []byte(key), nil); err != nil {
		t.Fatalf("seed key for %s: %v", id, err)
	}
}

func TestResolveModelEndpoint(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	seedProvider(t, sqlDB, "prov-1", "https://api.example.com/v1")
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")
	seedProviderModels(t, sqlDB, "prov-1", `[{"id":"glm-4.7","contextWindow":200000}]`)

	got, err := svc.ResolveModel(context.Background(),
		&apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "prov-1"})
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	want := backend.ResolvedModel{
		ProviderID:          "prov-1",
		BaseURL:             "https://api.example.com/v1",
		APIKey:              "sk-test",
		Model:               "glm-4.7",
		ContextWindowTokens: 200000,
	}
	if got != want {
		t.Errorf("ResolveModel = %+v, want %+v", got, want)
	}
}

// The declared entry supplies the model's capacity budget, the
// endpoint-discovered map fills in an undeclared id, and the catalog
// answers a model neither tier knows — in that order.
func TestResolveModelContextWindowChain(t *testing.T) {
	catalog := modelcatalog.NewFromMap(map[string]int{
		"glm-4.7":      500000,
		"catalog/only": 32000,
	})
	sqlDB, svc := resolverDB(t, catalog)
	seedProvider(t, sqlDB, "prov-1", "https://api.example.com/v1")
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")
	setWindows := func(models, discovered string) {
		t.Helper()
		if _, err := sqlDB.ExecContext(context.Background(),
			`UPDATE providers SET models = ?, discovered_context_windows = ? WHERE id = 'prov-1'`,
			models, discovered); err != nil {
			t.Fatalf("set windows %s / %s: %v", models, discovered, err)
		}
	}
	resolve := func(model string) int {
		t.Helper()
		got, err := svc.ResolveModel(context.Background(),
			&apitypes.ModelEntry{ID: model, ProviderID: "prov-1"})
		if err != nil {
			t.Fatalf("ResolveModel %s: %v", model, err)
		}
		return got.ContextWindowTokens
	}

	setWindows(`[{"id":"glm-4.7","contextWindow":200000}]`,
		`{"other-model":64000}`)
	if got := resolve("glm-4.7"); got != 200000 {
		t.Errorf("declared window = %d, want 200000 (declared outranks the catalog)", got)
	}
	if got := resolve("other-model"); got != 64000 {
		t.Errorf("discovered window = %d, want 64000", got)
	}
	if got := resolve("catalog/only"); got != 32000 {
		t.Errorf("catalog window = %d, want 32000", got)
	}
	if got := resolve("bare/only"); got != 32000 {
		t.Errorf("catalog suffix window = %d, want 32000", got)
	}

	// A discovered window outranks the catalog for the same id.
	setWindows(`[{"id":"glm-4.7","contextWindow":200000}]`, `{"catalog/only":7777}`)
	if got := resolve("catalog/only"); got != 7777 {
		t.Errorf("discovered-over-catalog window = %d, want 7777", got)
	}

	// A declaration outranks a discovered window for the same id.
	setWindows(`[{"id":"glm-4.7","contextWindow":200000}]`, `{"glm-4.7":1000}`)
	if got := resolve("glm-4.7"); got != 200000 {
		t.Errorf("declared-over-discovered window = %d, want 200000", got)
	}

	// A zero declared window is not a declaration.
	setWindows(`[{"id":"glm-4.7","contextWindow":0}]`, `{"glm-4.7":1000}`)
	if got := resolve("glm-4.7"); got != 1000 {
		t.Errorf("zero-declared window = %d, want the discovered 1000", got)
	}
}

// The endpoint's own listing is the last tier: fetched live and cached
// briefly, only when every local source missed. Local answers outrank
// it — the catalog wins for an id both know — and a listing that
// cannot be reached, or states only an input cap, is simply one more
// tier that knows nothing.
func TestResolveModelLearnsWindowFromListing(t *testing.T) {
	listing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"live/model","context_length":96000},
			{"id":"shared/id","context_length":64000},
			{"id":"input-only","max_input_tokens":4096}
		]}`))
	}))
	t.Cleanup(listing.Close)
	catalog := modelcatalog.NewFromMap(map[string]int{"shared/id": 32000})
	sqlDB, svc := resolverDB(t, catalog)
	seedProvider(t, sqlDB, "prov-1", listing.URL)
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")

	window := func(model string) int {
		t.Helper()
		got, err := svc.ResolveModel(context.Background(),
			&apitypes.ModelEntry{ID: model, ProviderID: "prov-1"})
		if err != nil {
			t.Fatalf("ResolveModel %s: %v", model, err)
		}
		return got.ContextWindowTokens
	}
	if got := window("live/model"); got != 96000 {
		t.Errorf("listing window = %d, want 96000", got)
	}
	// Local before network: the catalog's answer outranks the listing's.
	if got := window("shared/id"); got != 32000 {
		t.Errorf("catalog-over-listing window = %d, want 32000", got)
	}
	// An input-only cap is not a window.
	if got := window("input-only"); got != 0 {
		t.Errorf("input-only window = %d, want 0", got)
	}

	// An unreachable endpoint is a miss, never a failed resolution.
	seedProvider(t, sqlDB, "prov-dead", "http://127.0.0.1:1")
	seedProviderKey(t, sqlDB, "prov-dead", "sk-test")
	if _, err := svc.ResolveModel(context.Background(),
		&apitypes.ModelEntry{ID: "any/model", ProviderID: "prov-dead"}); err != nil {
		t.Fatalf("ResolveModel against a dead endpoint must not fail: %v", err)
	}
}

// A window an endpoint states in a rejection is recorded for the next
// resolution — the reactive tier of discovery.
func TestRecordDiscoveredWindow(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	seedProvider(t, sqlDB, "prov-1", "https://api.example.com/v1")
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")
	ctx := context.Background()

	if err := svc.RecordDiscoveredWindow(ctx, "prov-1", "glm-4.7", 8192); err != nil {
		t.Fatalf("RecordDiscoveredWindow: %v", err)
	}
	got, err := svc.ResolveModel(ctx, &apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "prov-1"})
	if err != nil {
		t.Fatalf("ResolveModel after record: %v", err)
	}
	if got.ContextWindowTokens != 8192 {
		t.Errorf("resolved window = %d, want the recorded 8192", got.ContextWindowTokens)
	}

	// A stated window replaces the older one: the endpoint is the most
	// recent authority on its own capacity.
	if err := svc.RecordDiscoveredWindow(ctx, "prov-1", "glm-4.7", 16384); err != nil {
		t.Fatalf("RecordDiscoveredWindow again: %v", err)
	}
	got, err = svc.ResolveModel(ctx, &apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "prov-1"})
	if err != nil {
		t.Fatalf("ResolveModel after second record: %v", err)
	}
	if got.ContextWindowTokens != 16384 {
		t.Errorf("resolved window = %d, want the newer 16384", got.ContextWindowTokens)
	}

	// A declaration still outranks what the endpoint stated.
	seedProviderModels(t, sqlDB, "prov-1", `[{"id":"glm-4.7","contextWindow":200000}]`)
	got, err = svc.ResolveModel(ctx, &apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "prov-1"})
	if err != nil {
		t.Fatalf("ResolveModel with a declaration: %v", err)
	}
	if got.ContextWindowTokens != 200000 {
		t.Errorf("resolved window = %d, want the declared 200000", got.ContextWindowTokens)
	}

	// Nothing to record is not a failure.
	for _, bad := range []struct {
		providerID, model string
		window            int
	}{
		{"", "glm-4.7", 8192},
		{"prov-1", "", 8192},
		{"prov-1", "glm-4.7", 0},
	} {
		if err := svc.RecordDiscoveredWindow(ctx, bad.providerID, bad.model, bad.window); err != nil {
			t.Errorf("RecordDiscoveredWindow(%q, %q, %d) = %v, want nil", bad.providerID, bad.model, bad.window, err)
		}
	}
}

// A recorded window merges into the row one key at a time: concurrent
// discovery writers cannot drop each other's entries, and an
// out-of-range figure is never persisted.
func TestRecordDiscoveredWindowMergesSingleKey(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	seedProvider(t, sqlDB, "prov-1", "https://api.example.com/v1")
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")
	ctx := context.Background()

	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE providers SET discovered_context_windows = '{"older-model":100}' WHERE id = 'prov-1'`); err != nil {
		t.Fatalf("seed windows: %v", err)
	}
	if err := svc.RecordDiscoveredWindow(ctx, "prov-1", "newer-model", 8192); err != nil {
		t.Fatalf("RecordDiscoveredWindow: %v", err)
	}

	read := func() map[string]int {
		t.Helper()
		var raw string
		if err := sqlDB.QueryRowContext(ctx,
			`SELECT discovered_context_windows FROM providers WHERE id = 'prov-1'`).Scan(&raw); err != nil {
			t.Fatalf("read windows: %v", err)
		}
		var got map[string]int
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("parse windows %q: %v", raw, err)
		}
		return got
	}

	want := map[string]int{"older-model": 100, "newer-model": 8192}
	if got := read(); !maps.Equal(got, want) {
		t.Fatalf("windows = %v, want %v — the single-key write must merge", got, want)
	}

	// A stated figure past the ceiling is dropped, never persisted.
	if err := svc.RecordDiscoveredWindow(ctx, "prov-1", "bad-model", 9_999_999_999); err != nil {
		t.Fatalf("RecordDiscoveredWindow oversized: %v", err)
	}
	if got := read(); !maps.Equal(got, want) {
		t.Fatalf("windows = %v, want %v — an out-of-range figure must not persist", got, want)
	}
}

// Nothing knows a model's window: resolution answers zero rather than
// guessing a capacity — and never fails the turn, because prompt turns
// consume no window; only window-aware paths (the compact lane) name the
// failure. A nil catalog is simply one more tier that knows nothing.
func TestResolveModelUnknownWindowIsZero(t *testing.T) {
	// A local listing that states nothing: even the live tier is
	// exercised and misses without leaving the machine.
	listing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"other/model"}]}`))
	}))
	t.Cleanup(listing.Close)
	sqlDB, svc := resolverDB(t, nil)
	seedProvider(t, sqlDB, "prov-1", listing.URL)
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")

	resolved, err := svc.ResolveModel(context.Background(),
		&apitypes.ModelEntry{ID: "nobody/unknown", ProviderID: "prov-1"})
	if err != nil {
		t.Fatalf("ResolveModel unknown window must not fail the turn: %v", err)
	}
	if resolved.ContextWindowTokens != 0 {
		t.Fatalf("window = %d, want 0 — nothing stated a capacity", resolved.ContextWindowTokens)
	}

	// The default-model branch answers the same way: a set default
	// whose model nobody sized resolves to zero, not an error.
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO default_model (id, model, provider_id) VALUES (1, 'nobody/unknown', 'prov-1')`,
	); err != nil {
		t.Fatalf("seed default model: %v", err)
	}
	resolved, err = svc.ResolveModel(context.Background(), nil)
	if err != nil {
		t.Fatalf("ResolveModel default branch must not fail the turn: %v", err)
	}
	if resolved.ContextWindowTokens != 0 {
		t.Fatalf("default branch window = %d, want 0", resolved.ContextWindowTokens)
	}
}

func TestResolveModelNilWithoutDefaultFails(t *testing.T) {
	_, svc := resolverDB(t, nil)
	got, err := svc.ResolveModel(context.Background(), nil)
	if err == nil || got != (backend.ResolvedModel{}) {
		t.Fatalf("ResolveModel nil entry, no default = (%+v, %v), want zero value and an error", got, err)
	}
	// The message tells the user what to do; the SQL plumbing stays
	// server-side.
	if !strings.Contains(err.Error(), "default model") || !strings.Contains(err.Error(), "pick") {
		t.Fatalf("ResolveModel nil entry, no default: error = %q, want an actionable no-default message", err)
	}
	for _, internal := range []string{"sql:", "no rows", "read default model"} {
		if strings.Contains(err.Error(), internal) {
			t.Fatalf("ResolveModel nil entry, no default: error = %q, leaks %q", err, internal)
		}
	}
}

func TestResolveModelNilFollowsDefault(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	seedProvider(t, sqlDB, "prov-1", "https://api.example.com/v1")
	seedProviderKey(t, sqlDB, "prov-1", "sk-test")
	seedProviderModels(t, sqlDB, "prov-1", `[{"id":"glm-4.7","contextWindow":131072}]`)
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO default_model (id, model, provider_id) VALUES (1, 'glm-4.7', 'prov-1')`,
	); err != nil {
		t.Fatalf("seed default model: %v", err)
	}

	got, err := svc.ResolveModel(context.Background(), nil)
	if err != nil {
		t.Fatalf("ResolveModel nil entry with default: %v", err)
	}
	want := backend.ResolvedModel{
		ProviderID:          "prov-1",
		BaseURL:             "https://api.example.com/v1",
		APIKey:              "sk-test",
		Model:               "glm-4.7",
		ContextWindowTokens: 131072,
	}
	if got != want {
		t.Errorf("ResolveModel nil entry = %+v, want %+v (the default pair)", got, want)
	}
}

func TestResolveModelUnknownProviderFails(t *testing.T) {
	_, svc := resolverDB(t, nil)
	entry := &apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "missing-prov"}
	if got, err := svc.ResolveModel(context.Background(), entry); err == nil || got != (backend.ResolvedModel{}) {
		t.Fatalf("ResolveModel unknown provider = (%+v, %v), want zero value and an error", got, err)
	}
}

func TestResolveModelKeylessProviderFails(t *testing.T) {
	sqlDB, svc := resolverDB(t, nil)
	seedProvider(t, sqlDB, "prov-2", "https://api.example.com/v1")
	if got, err := svc.ResolveModel(context.Background(),
		&apitypes.ModelEntry{ID: "glm-4.7", ProviderID: "prov-2"}); err == nil || got != (backend.ResolvedModel{}) {
		t.Fatalf("ResolveModel keyless provider = (%+v, %v), want zero value and an error", got, err)
	}
}
