package providers

// System-model resolution: the live model list over the provider's
// OpenAI-compatible /models endpoint (keyed and keyless alike), a short
// cache, the capacity each entry reports, and the save-time probe. The
// provider speaks the OpenAI /models shape.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeProvider serves the OpenAI models shape: key "k-1" gets the
// live list, anything else gets a 401. Live ids are deliberately
// unsorted to pin the sort.
func fakeProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k-1" {
			http.Error(w, `{"error":{"message":"bad key","type":"invalid_request_error"}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "live-z"}, {"id": "live-a"}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// noAuthProvider serves the models list to any request — the shape
// of local OpenAI-compatible servers (Ollama, LM Studio, vLLM).
func noAuthProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "local-m2"}, {"id": "local-m1"}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// modelIDs projects a listing to its ids, for id-level pins.
func modelIDs(models []modelInfo) []string { return systemModelIDs(models) }

// Keyed: live fetch wins, sorted, and the bearer key is sent.
func TestSystemModelsLiveFetch(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer k-1" {
			http.Error(w, `{"error":{"message":"bad key","type":"invalid_request_error"}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "live-z"}, {"id": "live-a"}},
		})
	}))
	t.Cleanup(srv.Close)

	got := modelIDs(systemModels(t.Context(), srv.URL, "k-1"))
	if len(got) != 2 || got[0] != "live-a" || got[1] != "live-z" {
		t.Errorf("systemModels = %v, want [live-a live-z]", got)
	}
	if hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1", hits.Load())
	}
}

// Keyless: a no-auth endpoint still serves its live list, and no
// credential rides the request — neither an empty bearer nor an
// ambient OPENAI_API_KEY from the server env.
func TestSystemModelsKeylessSendsNoCredential(t *testing.T) {
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "local-m2"}, {"id": "local-m1"}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_API_KEY", "leaked-env-key")

	got := modelIDs(systemModels(t.Context(), srv.URL, ""))
	if len(got) != 2 || got[0] != "local-m1" || got[1] != "local-m2" {
		t.Errorf("systemModels keyless = %v, want [local-m1 local-m2]", got)
	}

	sent, _ := auth.Load().(string)
	if sent != "" {
		t.Errorf("Authorization for an empty key = %q, want none", sent)
	}
}

// Keyless against an auth-required endpoint: nil — reads never
// surface the error.
func TestSystemModelsKeylessRejectedEmpty(t *testing.T) {
	if got := systemModels(t.Context(), fakeProvider(t).URL, ""); got != nil {
		t.Errorf("systemModels keyless-rejected = %v, want nil", got)
	}
}

// Keyed but the provider rejects the key: nil — reads never surface
// the error.
func TestSystemModelsFetchFailureEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"bad key","type":"invalid_request_error"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	if got := systemModels(t.Context(), srv.URL, "k-1"); got != nil {
		t.Errorf("systemModels fetch failure = %v, want nil", got)
	}
}

// A successful fetch is cached: a second resolve doesn't hit the
// server.
func TestSystemModelsCached(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "m1"}},
		})
	}))
	t.Cleanup(srv.Close)

	systemModels(t.Context(), srv.URL, "k-1")
	systemModels(t.Context(), srv.URL, "k-1")
	if hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1 (second resolve cached)", hits.Load())
	}
}

// The listing's capacity aliases are read as numbers, and a listing
// that states several is taken at its smallest — the request must fit
// the tightest stated bound.
func TestSystemModelsReportedWindow(t *testing.T) {
	entry := func(fields map[string]any) map[string]json.RawMessage {
		t.Helper()
		raw := make(map[string]json.RawMessage, len(fields))
		for key, value := range fields {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("encode %s: %v", key, err)
			}
			raw[key] = encoded
		}
		return raw
	}
	cases := []struct {
		name   string
		fields map[string]any
		want   int
	}{
		{"context_length", map[string]any{"context_length": 128000}, 128000},
		{"max_context_length", map[string]any{"max_context_length": 200000}, 200000},
		{"max_model_len", map[string]any{"max_model_len": 8192}, 8192},
		{"smallest wins", map[string]any{"context_length": 128000, "max_model_len": 8192}, 8192},
		{"absent", map[string]any{}, 0},
		{"null", map[string]any{"context_length": nil}, 0},
		{"string is not a count", map[string]any{"context_length": "128000"}, 0},
		{"zero and negative", map[string]any{"context_length": 0, "max_model_len": -1}, 0},
		// max_input_tokens is prompt capacity on other endpoints, not
		// the model's window: it is never read.
		{"max_input_tokens ignored", map[string]any{"max_input_tokens": 4096}, 0},
	}
	for _, tc := range cases {
		if got := reportedWindow(entry(tc.fields)); got != tc.want {
			t.Errorf("%s: reportedWindow = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// A listing that names no capacity yields zero windows — the ids still
// serve selectors.
func TestSystemModelsBareListingHasNoWindows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"bare-1"},{"id":"bare-2"}]}`))
	}))
	t.Cleanup(srv.Close)

	models := systemModels(t.Context(), srv.URL, "")
	if got := modelIDs(models); len(got) != 2 {
		t.Fatalf("systemModels = %v, want two ids", got)
	}
	if windows := reportedWindows(models); len(windows) != 0 {
		t.Errorf("reported windows = %v, want none", windows)
	}
}

// Probe: nil on success; the failure names the cause.
func TestProbe(t *testing.T) {
	srv := fakeProvider(t)

	if err := probeConnection(t.Context(), srv.URL, "k-1"); err != nil {
		t.Errorf("probe ok-case err = %v, want nil", err)
	}
	err := probeConnection(t.Context(), srv.URL, "wrong")
	if err == nil {
		t.Fatal("probe bad-key err = nil, want failure")
	}
	if want := "401"; !strings.Contains(err.Error(), want) {
		t.Errorf("probe bad-key err = %v, want it to mention %q", err, want)
	}
	err = probeConnection(t.Context(), srv.URL, "")
	if err == nil {
		t.Fatal("probe keyless-against-auth err = nil, want failure")
	}
	if want := "key missing or invalid"; !strings.Contains(err.Error(), want) {
		t.Errorf("probe keyless-against-auth err = %v, want it to mention %q", err, want)
	}
	err = probeConnection(t.Context(), "http://127.0.0.1:1/v1", "k-1")
	if err == nil {
		t.Fatal("probe unreachable err = nil, want failure")
	}
	if want := "could not reach"; !strings.Contains(err.Error(), want) {
		t.Errorf("probe unreachable err = %v, want it to mention %q", err, want)
	}
}

// The listing cache never keeps key material in its map keys: the
// provider secret rides a digest, so the cache index carries no
// spelling of the key in process memory.
func TestModelCacheKeysCarryNoSecret(t *testing.T) {
	srv := noAuthProvider(t)
	if _, err := resolvedModels(t.Context(), srv.URL, "sk-livetest-secret-123"); err != nil {
		t.Fatalf("resolvedModels: %v", err)
	}
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	for key := range modelCache {
		if strings.Contains(key, "sk-livetest-secret-123") {
			t.Fatalf("cache key %q carries the secret verbatim", key)
		}
	}
}

// noAuthURL is a live no-auth endpoint for tests that need a
// saveable connection without credentials.
func noAuthURL(t *testing.T) string {
	t.Helper()
	return noAuthProvider(t).URL
}

// A failed listing fetch is cached briefly: read paths and the
// resolver's live tier do not re-hit a failing endpoint on every call.
// The failure expires fast, so recovery costs one retry.
func TestSystemModelsCachesFetchFailures(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	if _, err := resolvedModels(t.Context(), srv.URL, ""); err == nil {
		t.Fatalf("first fetch against a failing endpoint = nil error, want failure")
	}
	if _, err := resolvedModels(t.Context(), srv.URL, ""); err == nil {
		t.Fatalf("second fetch = nil error, want the cached failure")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("endpoint hits = %d, want 1 — the failure must serve from the cache", got)
	}
}
