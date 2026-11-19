package tools_test

// GET/PATCH /tools handler tests over the real store: the catalog
// listing and the global-switch edit contract.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/api/tools"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func serve(t *testing.T, pattern string, h api.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	pattern = strings.TrimPrefix(pattern, method+" ")
	for i, seg := range strings.Split(pattern, "/") {
		if name, ok := strings.CutPrefix(seg, "{"); ok {
			if name, ok = strings.CutSuffix(name, "}"); ok {
				req.SetPathValue(name, strings.Split(path, "/")[i])
			}
		}
	}
	rec := httptest.NewRecorder()
	if err := h(rec, req); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return rec
}

// toolStore opens a fresh store with two catalog rows, the first
// globally disabled to prove the listing reflects saved state.
func toolStore(t *testing.T) *db.Queries {
	t.Helper()
	queries := db.New(testdb.Open(t, true))
	for i, id := range []string{"ddg", "notes"} {
		if err := queries.UpsertTool(t.Context(), db.UpsertToolParams{
			ID: id, Name: id + " name", Description: id + " does things", Builtin: 1,
		}); err != nil {
			t.Fatalf("seed tool %q: %v", id, err)
		}
		if i == 0 {
			if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
				Enabled: 0, ID: id,
			}); err != nil {
				t.Fatalf("disable %q: %v", id, err)
			}
		}
	}
	return queries
}

func TestGetToolsListsCatalog(t *testing.T) {
	queries := toolStore(t)

	rec := serve(t, "GET /tools", tools.GetTools(queries), http.MethodGet, "/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"tools":[{"id":"ddg","name":"ddg name","description":"ddg does things","builtin":true,"enabled":false,"kind":"builtin"},{"id":"notes","name":"notes name","description":"notes does things","builtin":true,"enabled":true,"kind":"builtin"}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestToggleToolFlipsGlobalSwitch(t *testing.T) {
	queries := toolStore(t)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), queries),
		http.MethodPatch, "/tools/ddg", `{"enabled":true,"kind":"builtin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"id":"ddg","name":"ddg name","description":"ddg does things","builtin":true,"enabled":true,"kind":"builtin"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	row, err := queries.GetTool(t.Context(), "ddg")
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled != 1 {
		t.Errorf("saved enabled = %d, want 1", row.Enabled)
	}
}

func TestToggleToolUnknownTool(t *testing.T) {
	queries := toolStore(t)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), queries),
		http.MethodPatch, "/tools/ghost", `{"enabled":true,"kind":"builtin"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	want := `{"message":"Tool \"ghost\" not found"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// false is a real value: an absent field is the only 400 here.
func TestToggleToolRequiresEnabled(t *testing.T) {
	queries := toolStore(t)

	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"yes"}`} {
		rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), queries),
			http.MethodPatch, "/tools/ddg", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); !strings.Contains(got, "message") {
			t.Errorf("body %s: response carries no message: %s", body, got)
		}
	}
	// The absent-field case names the missing requirement.
	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), queries),
		http.MethodPatch, "/tools/ddg", `{}`)
	want := `{"message":"enabled is required"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestToggleToolFalsePersists(t *testing.T) {
	queries := toolStore(t)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(api.NewThrottle(0), queries),
		http.MethodPatch, "/tools/notes", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	row, err := queries.GetTool(t.Context(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled != 0 {
		t.Errorf("saved enabled = %d, want 0", row.Enabled)
	}
}

// A burst on one tool draws a 429; the next toggle passes once the
// window elapses.
func TestToggleToolThrottlesBursts(t *testing.T) {
	queries := toolStore(t)
	throttle := api.NewThrottle(50 * time.Millisecond)

	rec := serve(t, "PATCH /tools/{id}", tools.ToggleTool(throttle, queries),
		http.MethodPatch, "/tools/ddg", `{"enabled":true,"kind":"builtin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("first toggle: status = %d, want 200", rec.Code)
	}
	rec = serve(t, "PATCH /tools/{id}", tools.ToggleTool(throttle, queries),
		http.MethodPatch, "/tools/ddg", `{"enabled":false}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("burst toggle: status = %d, want 429", rec.Code)
	}

	time.Sleep(60 * time.Millisecond)
	rec = serve(t, "PATCH /tools/{id}", tools.ToggleTool(throttle, queries),
		http.MethodPatch, "/tools/ddg", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-window toggle: status = %d, want 200", rec.Code)
	}
}
