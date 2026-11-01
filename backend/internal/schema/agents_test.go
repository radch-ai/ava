// Agents-table DB-enforced rules: created_at is stamped by the
// database on insert.
package schema_test

import (
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestCreatedAtStampedByDatabase(t *testing.T) {
	sqlDB := testdb.Open(t)

	if _, err := sqlDB.Exec(
		`INSERT INTO agents (id, name, icon) VALUES ('research', 'Research', 'globe')`,
	); err != nil {
		t.Fatalf("insert without created_at: %v", err)
	}

	var createdAt int64
	if err := sqlDB.QueryRow(
		`SELECT created_at FROM agents WHERE id = 'research'`,
	).Scan(&createdAt); err != nil {
		t.Fatalf("read created_at: %v", err)
	}
	// Micros since epoch, within 10s of now — the stamp comes from the
	// database clock, not the test.
	if delta := createdAt - time.Now().UnixMicro(); delta < -10*time.Second.Microseconds() || delta > 10*time.Second.Microseconds() {
		t.Errorf("created_at delta = %dµs, want within ±10s of now", delta)
	}
}
