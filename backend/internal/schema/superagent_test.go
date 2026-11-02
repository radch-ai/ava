package schema_test

// Superagent SQL-level invariants: the seed guarantees and the
// DB-enforced rules from migrations/0002_seed_superagent. Seeding
// goes through the sqlc query layer; raw SQL remains where no query
// reaches (the system row is excluded from GetAgents) and where the
// SQL rule itself is under test (the trigger probes).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestSeedSuperagent(t *testing.T) {
	sqlDB := testdb.Open(t)

	var name, icon, config string
	if err := sqlDB.QueryRow(
		`SELECT name, icon, config FROM agents WHERE id = '_superagent'`,
	).Scan(&name, &icon, &config); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if name != "Ava" || icon != "message-square" {
		t.Errorf("seed row = %q/%q, want Ava/message-square", name, icon)
	}
	// The document never duplicates identity; its home is the columns.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(config), &doc); err != nil {
		t.Fatalf("seed config JSON: %v", err)
	}
	if _, ok := doc["name"]; ok {
		t.Errorf("seed config carries name — identity belongs in columns")
	}
	if _, ok := doc["icon"]; ok {
		t.Errorf("seed config carries icon — identity belongs in columns")
	}
	// No default scheduled work — the heartbeat check is the only
	// proactive default; cron jobs are user-created.
	var jobs []json.RawMessage
	if err := json.Unmarshal(doc["cronJobs"], &jobs); err != nil || len(jobs) != 0 {
		t.Errorf("seed cronJobs = %s, want none", doc["cronJobs"])
	}
	var tools []string
	if err := json.Unmarshal(doc["toolsEnabled"], &tools); err != nil || len(tools) != 0 {
		t.Errorf("seed toolsEnabled = %s, want none until tools are wired", doc["toolsEnabled"])
	}
}

func TestSystemAgentsCannotBeDeleted(t *testing.T) {
	sqlDB := testdb.Open(t)
	queries := db.New(sqlDB)

	// The trigger aborts deletes of "_"-prefixed ids — every code
	// path, not just endpoints.
	_, err := sqlDB.Exec(`DELETE FROM agents WHERE id = '_superagent'`)
	if err == nil || !strings.Contains(err.Error(), "system agents cannot be deleted") {
		t.Fatalf("delete _superagent err = %v, want trigger abort", err)
	}

	// Non-system rows delete normally.
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe", Config: "{}",
	}); err != nil {
		t.Fatalf("insert research: %v", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM agents WHERE id = 'research'`); err != nil {
		t.Fatalf("delete research: %v", err)
	}
}
