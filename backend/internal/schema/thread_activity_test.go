package schema_test

// Thread and activity schema invariants: CHECKs, the agent cascade,
// and created_at stamping. Raw SQL — the schema rules are under test.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// seedAgentRow inserts an agent and its initial thread through the
// shared fixture; the assertions in each test stay raw SQL.
func seedAgentRow(t *testing.T, sqlDB *sql.DB, id string) {
	t.Helper()
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID:                    id,
		Name:                  id,
		Icon:                  "globe",
		SoulPersonality:       "p",
		SoulIdentity:          "i",
		OperatingInstructions: "o",
		ToolsContext:          "t",
	})
}

func TestThreadMessageRoleCheck(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	// All four wire roles store; anything else fails the CHECK.
	for _, role := range []string{"user", "assistant", "tool", "error"} {
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id)
			 VALUES (?, '_superagent', ?, 'ok', '', '_superagent-thread-1')`,
			"role-"+role, role,
		); err != nil {
			t.Fatalf("insert role %q: %v", role, err)
		}
	}
	_, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id)
		 VALUES ('bad', '_superagent', 'system', 'no', '', '_superagent-thread-1')`)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("bad role err = %v, want CHECK constraint failure", err)
	}
}

func TestAgentActivityTriggerCheck(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	// Both schedulers store; anything else fails the CHECK.
	for _, trigger := range []string{"heartbeat", "cron"} {
		if _, err := sqlDB.Exec(
			`INSERT INTO agent_activity (id, agent_id, triggered_by, action, status)
			 VALUES (?, '_superagent', ?, 'cron.run', 'completed')`,
			"trigger-"+trigger, trigger,
		); err != nil {
			t.Fatalf("insert trigger %q: %v", trigger, err)
		}
	}
	_, err := sqlDB.Exec(
		`INSERT INTO agent_activity (id, agent_id, triggered_by, action, status)
		 VALUES ('bad', '_superagent', 'user', 'thread.message', 'completed')`)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("bad trigger err = %v, want CHECK constraint failure", err)
	}
}

func TestAgentActivityStatusCheck(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	// All three wire statuses store; anything else fails the CHECK.
	for _, status := range []string{"running", "completed", "failed"} {
		if _, err := sqlDB.Exec(
			`INSERT INTO agent_activity (id, agent_id, triggered_by, action, status)
			 VALUES (?, '_superagent', 'cron', 'cron.run', ?)`,
			"status-"+status, status,
		); err != nil {
			t.Fatalf("insert status %q: %v", status, err)
		}
	}
	_, err := sqlDB.Exec(
		`INSERT INTO agent_activity (id, agent_id, triggered_by, action, status)
		 VALUES ('bad', '_superagent', 'cron', 'cron.run', 'pending')`)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("bad status err = %v, want CHECK constraint failure", err)
	}
}

func TestThreadAndActivityCascadeWithAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	seedAgentRow(t, sqlDB, "scratch")
	for _, insert := range []string{
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id)
		 VALUES ('tm1', 'scratch', 'user', 'hello', '', 'scratch-t1')`,
		`INSERT INTO agent_activity (id, agent_id, triggered_by, action, status)
		 VALUES ('aa1', 'scratch', 'cron', 'cron.run', 'completed')`,
	} {
		if _, err := sqlDB.Exec(insert); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}

	// No endpoint deletes agents, so the cascade is only provable here.
	if _, err := sqlDB.Exec(`DELETE FROM agents WHERE id = 'scratch'`); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	for table := range map[string]struct{}{
		"thread_messages": {},
		"agent_activity":  {},
	} {
		var n int
		if err := sqlDB.QueryRow(
			`SELECT count(*) FROM ` + table + ` WHERE agent_id = 'scratch'`,
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s rows after agent delete = %d, want 0", table, n)
		}
	}
}

func TestThreadAndActivityCreatedAtDefault(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	// Page order depends on every row carrying a DB-stamped created_at.
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id)
		 VALUES ('tm2', '_superagent', 'user', 'hi', '', '_superagent-thread-1')`,
	); err != nil {
		t.Fatalf("insert thread message: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO agent_activity (id, agent_id, triggered_by, action, status)
		 VALUES ('aa2', '_superagent', 'cron', 'cron.run', 'completed')`,
	); err != nil {
		t.Fatalf("insert activity: %v", err)
	}
	for table, id := range map[string]string{
		"thread_messages": "tm2",
		"agent_activity":  "aa2",
	} {
		var created int64
		if err := sqlDB.QueryRow(
			`SELECT created_at FROM `+table+` WHERE id = ?`, id,
		).Scan(&created); err != nil {
			t.Fatalf("select %s.created_at: %v", table, err)
		}
		if created < 1_700_000_000_000 { // after 2023-11 in millis
			t.Errorf("%s.created_at = %d, want a DB-stamped ms epoch", table, created)
		}
	}
}
