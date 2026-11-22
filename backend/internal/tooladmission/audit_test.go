package tooladmission_test

// Durable, payload-free audit behavior through the admission chain:
// concurrent outcome recovery, identifier bounding, retention cadence and
// restart reconciliation.

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// Every outstanding observed completion survives an audit failure
// and is recovered — one failure never overwrites another.
func TestConcurrentOutcomeFailuresAllRecovered(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	fake := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		started <- struct{}{}
		select {
		case <-release:
			return "observed", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	wrapped := h.wrap(fake)

	// Both outcome updates fail: no terminal write lands.
	if _, err := h.sqlDB.Exec(`CREATE TRIGGER fail_outcome BEFORE UPDATE ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := wrapped.InvokableRun(context.Background(), `{}`)
			if err != nil {
				t.Errorf("call: %v", err)
				return
			}
			if result != "observed" {
				t.Errorf("result = %q, want the observed outcome preserved", result)
			}
		}()
	}
	<-started
	<-started
	close(release)
	wg.Wait()

	// Recovery must repair both, then admissions resume.
	if _, err := h.sqlDB.Exec(`DROP TRIGGER fail_outcome;`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	result, err := wrapped.InvokableRun(t.Context(), `{}`)
	if err != nil || result != "observed" {
		t.Fatalf("post-recovery call = %q, %v; want the tool's result", result, err)
	}
	if got := fake.calls.Load(); got != 3 {
		t.Fatalf("executions = %d, want 3", got)
	}
	for _, event := range h.auditEvents() {
		if event.Event == tooladmission.EventAdmission && event.State != "terminal" {
			t.Fatalf("audit row %d left %q after recovery", event.ID, event.State)
		}
	}
}

// A model-supplied call id is a correlation handle, never trusted
// text. Oversized or secret-bearing ids are replaced deterministically.
func TestCallIdSanitizedInAudit(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ok", nil }}

	hostile := "call/with space\n" + strings.Repeat("x", 100)
	ctx := tooladmission.WithCallID(t.Context(), hostile)
	if _, err := h.wrap(fake).InvokableRun(ctx, `{}`); err != nil {
		t.Fatalf("call: %v", err)
	}
	clean := `toolu_01abc-XYZ.7`
	if _, err := h.wrap(fake).InvokableRun(tooladmission.WithCallID(t.Context(), clean), `{}`); err != nil {
		t.Fatalf("clean call: %v", err)
	}

	events := h.auditEvents()
	if len(events) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(events))
	}
	stored := events[0].ToolCallID.String
	if !strings.HasPrefix(stored, "h:") || len(stored) > 33 {
		t.Fatalf("hostile id stored as %q, want a bounded host-derived handle", stored)
	}
	if strings.Contains(stored, " ") || strings.Contains(stored, "\n") {
		t.Fatalf("hostile id kept unsafe characters: %q", stored)
	}
	if got := events[1].ToolCallID.String; got != clean {
		t.Fatalf("clean id stored as %q, want %q", got, clean)
	}
}

// Retention is driven by traffic — denial writes count too, so the
// 30-day bound expires without any successful admission.
func TestPruneOnDenialTraffic(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }}

	// 70 terminal rows past the retention cutoff.
	old := time.Now().Add(-31 * 24 * time.Hour).UnixMicro()
	for range 70 {
		if _, err := h.sqlDB.Exec(`INSERT INTO tool_audit_events
			(occurred_at, event, decision, reason_code, state) VALUES (?, 'denial', 'deny', 'policy_denied', 'terminal')`,
			old); err != nil {
			t.Fatalf("seed old audit row: %v", err)
		}
	}
	// Switch the tool off globally, so this is purely denial traffic
	// (denials need no capacity and never dispatch).
	if _, err := h.queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 0, ID: "gh:search",
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	for range 64 {
		if code := h.denyOnce(fake); code != tooladmission.CodePolicyDenied {
			t.Fatalf("code = %q, want policy_denied", code)
		}
	}

	var retained int64
	if err := h.sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events WHERE occurred_at = ?`, old).
		Scan(&retained); err != nil {
		t.Fatalf("count retained: %v", err)
	}
	if retained != 0 {
		t.Fatalf("%d expired rows survived the denial-driven sweep", retained)
	}
}

// A restart closes admissions whose outcome was never written, as
// unknown — nothing is replayed.
func TestRestartClosesOrphanAdmissions(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	if _, err := sqlDB.Exec(`INSERT INTO tool_audit_events
		(occurred_at, event, tool_call_id, decision, reason_code, state)
		VALUES (?, 'admission', 'call-1', 'admit', 'ok', 'admitted')`,
		time.Now().UnixMicro()); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	// A fresh process builds its admitter over the same store.
	tooladmission.New(queries, tooladmission.DefaultLimits(), nil, zerolog.Nop())

	var decision, reason, state string
	if err := sqlDB.QueryRow(`SELECT decision, reason_code, state FROM tool_audit_events`).
		Scan(&decision, &reason, &state); err != nil {
		t.Fatalf("read reconciled row: %v", err)
	}
	if decision != "error" || reason != "restart_unknown" || state != "terminal" {
		t.Fatalf("reconciled row = %s/%s/%s, want error/restart_unknown/terminal", decision, reason, state)
	}
}

// Retention drops terminal records past the cutoff and keeps the
// newest N of the rest; admitted records are never deleted.
func TestAuditRetentionProtectsAdmissions(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)

	oldID := insertAudit(t, sqlDB, 1, "admission", "terminal")
	admittedID := insertAudit(t, sqlDB, 2, "admission", "admitted")
	recentID := insertAudit(t, sqlDB, 500, "outcome", "terminal")

	if n, err := queries.DeleteAuditOlderThan(t.Context(), 100); err != nil || n != 1 {
		t.Fatalf("delete older-than: rows=%d err=%v, want 1", n, err)
	}
	if got := auditIDs(t, sqlDB); len(got) != 2 || got[0] != admittedID || got[1] != recentID {
		t.Fatalf("after age prune ids=%v, want [%d %d] (old %d gone)", got, admittedID, recentID, oldID)
	}

	_ = insertAudit(t, sqlDB, 600, "outcome", "terminal")
	if n, err := queries.DeleteAuditOverflow(t.Context(), 1); err != nil || n != 1 {
		t.Fatalf("delete overflow: rows=%d err=%v, want 1", n, err)
	}
	if got := auditIDs(t, sqlDB); len(got) != 2 {
		t.Fatalf("after overflow prune ids=%v, want the admission plus one terminal row", got)
	}
}

// insertAudit lands one record directly and returns its id.
func insertAudit(t *testing.T, sqlDB interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, occurredAt int64, event, state string) int64 {
	t.Helper()
	var id int64
	if err := sqlDB.QueryRowContext(t.Context(), `
INSERT INTO tool_audit_events (occurred_at, event, decision, reason_code, state)
VALUES (?, ?, 'admit', 'ok', ?) RETURNING id`, occurredAt, event, state).Scan(&id); err != nil {
		t.Fatalf("insert audit %s/%s: %v", event, state, err)
	}
	return id
}

// auditIDs lists the surviving record ids in order.
func auditIDs(t *testing.T, sqlDB *sql.DB) []int64 {
	t.Helper()
	rows, err := sqlDB.QueryContext(t.Context(), `SELECT id FROM tool_audit_events ORDER BY id`)
	if err != nil {
		t.Fatalf("select audit ids: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan audit id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("audit ids: %v", err)
	}
	return ids
}

// Approval traffic drives retention too — one Auditor.Record
// sweeps, so expired rows leave on an operator action alone.
func TestApprovalTrafficPrunes(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)

	old := time.Now().Add(-31 * 24 * time.Hour).UnixMicro()
	for range 3 {
		insertAudit(t, sqlDB, old, "approval_change", "terminal")
	}

	auditor := tooladmission.NewAuditor(queries)
	if err := auditor.Record(t.Context(), tooladmission.Decision{
		Event:   tooladmission.EventApprovalChange,
		Outcome: "grant",
		Reason:  "approved",
	}); err != nil {
		t.Fatalf("record approval: %v", err)
	}

	var retained int64
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events WHERE occurred_at = ?`, old).
		Scan(&retained); err != nil {
		t.Fatalf("count expired: %v", err)
	}
	if retained != 0 {
		t.Fatalf("%d expired rows survived the approval-driven sweep", retained)
	}
	var fresh int64
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events WHERE event = 'approval_change' AND occurred_at > ?`, old).
		Scan(&fresh); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if fresh != 1 {
		t.Fatalf("approval record = %d rows, want the one just written", fresh)
	}
}

// A call id that is charset-valid and bounded but equal to a known
// secret is still a payload — it is stored as the hashed handle, never
// the secret itself.
func TestCallIdSecretHashed(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	secret := "canary-secret-value" // [A-Za-z-] only, 19 chars
	h.opts.KnownSecrets = []string{secret}
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ok", nil }}

	if _, err := h.wrap(fake).InvokableRun(tooladmission.WithCallID(t.Context(), secret), `{}`); err != nil {
		t.Fatalf("call: %v", err)
	}
	events := h.auditEvents()
	if len(events) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(events))
	}
	stored := events[0].ToolCallID.String
	if !strings.HasPrefix(stored, "h:") || len(stored) > 33 {
		t.Fatalf("id stored as %q, want the bounded host-derived handle", stored)
	}
	if strings.Contains(stored, secret) {
		t.Fatalf("stored id leaks the secret: %q", stored)
	}
}

// The prune ticker expires aged terminal records without any
// admission, denial or approval write; fresh terminal rows stay.
func TestStartPruneExpiresWithoutTraffic(t *testing.T) {
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	admitter := tooladmission.New(queries, tooladmission.DefaultLimits(), nil, zerolog.Nop())

	old := time.Now().Add(-31 * 24 * time.Hour).UnixMicro()
	for range 3 {
		insertAudit(t, sqlDB, old, "approval_change", "terminal")
	}
	fresh := insertAudit(t, sqlDB, time.Now().UnixMicro(), "denial", "terminal")

	stop := admitter.StartPrune(20 * time.Millisecond)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var stale int64
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events WHERE occurred_at = ?`, old).
			Scan(&stale); err != nil {
			t.Fatalf("count expired: %v", err)
		}
		if stale == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expired rows survived the ticker")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var kept int64
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tool_audit_events WHERE id = ?`, fresh).
		Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("fresh terminal row swept: kept=%d err=%v", kept, err)
	}
}
