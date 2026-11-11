package schema_test

// Thread and activity schema invariants: CHECKs, the agent cascade,
// and created_at stamping. Raw SQL — the schema rules are under test.

import (
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestThreadMessageRoleCheck(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	// Both wire roles store; anything else fails the CHECK.
	for _, role := range []string{"user", "assistant"} {
		if _, err := sqlDB.Exec(
			`INSERT INTO thread_messages (id, agent_id, role, content)
			 VALUES (?, '_superagent', ?, 'ok')`,
			"role-"+role, role,
		); err != nil {
			t.Fatalf("insert role %q: %v", role, err)
		}
	}
	_, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content)
		 VALUES ('bad', '_superagent', 'system', 'no')`)
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

	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity,
		                     operating_instructions, tools_context)
		 VALUES ('scratch', 'Scratch', 'globe', 'p', 'i', 'o', 't')`,
	); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	for _, insert := range []string{
		`INSERT INTO thread_messages (id, agent_id, role, content)
		 VALUES ('tm1', 'scratch', 'user', 'hello')`,
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
		`INSERT INTO thread_messages (id, agent_id, role, content)
		 VALUES ('tm2', '_superagent', 'user', 'hi')`,
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
