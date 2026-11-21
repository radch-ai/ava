package schema_test

// Item lifecycle schema invariants: the CHECKs that bound item kinds,
// states, and invocation states, the composite keys that keep one
// agent's bookkeeping from naming another agent's rows, and the
// single-row history identity. Behavior over these tables is pinned
// through the backend service, not here.

import (
	"database/sql"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestSyncHistoryIsOneRow(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	var historyID string
	if err := sqlDB.QueryRow(`SELECT history_id FROM sync_history WHERE id = 1`).Scan(&historyID); err != nil {
		t.Fatalf("read history identity: %v", err)
	}
	// The contract is a non-empty identity; its shape is the cursor's
	// business, not the schema's.
	if historyID == "" {
		t.Errorf("history id is empty, want a per-database identity")
	}
	// The singleton rule is the pinned primary key.
	if _, err := sqlDB.Exec(
		`INSERT INTO sync_history (id, history_id) VALUES (2, 'other')`); err == nil {
		t.Fatal("second history row inserted, want CHECK (id = 1) rejection")
	}
}

// Every database mints its own history identity: a cursor from one
// store can never resume against another, in-memory or file-backed.
func TestSyncHistoryDiffersPerDatabase(t *testing.T) {
	first := testdb.Open(t, true)
	second := testdb.Open(t, true)

	read := func(sqlDB *sql.DB) string {
		var id string
		if err := sqlDB.QueryRow(`SELECT history_id FROM sync_history WHERE id = 1`).Scan(&id); err != nil {
			t.Fatalf("read history identity: %v", err)
		}
		return id
	}
	if read(first) == read(second) {
		t.Fatal("two databases share a history identity, want per-database minting")
	}
}

func TestQueueItemKindAndStateChecks(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")

	if _, err := sqlDB.Exec(
		`INSERT INTO queue_items (id, agent_id, kind, content, state, accepted_ms)
		 VALUES ('i1', 'a', 'chat', 'x', 'accepted', 1)`); err == nil {
		t.Fatal("unknown kind stored, want CHECK rejection")
	}
	// Every legal state stores; anything else fails.
	for _, state := range []string{"accepted", "started", "completed", "failed", "stopped", "cancelled", "interrupted"} {
		if _, err := sqlDB.Exec(
			`INSERT INTO queue_items (id, agent_id, kind, content, state, accepted_ms)
			 VALUES (?, 'a', 'prompt', 'x', ?, 1)`, "state-"+state, state); err != nil {
			t.Fatalf("insert state %q: %v", state, err)
		}
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO queue_items (id, agent_id, kind, content, state, accepted_ms)
		 VALUES ('i2', 'a', 'prompt', 'x', 'running', 1)`); err == nil {
		t.Fatal("unknown state stored, want CHECK rejection")
	}
}

func TestQueueItemThreadMustBelongToAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	seedAgentRow(t, sqlDB, "b")

	// a's item can never name b's thread.
	_, err := sqlDB.Exec(
		`INSERT INTO queue_items (id, agent_id, kind, content, state, thread_id, accepted_ms)
		 VALUES ('i1', 'a', 'prompt', 'x', 'started', 'b-t1', 1)`)
	if err == nil {
		t.Fatal("cross-owner thread stored, want foreign key rejection")
	}
}

func TestToolInvocationMustBelongToAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	seedAgentRow(t, sqlDB, "b")

	if _, err := sqlDB.Exec(
		`INSERT INTO queue_items (id, agent_id, kind, content, state, accepted_ms)
		 VALUES ('i1', 'a', 'prompt', 'x', 'started', 1)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	// b's invocation cannot name a's item.
	_, err := sqlDB.Exec(
		`INSERT INTO tool_invocations (id, agent_id, item_id, call_id, name, args, started_ms, state)
		 VALUES ('v1', 'b', 'i1', 'call', 'fetch', '{}', 1, 'open')`)
	if err == nil {
		t.Fatal("cross-owner invocation stored, want foreign key rejection")
	}
	// The state CHECK bounds the two invocation states.
	if _, err := sqlDB.Exec(
		`INSERT INTO tool_invocations (id, agent_id, item_id, call_id, name, args, started_ms, state)
		 VALUES ('v2', 'a', 'i1', 'call', 'fetch', '{}', 1, 'interrupted')`); err == nil {
		t.Fatal("unknown invocation state stored, want CHECK rejection")
	}
}

func TestLifecycleEventMustBelongToAgent(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	seedAgentRow(t, sqlDB, "b")

	if _, err := sqlDB.Exec(
		`INSERT INTO queue_items (id, agent_id, kind, content, state, accepted_ms)
		 VALUES ('i1', 'a', 'prompt', 'x', 'accepted', 1)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	_, err := sqlDB.Exec(
		`INSERT INTO lifecycle_journal (agent_id, pos, item_id, kind, payload, created_ms)
		 VALUES ('b', 1, 'i1', 'item_accepted', '{}', 1)`)
	if err == nil {
		t.Fatal("cross-owner event stored, want foreign key rejection")
	}
	// Positions are unique per agent.
	if _, err := sqlDB.Exec(
		`INSERT INTO lifecycle_journal (agent_id, pos, item_id, kind, payload, created_ms)
		 VALUES ('a', 1, 'i1', 'item_accepted', '{}', 1)`); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO lifecycle_journal (agent_id, pos, item_id, kind, payload, created_ms)
		 VALUES ('a', 1, 'i1', 'item_started', '{}', 2)`); err == nil {
		t.Fatal("repeated position stored, want primary key rejection")
	}
}

func TestAgentDeletionCascadesLifecycleRows(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	seedAgentRow(t, sqlDB, "b")

	if _, err := sqlDB.Exec(
		`INSERT INTO queue_items (id, agent_id, kind, content, state, accepted_ms)
		 VALUES ('i1', 'a', 'prompt', 'x', 'started', 1)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO agent_sync_state (agent_id) VALUES ('a')`); err != nil {
		t.Fatalf("seed sync state: %v", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM agents WHERE id = 'a'`); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	for _, table := range []string{"queue_items", "agent_sync_state"} {
		var count int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s rows = %d, want the agent cascade to remove them", table, count)
		}
	}
}
