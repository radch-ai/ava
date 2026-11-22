// Audit-table rules: the durable, payload-free security audit.
// The schema itself is the payload guard -- there is no
// free-form column, and both enums are CHECK-constrained.
package schema_test

import (
	"testing"

	"github.com/radch-ai/ava/backend/internal/testdb"
)

// AuditEvent values are a closed set: an unknown event cannot be
// stored, so no report has to interpret one.
func TestAuditEventChecked(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, err := sqlDB.ExecContext(t.Context(), `
INSERT INTO tool_audit_events (occurred_at, event, decision, reason_code, state)
VALUES (1, 'payload', 'admit', 'ok', 'admitted')`)
	if err == nil {
		t.Fatal("event 'payload' accepted, want CHECK rejection")
	}
}

// State is a closed set too: a record is either an admission awaiting
// its outcome or terminal, never an open-ended status string.
func TestAuditStateChecked(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, err := sqlDB.ExecContext(t.Context(), `
INSERT INTO tool_audit_events (occurred_at, event, decision, reason_code, state)
VALUES (1, 'admission', 'admit', 'ok', 'pending')`)
	if err == nil {
		t.Fatal("state 'pending' accepted, want CHECK rejection")
	}
}

// Positive control for the two rejections above: an admitted
// pre-dispatch record with identifiers stores cleanly — the CHECKs
// constrain the closed sets, not legitimate audit traffic.
func TestAuditValidRowAccepted(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	_, err := sqlDB.ExecContext(t.Context(), `
INSERT INTO tool_audit_events (occurred_at, event, agent_id, tool_id, decision, reason_code, state)
VALUES (1, 'admission', 'research', 'ddg', 'admit', 'ok', 'admitted')`)
	if err != nil {
		t.Fatalf("valid admission row rejected: %v", err)
	}
}
