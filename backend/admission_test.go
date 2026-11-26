package backend_test

// End-to-end admission through the backend's public surface: the budget
// code paths, cancellation of admitted calls, the advertised-definition
// ceiling, and the audit rows the chain leaves behind. The chain's unit
// behavior lives in internal/tooladmission; this file proves it is
// composed where turns resolve tools.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcptest"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// admissionSetup seeds one authorized MCP tool over a live fixture and
// boots the backend with the given limits.
type admissionSetup struct {
	sqlDB *sql.DB
	svc   *backend.TurnService
	srv   *mcptest.Server
}

// newAdmissionSetup builds the setup; an optional fixture option list
// customizes the server.
func newAdmissionSetup(t *testing.T, limits backend.ToolLimits, opts ...mcptest.Option) *admissionSetup {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	srv := mcptest.Start(t, opts...)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
	})
	testdb.SeedMcpServer(t, sqlDB, "helper", srv.URL, func(params *db.CreateMcpServerParams) {
		params.TimeoutMs = sql.NullInt64{Int64: 5000, Valid: true}
	})
	_, _, svc, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, limits, nil)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	t.Cleanup(svc.CloseTools)

	// Switch every discovered tool on globally and open the parent gate:
	// the agent inherits them with no per-tool rows.
	rows, err := queries.ListTools(t.Context())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, row := range rows {
		if !row.ServerID.Valid || row.ServerID.String != "helper" {
			continue
		}
		if _, err := queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
			Enabled: 1, ID: row.ID,
		}); err != nil {
			t.Fatalf("enable %q: %v", row.ID, err)
		}
	}
	if _, err := queries.SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{
		AgentID: "research", ServerID: "helper", Enabled: 1,
	}); err != nil {
		t.Fatalf("approve gate: %v", err)
	}
	return &admissionSetup{sqlDB: sqlDB, svc: svc, srv: srv}
}

// toolNamed returns the wrapped invokable the turn would attach.
func (s *admissionSetup) toolNamed(t *testing.T, modelName string) tool.InvokableTool {
	t.Helper()
	resolved, err := s.svc.AgentTools(t.Context(), "research")
	if err != nil {
		t.Fatalf("agent tools: %v", err)
	}
	for i, impl := range resolved.Impls {
		if resolved.Infos[i].Name == modelName {
			return impl
		}
	}
	t.Fatalf("tool %q not in the turn set (%d tools)", modelName, len(resolved.Impls))
	return nil
}

// A full per-server concurrency budget delays a call instead of failing
// it: both callers are admitted, through the composed chain, and the
// audit trail pairs each admission with its terminal outcome.
func TestAgentToolsConcurrencyWaits(t *testing.T) {
	limits := backend.DefaultToolLimits()
	limits.ConcurrencyPerServer = 1
	setup := newAdmissionSetup(t, limits, mcptest.WithDelay(50*time.Millisecond))
	echo := setup.toolNamed(t, "helper_echo")

	// Two simultaneous callers, one slot: both complete.
	start := make(chan struct{})
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			result, err := echo.InvokableRun(context.Background(), `{"text":"x"}`)
			results <- result
			errs <- err
		}()
	}
	close(start)

	for range 2 {
		result, err := <-results, <-errs
		if err != nil {
			t.Fatalf("call errored: %v", err)
		}
		if result != "x" {
			t.Fatalf("call result = %q, want both callers admitted", result)
		}
	}

	rows, err := setup.sqlDB.Query(`SELECT event, decision, reason_code, state, tool_id
		FROM tool_audit_events ORDER BY id`)
	if err != nil {
		t.Fatalf("select audit: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var terminal int
	for rows.Next() {
		var event, decision, reason, state, toolID string
		if err := rows.Scan(&event, &decision, &reason, &state, &toolID); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		if toolID != "helper:echo" {
			t.Fatalf("audit row for %q, want the catalog id", toolID)
		}
		if state == "terminal" {
			terminal++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	if terminal != 2 {
		t.Fatalf("terminal audit rows = %d, want one per admitted call", terminal)
	}
}

// Revocation cancels admitted calls through their real request contexts
// and never claims the remote action was rolled back.
func TestRevocationCancelsInFlight(t *testing.T) {
	setup := newAdmissionSetup(t, backend.DefaultToolLimits(), mcptest.WithDelay(1500*time.Millisecond))
	echo := setup.toolNamed(t, "helper_echo")

	type outcome struct {
		result string
		err    error
	}
	base := setup.srv.Calls()
	done := make(chan outcome, 1)
	go func() {
		result, err := echo.InvokableRun(context.Background(), `{"text":"slow"}`)
		done <- outcome{result: result, err: err}
	}()
	// Wait until the invocation's own request reached the fixture.
	deadline := time.Now().Add(5 * time.Second)
	for setup.srv.Calls() <= base && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if setup.srv.Calls() <= base {
		t.Fatal("the invocation never reached the fixture")
	}
	if cancelled := setup.svc.CancelServerTools("helper"); cancelled != 1 {
		t.Fatalf("cancelled %d calls, want 1", cancelled)
	}
	select {
	case out := <-done:
		if out.err == nil {
			t.Fatalf("cancelled call returned %q with no error", out.result)
		}
		if !strings.Contains(out.err.Error(), "remote outcome unknown") {
			t.Fatalf("error = %q, want the unknown-outcome wording", out.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revocation did not reach the invocation")
	}
}

// The per-turn advertised-definition ceiling fails visibly and names the
// largest offenders: tools are never silently hidden.
func TestToolsetBudgetExceeded(t *testing.T) {
	// 40 tools × 4 KiB descriptions ≈ 160 KiB, over the 128 KiB budget.
	setup := newAdmissionSetup(t, backend.DefaultToolLimits(),
		mcptest.WithDescribedTools(40, 4<<10))

	_, err := setup.svc.AgentTools(t.Context(), "research")
	if err == nil || !strings.Contains(err.Error(), "toolset budget exceeded") {
		t.Fatalf("agent tools err = %v, want the toolset budget failure", err)
	}
	if !strings.Contains(err.Error(), "described-") {
		t.Fatalf("error %q does not name the largest definitions", err)
	}
}
