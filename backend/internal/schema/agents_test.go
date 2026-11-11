// Agents-table rules: typed text columns; created_at stamped by
// the DB on insert.
package schema_test

import (
	"slices"
	"testing"
	"time"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// The table is exactly these columns; the exact set is what
// catches a JSON blob column being added.
func TestAgentsColumnSet(t *testing.T) {
	sqlDB := testdb.Open(t, true)

	rows, err := sqlDB.Query(`PRAGMA table_info(agents)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	want := []string{"id", "name", "icon", "soul_personality", "soul_identity",
		"user_context", "operating_instructions", "tools_context", "tools_enabled",
		"created_at"}
	if !slices.Equal(got, want) {
		t.Errorf("agents columns = %v, want %v", got, want)
	}
}

func TestCreatedAtStampedByDatabase(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	// CreateAgent omits created_at — the app's only write path.
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	}); err != nil {
		t.Fatalf("create agent: %v", err)
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

// tools_enabled defaults to the empty JSON array — never NULL, the
// bundle's [] shape.
func TestToolsEnabledDefaultsToEmptyArray(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	// CreateAgent omits tools_enabled — the column default applies.
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var toolsEnabled string
	if err := sqlDB.QueryRow(
		`SELECT tools_enabled FROM agents WHERE id = 'research'`,
	).Scan(&toolsEnabled); err != nil {
		t.Fatalf("read tools_enabled: %v", err)
	}
	if toolsEnabled != "[]" {
		t.Errorf("tools_enabled = %s, want []", toolsEnabled)
	}
}
