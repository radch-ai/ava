package schema_test

// Heartbeat and cron-job invariants: per-agent heartbeat singleton,
// cron rows, cascades. Raw SQL: the schema rules are under test.

import (
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestSeedSuperagentHeartbeat(t *testing.T) {
	sqlDB := testdb.Open(t)
	var model any
	var enabled int64
	var duration, content string
	if err := sqlDB.QueryRow(
		`SELECT model, enabled, duration, content
		 FROM heartbeat WHERE agent_id = '_superagent'`,
	).Scan(&model, &enabled, &duration, &content); err != nil {
		t.Fatalf("seed heartbeat row: %v", err)
	}
	// NULL model = follow the default routing — no sentinel string
	// in the column.
	if model != nil {
		t.Errorf("seed model = %v, want NULL (follow default)", model)
	}
	if enabled != 1 {
		t.Errorf("seed enabled = %d, want 1", enabled)
	}
	if duration != "30m" {
		t.Errorf("seed duration = %q, want 30m", duration)
	}
	wantContent := "Check agent heartbeats and flag anything stalled. If nothing needs attention, reply NO_REPLY."
	if content != wantContent {
		t.Errorf("seed content = %q, want the NO_REPLY check", content)
	}
}

func TestHeartbeatSingletonPerAgent(t *testing.T) {
	sqlDB := testdb.Open(t)

	// agent_id is the primary key: a second row for the same agent
	// cannot exist — updates land on the one row, never a duplicate.
	_, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, enabled, duration, content)
		 VALUES ('_superagent', NULL, 0, '1hr', 'duplicate')`)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("second heartbeat err = %v, want UNIQUE constraint failure", err)
	}
}

func TestAgentDeleteCascades(t *testing.T) {
	sqlDB := testdb.Open(t)

	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context)
		 VALUES ('temp', 'Temp', 'globe', '', '', '', '')`); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO heartbeat (agent_id, model, enabled, duration, content)
		 VALUES ('temp', NULL, 1, '30m', 'x')`); err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO cron_jobs (id, agent_id, time, frequency, prompt)
		 VALUES ('job-1', 'temp', '09:00', 'daily', 'digest')`); err != nil {
		t.Fatalf("insert cron job: %v", err)
	}

	if _, err := sqlDB.Exec(`DELETE FROM agents WHERE id = 'temp'`); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	for _, table := range []string{"heartbeat", "cron_jobs"} {
		var n int
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM ` + table + ` WHERE agent_id = 'temp'`,
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s rows after agent delete = %d, want 0 (cascade)", table, n)
		}
	}
}
