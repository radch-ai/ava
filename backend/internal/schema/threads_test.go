package schema_test

// Thread ownership rules: the composite keys make the pointer and
// every message name a thread of the same agent, while deferral lets
// an agent and its initial thread land in one transaction.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestActiveThreadMustBelongToAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	seedAgentRow(t, sqlDB, "b")

	// b's thread is not a's to point at: the composite FK rejects it.
	_, err := sqlDB.Exec(`UPDATE agents SET active_thread_id = 'b-t1' WHERE id = 'a'`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("cross-agent pointer err = %v, want foreign key rejection", err)
	}
}

func TestActiveThreadCannotBeNull(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")

	_, err := sqlDB.Exec(`UPDATE agents SET active_thread_id = NULL WHERE id = 'a'`)
	if err == nil || !strings.Contains(err.Error(), "NOT NULL") {
		t.Fatalf("null pointer err = %v, want NOT NULL rejection", err)
	}

	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = tx.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity,
		                     operating_instructions, tools_context, active_thread_id)
		 VALUES ('n', 'N', 'globe', '', '', '', '', NULL)`)
	if err == nil || !strings.Contains(err.Error(), "NOT NULL") {
		_ = tx.Rollback()
		t.Fatalf("null pointer insert err = %v, want NOT NULL rejection", err)
	}
	_ = tx.Rollback()
}

func TestThreadMessageMustBelongToAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	seedAgentRow(t, sqlDB, "b")

	// A row can never name another agent's thread.
	_, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id)
		 VALUES ('x', 'a', 'user', 'leak', '', 'b-t1')`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("cross-owner message err = %v, want foreign key rejection", err)
	}
}

func TestAgentAndThreadInsertTogether(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	// Autocommit: the agent alone cannot satisfy the pointer.
	_, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity,
		                     operating_instructions, tools_context, active_thread_id)
		 VALUES ('solo', 'Solo', 'globe', '', '', '', '', 'solo-t1')`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("agent without thread err = %v, want deferred foreign key failure", err)
	}

	// One transaction: agent first, thread second, check at commit.
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity,
		                     operating_instructions, tools_context, active_thread_id)
		 VALUES ('pair', 'Pair', 'globe', '', '', '', '', 'pair-t1')`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO threads (id, agent_id) VALUES ('pair-t1', 'pair')`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert thread: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit both rows: %v", err)
	}
}

func TestSuperagentSeedOwnsActiveThread(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	var active string
	if err := sqlDB.QueryRow(
		`SELECT active_thread_id FROM agents WHERE id = '_superagent'`).Scan(&active); err != nil {
		t.Fatalf("read superagent pointer: %v", err)
	}
	if active != "_superagent-thread-1" {
		t.Errorf("superagent active thread = %q, want _superagent-thread-1", active)
	}
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM threads WHERE id = ? AND agent_id = '_superagent'`, active).Scan(&n); err != nil {
		t.Fatalf("count threads: %v", err)
	}
	if n != 1 {
		t.Errorf("owned thread rows = %d, want 1", n)
	}
}

// Deleting an agent takes its threads and messages with it, pointer
// present.
func TestThreadsCascadeWithAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "gone")
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id)
		 VALUES ('m1', 'gone', 'user', 'hello', '', 'gone-t1')`); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	if _, err := sqlDB.Exec(`DELETE FROM agents WHERE id = 'gone'`); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	for table := range map[string]struct{}{"threads": {}, "thread_messages": {}} {
		var n int
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM ` + table + ` WHERE agent_id = 'gone'`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s rows after agent delete = %d, want 0", table, n)
		}
	}
}

// The predecessor is display navigation, not a second ownership
// claim: a thread can only replace one of its own agent's threads.
func TestThreadPredecessorMustBelongToAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	seedAgentRow(t, sqlDB, "b")

	_, err := sqlDB.Exec(
		`INSERT INTO threads (id, agent_id, previous_thread_id) VALUES ('a-t2', 'a', 'b-t1')`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("cross-agent predecessor err = %v, want foreign key rejection", err)
	}

	// The same agent's thread is the one legal predecessor.
	if _, err := sqlDB.Exec(
		`INSERT INTO threads (id, agent_id, previous_thread_id) VALUES ('a-t2', 'a', 'a-t1')`); err != nil {
		t.Fatalf("same-agent predecessor: %v", err)
	}
}

// An initial thread starts the chain: no predecessor until /new.
func TestInitialThreadHasNoPredecessor(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")

	for _, id := range []string{"a-t1", "_superagent-thread-1"} {
		var previous sql.NullString
		if err := sqlDB.QueryRow(
			`SELECT previous_thread_id FROM threads WHERE id = ?`, id).Scan(&previous); err != nil {
			t.Fatalf("read %s predecessor: %v", id, err)
		}
		if previous.Valid {
			t.Errorf("%s predecessor = %q, want NULL", id, previous.String)
		}
	}
}
