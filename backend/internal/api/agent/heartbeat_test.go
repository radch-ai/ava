package agent

// UpdateHeartbeat handler tests: upsert singletons and the model
// entry column rule (see entryToDB), read back raw.

import (
	"net/http"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestUpdateHeartbeatUnknownAgent(t *testing.T) {
	queries := db.New(testdb.Open(t, true))

	rec, err := serve(t, "PATCH /agent/{id}/heartbeat", UpdateHeartbeat(queries),
		http.MethodPatch, "/agent/ghost/heartbeat",
		`{"model":null,"enabled":false,"duration":"1hr","content":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateHeartbeatUpsertsSingleRow(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	patch := `{"model":null,"enabled":true,"duration":"30m","content":"first"}`
	for _, body := range []string{patch,
		`{"model":null,"enabled":false,"duration":"1hr","content":"second"}`} {
		rec, err := serve(t, "PATCH /agent/{id}/heartbeat", UpdateHeartbeat(queries),
			http.MethodPatch, "/agent/research/heartbeat", body)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	}

	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM heartbeat WHERE agent_id = 'research'`,
	).Scan(&n); err != nil {
		t.Fatalf("count heartbeat: %v", err)
	}
	if n != 1 {
		t.Fatalf("heartbeat rows = %d, want 1 (upsert, no duplicates)", n)
	}
	var content string
	if err := sqlDB.QueryRow(
		`SELECT content FROM heartbeat WHERE agent_id = 'research'`,
	).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "second" {
		t.Errorf("content = %q, want second (last write wins)", content)
	}
}

func TestUpdateHeartbeatModelPair(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO providers (id, name, base_url) VALUES ('p-1', 'P1', 'https://p1.dev/v1')`,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	patchBody := func(t *testing.T, body string) {
		t.Helper()
		rec, err := serve(t, "PATCH /agent/{id}/heartbeat", UpdateHeartbeat(queries),
			http.MethodPatch, "/agent/research/heartbeat", body)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, body)
		}
	}
	model := func(t *testing.T) (any, any) {
		t.Helper()
		var model, provider any
		if err := sqlDB.QueryRow(
			`SELECT model, provider_id FROM heartbeat WHERE agent_id = 'research'`,
		).Scan(&model, &provider); err != nil {
			t.Fatal(err)
		}
		return model, provider
	}

	patchBody(t, `{"model":null,"enabled":false,"duration":"1hr","content":""}`)
	if m, p := model(t); m != nil || p != nil {
		t.Errorf("model = %v, provider = %v, want NULL/NULL (null model)", m, p)
	}
	patchBody(t, `{"model":{"id":"m1"},"enabled":false,"duration":"1hr","content":""}`)
	if m, p := model(t); m != nil || p != nil {
		t.Errorf("model = %v, provider = %v, want NULL/NULL (partial entry)", m, p)
	}
	patchBody(t, `{"model":{"id":"m1","providerId":"p-1"},"enabled":false,"duration":"1hr","content":""}`)
	if m, p := model(t); m != "m1" || p != "p-1" {
		t.Errorf("model = %v, provider = %v, want m1/p-1 (concrete pair)", m, p)
	}

	patchBody(t, `{"model":null,"enabled":false,"duration":"1hr","content":""}`)
	if m, p := model(t); m != nil || p != nil {
		t.Errorf("model = %v, provider = %v, want NULL/NULL (cleared)", m, p)
	}
}
