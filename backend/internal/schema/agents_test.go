// Agents-table rules: typed text columns; created_at stamped by
// the DB on insert.
package schema_test

import (
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestCreatedAtStampedByDatabase(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	// SeedAgent omits created_at — the insert path never supplies it.
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})

	var createdAt int64
	if err := sqlDB.QueryRow(
		`SELECT created_at FROM agents WHERE id = 'research'`,
	).Scan(&createdAt); err != nil {
		t.Fatalf("read created_at: %v", err)
	}
	// Micros since epoch, within 10s of now — the stamp comes from
	// the database clock, not the test.
	if delta := createdAt - time.Now().UnixMicro(); delta < -10*time.Second.Microseconds() || delta > 10*time.Second.Microseconds() {
		t.Errorf("created_at delta = %dµs, want within ±10s of now", delta)
	}
}
