package providers

// System-model resolution: live model list via the openai-go SDK
// (keyed and keyless alike), a short cache, and the save-time probe.
// The provider speaks the OpenAI /models shape.

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

	got := systemModels(t.Context(), srv.URL, "k-1")
	if len(got) != 2 || got[0] != "live-a" || got[1] != "live-z" {
		t.Errorf("systemModels = %v, want [live-a live-z]", got)
	}
	if hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1", hits.Load())
	}
}

// Keyless: a no-auth endpoint still serves its live list — the SDK
// carries an empty key without us branching. Pins what the SDK sends
// for an empty key (no usable credential) and that no ambient
// OPENAI_API_KEY leaks into the request.
func TestSystemModelsKeylessThroughSDK(t *testing.T) {
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

	got := systemModels(t.Context(), srv.URL, "")
	if len(got) != 2 || got[0] != "local-m1" || got[1] != "local-m2" {
		t.Errorf("systemModels keyless = %v, want [local-m1 local-m2]", got)
	}

	sent, _ := auth.Load().(string)
	if sent == "Bearer leaked-env-key" {
		t.Fatal("Authorization carries OPENAI_API_KEY from the server env — keyless leak")
	}
	// The SDK sends an empty bearer for an empty key: no usable
	// credential. No-auth endpoints ignore it; auth-required ones
	// 401, which reads as keyless-rejected above.
	if sent != "Bearer" && sent != "" {
		t.Errorf("Authorization for empty key = %q, want an empty bearer or none", sent)
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

// noAuthURL is a live no-auth endpoint for tests that need a
// saveable connection without credentials.
func noAuthURL(t *testing.T) string {
	t.Helper()
	return noAuthProvider(t).URL
}
