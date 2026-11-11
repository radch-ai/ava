package agent

// UpdateHeartbeat handler tests: upsert singletons and the model
// column's NULL-is-auto rule, read back raw.

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
		`{"model":"auto","enabled":false,"duration":"1hr","content":""}`)
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

	patch := `{"model":"auto","enabled":true,"duration":"30m","content":"first"}`
	for _, body := range []string{patch,
		`{"model":"auto","enabled":false,"duration":"1hr","content":"second"}`} {
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

func TestUpdateHeartbeatModelAutoStoresNULL(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	// "auto" and "" both mean "follow the default routing": the
	// column stays NULL, the bundle reads back "auto".
	for _, body := range []string{
		`{"model":"auto","enabled":false,"duration":"1hr","content":""}`,
		`{"model":"","enabled":false,"duration":"1hr","content":""}`,
	} {
		rec, err := serve(t, "PATCH /agent/{id}/heartbeat", UpdateHeartbeat(queries),
			http.MethodPatch, "/agent/research/heartbeat", body)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, body)
		}
		var model any
		if err := sqlDB.QueryRow(
			`SELECT model FROM heartbeat WHERE agent_id = 'research'`,
		).Scan(&model); err != nil {
			t.Fatal(err)
		}
		if model != nil {
			t.Errorf("model = %v, want NULL (%s)", model, body)
		}
	}
}
