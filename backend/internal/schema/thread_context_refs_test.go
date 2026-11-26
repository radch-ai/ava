package schema_test

// Mid-loop compaction's reference table: membership is ordered, unique,
// and owned — one position and one message per successor thread, same
// agent on both sides, and the whole set dies with the thread it
// belongs to. The is_summary marker accepts only its two states.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// seedRefThread seeds one agent whose initial thread holds one message
// row, plus a successor compact thread to carry references: the minimal
// shape every ref assertion needs.
func seedRefThread(t *testing.T, sqlDB *sql.DB, agent string) {
	t.Helper()
	seedAgentRow(t, sqlDB, agent)
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := []string{
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, created_at)
		 VALUES ('` + agent + `-m1', '` + agent + `', 'user', 'prompt', '', '` + agent + `-t1', 1)`,
		`INSERT INTO threads (id, agent_id, previous_thread_id, kind, created_at)
		 VALUES ('` + agent + `-succ', '` + agent + `', '` + agent + `-t1', 'compact', 2)`,
	}
	for _, q := range seed {
		if _, err := tx.Exec(q); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
}

func insertRef(t *testing.T, sqlDB *sql.DB, agent, thread, message string, position int) error {
	t.Helper()
	_, err := sqlDB.Exec(
		`INSERT INTO thread_context_refs (agent_id, thread_id, position, message_id)
		 VALUES (?, ?, ?, ?)`, agent, thread, position, message)
	return err
}

func TestContextRefPositionUniquePerThread(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedRefThread(t, sqlDB, "a")
	if err := insertRef(t, sqlDB, "a", "a-succ", "a-m1", 0); err != nil {
		t.Fatalf("insert ref: %v", err)
	}
	err := insertRef(t, sqlDB, "a", "a-succ", "a-m1", 0)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate position err = %v, want UNIQUE rejection", err)
	}
}

func TestContextRefMessageUniquePerThread(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedRefThread(t, sqlDB, "a")
	// A second message row: the same source cannot be referenced twice
	// by one successor, at any position.
	if _, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, created_at)
		 VALUES ('a-m2', 'a', 'tool', '{}', '', 'a-t1', 3)`); err != nil {
		t.Fatalf("seed second message: %v", err)
	}
	if err := insertRef(t, sqlDB, "a", "a-succ", "a-m1", 0); err != nil {
		t.Fatalf("insert ref: %v", err)
	}
	err := insertRef(t, sqlDB, "a", "a-succ", "a-m1", 1)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate message err = %v, want UNIQUE rejection", err)
	}
}

func TestContextRefRejectsForeignAgentAndUnknownMessage(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedRefThread(t, sqlDB, "a")
	seedRefThread(t, sqlDB, "b")

	// b's successor is not a's to fill: the composite thread FK rejects.
	err := insertRef(t, sqlDB, "a", "b-succ", "a-m1", 0)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("foreign thread err = %v, want foreign key rejection", err)
	}

	// A row of another agent is not a's to reference: the composite
	// message FK rejects.
	err = insertRef(t, sqlDB, "a", "a-succ", "b-m1", 0)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("foreign message err = %v, want foreign key rejection", err)
	}
}

func TestContextRefsCascadeWithThread(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedRefThread(t, sqlDB, "a")
	if err := insertRef(t, sqlDB, "a", "a-succ", "a-m1", 0); err != nil {
		t.Fatalf("insert ref: %v", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM threads WHERE id = 'a-succ'`); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM thread_context_refs`).Scan(&n); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if n != 0 {
		t.Errorf("refs surviving thread deletion = %d, want 0", n)
	}
}

func TestThreadMessageSummaryCheck(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	seedAgentRow(t, sqlDB, "a")
	_, err := sqlDB.Exec(
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, is_summary)
		 VALUES ('s', 'a', 'user', 'x', '', 'a-t1', 2)`)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("is_summary=2 err = %v, want CHECK constraint failure", err)
	}
}

// Migration 0013 backfills is_summary onto the seeded summary row of
// every legacy compact thread: the reference projection's sort band
// keys on the marker, so an unmarked seed would misplace every
// pre-0013 compact successor. Only a compact thread's first row marks,
// and only when it is an assistant row — the seed shape every rotation
// this codebase ships writes first.
func TestSummaryBackfillMarksLegacyCompactSeeds(t *testing.T) {
	db := testdb.Open(t, false) // file-backed: the stepwise engine shares it
	m := migrateInstance(t, db)
	if err := m.Migrate(12); err != nil {
		t.Fatalf("step back to the pre-refs schema: %v", err)
	}

	seedAgentRow(t, db, "backfill")
	seed := []string{
		// A legacy compact successor: seeded assistant summary first,
		// then a later user row.
		`INSERT INTO threads (id, agent_id, previous_thread_id, kind, created_at)
		 VALUES ('backfill-c1', 'backfill', 'backfill-t1', 'compact', 2)`,
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, created_at)
		 VALUES ('backfill-c1-seed', 'backfill', 'assistant', 'old summary', '', 'backfill-c1', 3)`,
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, created_at)
		 VALUES ('backfill-c1-after', 'backfill', 'user', 'after', '', 'backfill-c1', 4)`,
		// A plain thread's assistant row: the thread kind gates the
		// backfill, not the role alone.
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, created_at)
		 VALUES ('backfill-plain', 'backfill', 'assistant', 'not a summary', '', 'backfill-t1', 5)`,
		// A compact thread whose first row is not an assistant row:
		// the conservative backfill marks nothing.
		`INSERT INTO threads (id, agent_id, previous_thread_id, kind, created_at)
		 VALUES ('backfill-c2', 'backfill', 'backfill-c1', 'compact', 6)`,
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, created_at)
		 VALUES ('backfill-c2-user', 'backfill', 'user', 'first', '', 'backfill-c2', 7)`,
		`INSERT INTO thread_messages (id, agent_id, role, content, turn_id, thread_id, created_at)
		 VALUES ('backfill-c2-asst', 'backfill', 'assistant', 'second', '', 'backfill-c2', 8)`,
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	for _, q := range seed {
		if _, err := tx.Exec(q); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("apply 0013: %v", err)
	}

	marked := func(id string) int {
		var v int
		if err := db.QueryRow(`SELECT is_summary FROM thread_messages WHERE id = ?`, id).Scan(&v); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return v
	}
	if got := marked("backfill-c1-seed"); got != 1 {
		t.Fatalf("legacy compact seed is_summary = %d, want 1", got)
	}
	if got := marked("backfill-c1-after"); got != 0 {
		t.Fatalf("later row is_summary = %d, want 0", got)
	}
	if got := marked("backfill-plain"); got != 0 {
		t.Fatalf("plain thread assistant row is_summary = %d, want 0", got)
	}
	if got := marked("backfill-c2-user"); got != 0 {
		t.Fatalf("compact thread with non-assistant first row is_summary = %d, want 0", got)
	}
	if got := marked("backfill-c2-asst"); got != 0 {
		t.Fatalf("compact thread's second row is_summary = %d, want 0", got)
	}
}
