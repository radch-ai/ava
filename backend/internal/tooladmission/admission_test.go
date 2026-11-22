package tooladmission_test

// The admission chain: authorization, structured denials, budgets,
// audit-before-dispatch, deadlines, cancellation, scrubbing and output
// bounds.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// fakeTool is one controllable invokable.
type fakeTool struct {
	info  *schema.ToolInfo
	run   func(ctx context.Context, args string) (string, error)
	calls atomic.Int32
}

// Info reports the advertised metadata.
func (f *fakeTool) Info(context.Context) (*schema.ToolInfo, error) { return f.toolInfo(), nil }

// toolInfo returns the advertised metadata, filling a default when unset.
func (f *fakeTool) toolInfo() *schema.ToolInfo {
	if f.info == nil {
		f.info = &schema.ToolInfo{Name: "gh_search", Desc: "search"}
	}
	return f.info
}

// InvokableRun records the call and runs the fixture's behavior.
func (f *fakeTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	f.calls.Add(1)
	return f.run(ctx, args)
}

// harness is one admission setup: the store, the admitter and the
// options an authorized call would carry.
type harness struct {
	t        *testing.T
	sqlDB    *sql.DB
	queries  *db.Queries
	admitter *tooladmission.Admitter
	registry *tooladmission.CancelRegistry
	opts     tooladmission.Options
}

// newHarness seeds one authorized MCP tool: server row, globally enabled
// catalog row, and the agent's parent gate on — inheritance does the
// rest, so the agent holds no per-tool row at all.
func newHarness(t *testing.T, limits tooladmission.Limits) *harness {
	t.Helper()
	sqlDB := testdb.Open(t, true)
	queries := db.New(sqlDB)
	ctx := t.Context()
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID: "a1", Name: "Agent", Icon: "globe",
	})
	if err := queries.CreateMcpServer(ctx, db.CreateMcpServerParams{
		ID: "gh", Name: "GitHub",
		Url:        sql.NullString{String: "https://example.test/mcp", Valid: true},
		HeaderKeys: "[]",
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := queries.UpsertMcpTool(ctx, db.UpsertMcpToolParams{
		ID: "gh:search", Name: "search", Description: "search",
		ServerID: sql.NullString{String: "gh", Valid: true}, DefinitionJson: `{}`,
	}); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
	if _, err := queries.ToggleToolEnabled(ctx, db.ToggleToolEnabledParams{
		Enabled: 1, ID: "gh:search",
	}); err != nil {
		t.Fatalf("enable tool: %v", err)
	}
	if _, err := queries.SetAgentMcpEnabled(ctx, db.SetAgentMcpEnabledParams{
		AgentID: "a1", ServerID: "gh", Enabled: 1,
	}); err != nil {
		t.Fatalf("gate: %v", err)
	}
	registry := tooladmission.NewCancelRegistry()
	return &harness{
		t:        t,
		sqlDB:    sqlDB,
		queries:  queries,
		admitter: tooladmission.New(queries, limits, registry, zerolog.Nop()),
		registry: registry,
		opts: tooladmission.Options{
			AgentID:   "a1",
			ServerID:  "gh",
			CatalogID: "gh:search",
			Timeout:   time.Second,
		},
	}
}

// wrap rides the fake tool on the admission chain.
func (h *harness) wrap(fake *fakeTool) tool.InvokableTool {
	h.t.Helper()
	return h.admitter.Wrap(fake, fake.toolInfo(), h.opts)
}

// seedBuiltin installs one server-less built-in through the boot-sync
// upsert (UpsertTool): built-ins install globally ON, so every agent
// inherits it. It returns the options the built-in's wrapper carries.
func (h *harness) seedBuiltin(toolID string) tooladmission.Options {
	h.t.Helper()
	if err := h.queries.UpsertTool(h.t.Context(), db.UpsertToolParams{
		ID: toolID, Name: "fetch", Description: "fetch a document", Builtin: 1,
	}); err != nil {
		h.t.Fatalf("seed builtin %s: %v", toolID, err)
	}
	return tooladmission.Options{
		AgentID:   "a1",
		ServerID:  "", // a built-in has no server
		CatalogID: toolID,
		Timeout:   2 * time.Second,
	}
}

// denialCode decodes a structured denial's code, failing on any other
// shape.
func denialCode(t *testing.T, result string) string {
	t.Helper()
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || out.Error.Code == "" {
		t.Fatalf("result = %q, want a structured denial", result)
	}
	return out.Error.Code
}

// denyOnce runs one call and returns its denial code.
func (h *harness) denyOnce(fake *fakeTool) string {
	h.t.Helper()
	result, err := h.wrap(fake).InvokableRun(h.t.Context(), `{"q":"x"}`)
	if err != nil {
		h.t.Fatalf("denial returned an error: %v", err)
	}
	return denialCode(h.t, result)
}

// auditEvents lists one tool's audit rows in order.
func (h *harness) auditEvents() []db.ToolAuditEvent {
	h.t.Helper()
	rows, err := h.sqlDB.Query(`SELECT id, occurred_at, event, agent_id, server_id, tool_id,
		tool_call_id, decision, reason_code, count, state
		FROM tool_audit_events ORDER BY id`)
	if err != nil {
		h.t.Fatalf("select audit: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []db.ToolAuditEvent
	for rows.Next() {
		var (
			event                         db.ToolAuditEvent
			agent, server, toolID, callID sql.NullString
			count                         sql.NullInt64
		)
		if err := rows.Scan(&event.ID, &event.OccurredAt, &event.Event, &agent, &server, &toolID,
			&callID, &event.Decision, &event.ReasonCode, &count, &event.State); err != nil {
			h.t.Fatalf("scan audit: %v", err)
		}
		event.AgentID, event.ServerID, event.ToolID = agent, server, toolID
		event.ToolCallID, event.Count = callID, count
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("audit rows: %v", err)
	}
	return out
}

// A globally disabled tool is denied as a structured outcome and never
// dispatched.
func TestPolicyDeniedNeverDispatches(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }}

	// The switch moved between turns: authorization is per call.
	if _, err := h.queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 0, ID: "gh:search",
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if code := h.denyOnce(fake); code != tooladmission.CodePolicyDenied {
		t.Fatalf("denial code = %q, want policy_denied", code)
	}
	if fake.calls.Load() != 0 {
		t.Fatalf("denied call dispatched %d times", fake.calls.Load())
	}
	if events := h.auditEvents(); len(events) != 1 || events[0].Event != tooladmission.EventDenial {
		t.Fatalf("audit = %+v, want one denial record", events)
	}
}

// A full per-server budget delays a call instead of failing it: the
// waiter is admitted exactly once, when the holder releases its slot.
func TestCapacityWaitAdmitsOnRelease(t *testing.T) {
	limits := tooladmission.DefaultLimits()
	limits.ConcurrencyPerServer = 1
	h := newHarness(t, limits)

	release := make(chan struct{})
	blocking := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		select {
		case <-release:
			return "held", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	holder := h.wrap(blocking)

	done := make(chan structural, 1)
	go func() {
		result, err := holder.InvokableRun(context.Background(), `{}`)
		done <- structural{result: result, err: err}
	}()
	// Wait until the holder occupies the only slot.
	deadline := time.Now().Add(2 * time.Second)
	for blocking.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if blocking.calls.Load() == 0 {
		t.Fatal("holder never started")
	}

	waiter := &fakeTool{run: func(context.Context, string) (string, error) { return "waited", nil }}
	waited := make(chan structural, 1)
	go func() {
		result, err := h.wrap(waiter).InvokableRun(context.Background(), `{}`)
		waited <- structural{result: result, err: err}
	}()

	// The waiter is queued, not dispatched, while the slot is taken.
	time.Sleep(50 * time.Millisecond)
	if waiter.calls.Load() != 0 {
		t.Fatalf("waiter dispatched %d times while the slot was taken", waiter.calls.Load())
	}

	close(release)
	holderOut := <-done
	if holderOut.err != nil || holderOut.result != "held" {
		t.Fatalf("holder = (%q, %v), want its own result", holderOut.result, holderOut.err)
	}
	waiterOut := <-waited
	if waiterOut.err != nil || waiterOut.result != "waited" {
		t.Fatalf("waiter = (%q, %v), want admission after the release", waiterOut.result, waiterOut.err)
	}
	if waiter.calls.Load() != 1 {
		t.Fatalf("waiter dispatched %d times, want exactly once", waiter.calls.Load())
	}
}

// A wait past the invocation deadline is the deadline outcome, and the
// remote is never dialed: waiting is bounded work, not a queued call.
func TestCapacityWaitDeadlineExceeded(t *testing.T) {
	limits := tooladmission.DefaultLimits()
	limits.ConcurrencyPerServer = 1
	h := newHarness(t, limits)

	release := make(chan struct{})
	started := make(chan struct{})
	holder := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		close(started)
		select {
		case <-release:
			return "held", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}

	holderDone := make(chan structural, 1)
	go func() {
		result, err := h.wrap(holder).InvokableRun(context.Background(), `{}`)
		holderDone <- structural{result: result, err: err}
	}()
	<-started

	waiterOpts := h.opts
	waiterOpts.Timeout = 30 * time.Millisecond
	waiter := &fakeTool{run: func(context.Context, string) (string, error) { return "waited", nil }}
	result, err := h.admitter.Wrap(waiter, waiter.toolInfo(), waiterOpts).
		InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("waiter errored: %v", err)
	}
	if code := denialCode(t, result); code != tooladmission.CodeDeadlineExceeded {
		t.Fatalf("code = %q, want deadline_exceeded", code)
	}
	if waiter.calls.Load() != 0 {
		t.Fatalf("timed-out waiter dispatched %d times", waiter.calls.Load())
	}

	close(release)
	<-holderDone
}

// A call that waited past a permission change is denied before
// dispatch: the authorization that counts is the one after the wait.
func TestWaiterRechecksAuthorization(t *testing.T) {
	limits := tooladmission.DefaultLimits()
	limits.ConcurrencyPerServer = 1
	h := newHarness(t, limits)

	release := make(chan struct{})
	started := make(chan struct{})
	holder := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		close(started)
		select {
		case <-release:
			return "held", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	holderDone := make(chan structural, 1)
	go func() {
		result, err := h.wrap(holder).InvokableRun(context.Background(), `{}`)
		holderDone <- structural{result: result, err: err}
	}()
	<-started

	waiter := &fakeTool{run: func(context.Context, string) (string, error) { return "waited", nil }}
	waited := make(chan structural, 1)
	go func() {
		result, err := h.wrap(waiter).InvokableRun(context.Background(), `{}`)
		waited <- structural{result: result, err: err}
	}()
	// Let the waiter queue behind the holder.
	time.Sleep(50 * time.Millisecond)

	// The permission changes while the waiter holds no capacity.
	if _, err := h.queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 0, ID: "gh:search",
	}); err != nil {
		t.Fatalf("disable tool: %v", err)
	}
	close(release)
	<-holderDone

	out := <-waited
	if out.err != nil {
		t.Fatalf("waiter errored: %v", out.err)
	}
	if code := denialCode(t, out.result); code != tooladmission.CodePolicyDenied {
		t.Fatalf("code = %q, want policy_denied after the wait", code)
	}
	if waiter.calls.Load() != 0 {
		t.Fatalf("waiter dispatched %d times despite the denial", waiter.calls.Load())
	}
}

// structured carries one goroutine's invocation result.
type structural struct {
	result string
	err    error
}

// No durable admission record means no execution.
func TestAuditUnavailableDeniesWithoutDispatch(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }}

	if _, err := h.sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}
	if code := h.denyOnce(fake); code != tooladmission.CodeAuditUnavailable {
		t.Fatalf("code = %q, want audit_unavailable", code)
	}
	if fake.calls.Load() != 0 {
		t.Fatal("call executed without an admission record")
	}
}

// Caller-supplied arguments carrying a known secret are denied before
// dispatch and never rewritten.
func TestOutboundCredentialUseDenied(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	h.opts.KnownSecrets = []string{"canary-secret-value"}
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }}

	result, err := h.wrap(fake).InvokableRun(t.Context(), `{"q":"canary-secret-value"}`)
	if err != nil {
		t.Fatalf("denial errored: %v", err)
	}
	if code := denialCode(t, result); code != tooladmission.CodeCredentialUseDenied {
		t.Fatalf("code = %q, want credential_use_denied", code)
	}
	if fake.calls.Load() != 0 {
		t.Fatal("credential-bearing call dispatched")
	}
}

// Results are sanitized before delivery, and the audit records the
// redaction without storing any payload.
func TestResultScrubbed(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	h.opts.KnownSecrets = []string{"canary-secret-value"}
	fake := &fakeTool{run: func(context.Context, string) (string, error) {
		return `{"echo":"canary-secret-value"}`, nil
	}}

	result, err := h.wrap(fake).InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if strings.Contains(result, "canary-secret-value") {
		t.Fatalf("result leaked the secret: %q", result)
	}
	if !strings.Contains(result, "[REDACTED:secret]") {
		t.Fatalf("result = %q, want the static marker", result)
	}

	// One admission row closes into its terminal state; the redaction is
	// its own point-in-time record.
	events := h.auditEvents()
	var closedAdmission, sawRedaction bool
	for _, event := range events {
		switch event.Event {
		case tooladmission.EventAdmission:
			closedAdmission = event.State == "terminal" && event.Decision == "complete"
		case tooladmission.EventRedaction:
			sawRedaction = true
		}
		for _, nullable := range []sql.NullString{
			event.AgentID, event.ServerID, event.ToolID, event.ToolCallID,
		} {
			if strings.Contains(nullable.String, "canary-secret-value") {
				t.Fatalf("audit row stored the secret: %+v", event)
			}
		}
	}
	if !closedAdmission || !sawRedaction {
		t.Fatalf("audit events = %+v, want a closed admission and a redaction", events)
	}
}

// Plain-text overflow truncates inside the ceiling with the marker; it
// never becomes a raw partial body.
func TestOutcomeBoundPlainText(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) {
		return strings.Repeat("é", tooladmission.MaxDeliveredBytes), nil
	}}

	result, err := h.wrap(fake).InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(result) > tooladmission.MaxDeliveredBytes {
		t.Fatalf("delivered %d bytes, over the ceiling", len(result))
	}
	if !strings.HasSuffix(result, "[truncated by output limit]") {
		t.Fatalf("result tail = %q, want the truncation marker", result[len(result)-40:])
	}
	if !strings.HasPrefix(result, "é") {
		t.Fatal("truncation broke UTF-8")
	}
}

// Structured overflow is a safe outcome, never malformed JSON.
func TestOutcomeBoundStructured(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) {
		body, err := json.Marshal(map[string]string{"blob": strings.Repeat("x", tooladmission.MaxDeliveredBytes)})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(body), nil
	}}

	result, err := h.wrap(fake).InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if code := denialCode(t, result); code != tooladmission.CodeOutputLimitExceeded {
		t.Fatalf("code = %q, want output_limit_exceeded", code)
	}
	if strings.Contains(result, "xxxx") {
		t.Fatal("partial structured body surfaced")
	}
}

// A registered call observes cancellation: revocation cancels the actual
// request context, and the host reports an unknown remote outcome.
func TestCancellationMidFlight(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	started := make(chan struct{})
	fake := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}}

	done := make(chan structural, 1)
	go func() {
		result, err := h.wrap(fake).InvokableRun(context.Background(), `{}`)
		done <- structural{result: result, err: err}
	}()
	<-started
	deadline := time.Now().Add(2 * time.Second)
	for h.registry.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if cancelled := h.registry.CancelServer("gh"); cancelled != 1 {
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
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not reach the invocation")
	}
}

// An expired call budget returns the safe outcome instead of hanging or
// reporting a rollback.
func TestDeadlineExceeded(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	h.opts.Timeout = 50 * time.Millisecond
	fake := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}

	start := time.Now()
	result, err := h.wrap(fake).InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("expired call errored: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("call took %v despite a 50ms budget", elapsed)
	}
	if code := denialCode(t, result); code != tooladmission.CodeDeadlineExceeded {
		t.Fatalf("code = %q, want deadline_exceeded", code)
	}
}

// A genuine dispatch failure stays a Go error: the turn fails at message
// level rather than pretending the tool answered.
func TestDispatchFailureIsAnError(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) {
		return "", errors.New("upstream exploded")
	}}
	_, err := h.wrap(fake).InvokableRun(t.Context(), `{}`)
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("err = %v, want the dispatch failure", err)
	}
}

// Concurrent callers share the capacity instead of overspending it:
// every caller is eventually admitted, and the observed concurrency
// never exceeds the global budget.
func TestConcurrentAdmissionDoesNotOverspend(t *testing.T) {
	limits := tooladmission.DefaultLimits()
	limits.ConcurrencyPerServer = 2
	limits.ConcurrencyGlobal = 3
	h := newHarness(t, limits)

	var inFlight, peak atomic.Int32
	fake := &fakeTool{run: func(context.Context, string) (string, error) {
		n := inFlight.Add(1)
		for {
			seen := peak.Load()
			if n <= seen || peak.CompareAndSwap(seen, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inFlight.Add(-1)
		return "ok", nil
	}}
	wrapped := h.wrap(fake)

	const callers = 32
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := wrapped.InvokableRun(context.Background(), `{}`)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("call errored: %v", err)
				return
			}
			if result != "ok" {
				t.Errorf("call result = %q, want every caller admitted", result)
				return
			}
			admitted++
		}()
	}
	wg.Wait()
	if admitted != callers {
		t.Fatalf("%d of %d callers admitted, want all (waiting, not rejecting)", admitted, callers)
	}
	if got := fake.calls.Load(); int(got) != callers {
		t.Fatalf("dispatched %d calls but admitted %d", got, admitted)
	}
	if got := peak.Load(); int(got) > limits.ConcurrencyGlobal {
		t.Fatalf("observed %d concurrent calls, over the global budget %d", got, limits.ConcurrencyGlobal)
	}
}

// A lost outcome write preserves the observed result and blocks further
// admissions until the record is repaired: the trail may lag, never lie.
func TestOutcomeFailureBlocksAdmissionsUntilRecovered(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "observed", nil }}
	wrapped := h.wrap(fake)

	// The outcome update cannot land.
	if _, err := h.sqlDB.Exec(`CREATE TRIGGER fail_outcome BEFORE UPDATE ON tool_audit_events
		BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}
	result, err := wrapped.InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if result != "observed" {
		t.Fatalf("result = %q, want the observed outcome preserved", result)
	}

	// The next admission is denied: no execution on an incomplete trail.
	result, err = wrapped.InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("blocked call errored: %v", err)
	}
	if code := denialCode(t, result); code != tooladmission.CodeAuditUnavailable {
		t.Fatalf("code = %q, want audit_unavailable", code)
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("blocked call dispatched: %d executions", fake.calls.Load())
	}

	// Recovery: the retried outcome write lands, then admissions resume.
	if _, err := h.sqlDB.Exec(`DROP TRIGGER fail_outcome;`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	result, err = wrapped.InvokableRun(t.Context(), `{}`)
	if err != nil || result != "observed" {
		t.Fatalf("post-recovery call = %q, %v; want the tool's result", result, err)
	}
	if fake.calls.Load() != 2 {
		t.Fatalf("executions = %d, want 2", fake.calls.Load())
	}
	// Both admissions are terminal now: nothing is left for a later repair.
	for _, event := range h.auditEvents() {
		if event.Event == tooladmission.EventAdmission && event.State != "terminal" {
			t.Fatalf("audit row %d left %q after recovery", event.ID, event.State)
		}
	}
}

// Every structured denial carries exactly a code — no retry hint, no
// prose — so the model sees a policy outcome. Each case is produced by
// the real chain, not by the renderer alone.
func TestDenialOutcomeShape(t *testing.T) {
	// runDenial returns the raw structured outcome of one call.
	runDenial := func(t *testing.T, h *harness, fake *fakeTool) string {
		t.Helper()
		result, err := h.wrap(fake).InvokableRun(t.Context(), `{}`)
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return result
	}

	denyWith := map[string]func(t *testing.T) string{
		tooladmission.CodePolicyDenied: func(t *testing.T) string {
			h := newHarness(t, tooladmission.DefaultLimits())
			if _, err := h.queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
				Enabled: 0, ID: "gh:search",
			}); err != nil {
				t.Fatalf("disable: %v", err)
			}
			return runDenial(t, h, &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }})
		},
		tooladmission.CodeCredentialUseDenied: func(t *testing.T) string {
			h := newHarness(t, tooladmission.DefaultLimits())
			h.opts.KnownSecrets = []string{"s3cr3t-value"}
			fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }}
			result, err := h.wrap(fake).InvokableRun(t.Context(), `{"q":"s3cr3t-value"}`)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			return result
		},
		tooladmission.CodeAuditUnavailable: func(t *testing.T) string {
			h := newHarness(t, tooladmission.DefaultLimits())
			if _, err := h.sqlDB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON tool_audit_events
				BEGIN SELECT RAISE(ABORT, 'audit down'); END;`); err != nil {
				t.Fatalf("install trigger: %v", err)
			}
			return runDenial(t, h, &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }})
		},
		tooladmission.CodeDeadlineExceeded: func(t *testing.T) string {
			h := newHarness(t, tooladmission.DefaultLimits())
			h.opts.Timeout = 20 * time.Millisecond
			fake := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			}}
			result, err := h.wrap(fake).InvokableRun(context.Background(), `{}`)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			return result
		},
		tooladmission.CodeOutputLimitExceeded: func(t *testing.T) string {
			h := newHarness(t, tooladmission.DefaultLimits())
			fake := &fakeTool{run: func(context.Context, string) (string, error) {
				body, err := json.Marshal(map[string]string{"blob": strings.Repeat("x", tooladmission.MaxDeliveredBytes)})
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				return string(body), nil
			}}
			result, err := h.wrap(fake).InvokableRun(t.Context(), `{}`)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			return result
		},
	}

	for code, produce := range denyWith {
		encoded := produce(t)
		var out struct {
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal([]byte(encoded), &out); err != nil {
			t.Fatalf("%s: not JSON: %q", code, encoded)
		}
		if out.Error["code"] != code {
			t.Fatalf("code = %v, want %q", out.Error["code"], code)
		}
		if len(out.Error) != 1 {
			t.Fatalf("%s: denial carries %v, want the code alone", code, out.Error)
		}
	}
}

// A server-less built-in executes on its global switch and opt-out
// alone — no parent gate, no server row — and its switch off is the
// denial.
func TestBuiltinExecutesWithoutServerGate(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	opts := h.seedBuiltin("web_fetch")
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }}
	wrapped := h.admitter.Wrap(fake, fake.toolInfo(), opts)

	result, err := wrapped.InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("builtin call: %v", err)
	}
	if result != "ran" || fake.calls.Load() != 1 {
		t.Fatalf("builtin = %q after %d dispatches, want one clean run", result, fake.calls.Load())
	}
	if events := h.auditEvents(); len(events) != 1 || events[0].Event != tooladmission.EventAdmission || events[0].ServerID.Valid {
		t.Fatalf("audit = %+v, want one server-less admission row", events)
	}

	// The twin: the global switch off denies the built-in pre-dispatch.
	if _, err := h.queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 0, ID: "web_fetch",
	}); err != nil {
		t.Fatalf("disable builtin: %v", err)
	}
	result, err = wrapped.InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("denied builtin errored: %v", err)
	}
	if code := denialCode(t, result); code != tooladmission.CodePolicyDenied {
		t.Fatalf("code = %q, want policy_denied", code)
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("disabled builtin dispatched again: %d calls", fake.calls.Load())
	}
}

// Two servers hold their own slots: a contender queues behind its
// server's holder and is admitted when that holder releases, while the
// other server keeps serving. Meaningful under -race: the lease and the
// slot map are shared across both.
func TestTwoServersConcurrentSlots(t *testing.T) {
	limits := tooladmission.DefaultLimits()
	limits.ConcurrencyPerServer = 1
	limits.ConcurrencyGlobal = 2
	h := newHarness(t, limits)

	// A second server with its own globally enabled tool, gated for a1.
	if err := h.queries.CreateMcpServer(t.Context(), db.CreateMcpServerParams{
		ID: "gl", Name: "GitLab",
		Url:        sql.NullString{String: "https://example.test/gl", Valid: true},
		HeaderKeys: "[]",
	}); err != nil {
		t.Fatalf("seed second server: %v", err)
	}
	if err := h.queries.UpsertMcpTool(t.Context(), db.UpsertMcpToolParams{
		ID: "gl:search", Name: "search", Description: "search",
		ServerID: sql.NullString{String: "gl", Valid: true}, DefinitionJson: `{}`,
	}); err != nil {
		t.Fatalf("seed second tool: %v", err)
	}
	if _, err := h.queries.ToggleToolEnabled(t.Context(), db.ToggleToolEnabledParams{
		Enabled: 1, ID: "gl:search",
	}); err != nil {
		t.Fatalf("enable second tool: %v", err)
	}
	if _, err := h.queries.SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{
		AgentID: "a1", ServerID: "gl", Enabled: 1,
	}); err != nil {
		t.Fatalf("gate second server: %v", err)
	}

	// rounds drives one server's release/wait-admit loop.
	rounds := func(opts tooladmission.Options) {
		newCall := func(release chan struct{}) (*fakeTool, tool.InvokableTool) {
			fake := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
				select {
				case <-release:
					return "ok", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}}
			return fake, h.admitter.Wrap(fake, fake.toolInfo(), opts)
		}
		for range 8 {
			release := make(chan struct{})
			holderFake, holder := newCall(release)
			done := make(chan structural, 1)
			go func() {
				result, err := holder.InvokableRun(context.Background(), `{}`)
				done <- structural{result: result, err: err}
			}()
			deadline := time.Now().Add(2 * time.Second)
			for holderFake.calls.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if holderFake.calls.Load() != 1 {
				t.Errorf("%s: holder never dispatched", opts.ServerID)
				close(release)
				<-done
				return
			}
			// The slot is held through dispatch: the contender waits.
			contenderFake, contender := newCall(release)
			contenderDone := make(chan structural, 1)
			go func() {
				result, err := contender.InvokableRun(context.Background(), `{}`)
				contenderDone <- structural{result: result, err: err}
			}()
			time.Sleep(20 * time.Millisecond)
			if contenderFake.calls.Load() != 0 {
				t.Errorf("%s: contender dispatched while the slot was taken", opts.ServerID)
			}
			close(release)
			holderOut := <-done
			if holderOut.err != nil || holderOut.result != "ok" {
				t.Errorf("%s: holder = (%q, %v)", opts.ServerID, holderOut.result, holderOut.err)
			}
			contenderOut := <-contenderDone
			if contenderOut.err != nil || contenderOut.result != "ok" {
				t.Errorf("%s: contender = (%q, %v), want admission after the release",
					opts.ServerID, contenderOut.result, contenderOut.err)
			}
		}
	}

	ghOpts := h.opts
	ghOpts.Timeout = 2 * time.Second
	glOpts := ghOpts
	glOpts.CatalogID = "gl:search"
	glOpts.ServerID = "gl"
	var wg sync.WaitGroup
	for _, opts := range []tooladmission.Options{ghOpts, glOpts} {
		wg.Add(1)
		go func(opts tooladmission.Options) {
			defer wg.Done()
			rounds(opts)
		}(opts)
	}
	wg.Wait()
}

// An agent-scoped withdrawal cancels only that agent's in-flight
// call; another agent's call on the same server runs to completion.
func TestAgentScopedCancelSparesOtherAgent(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())

	// a2 inherits the same tool and holds the same gate on gh.
	testdb.SeedAgent(t, h.sqlDB, db.CreateAgentParams{
		ID: "a2", Name: "Agent 2", Icon: "globe",
	})
	if _, err := h.queries.SetAgentMcpEnabled(t.Context(), db.SetAgentMcpEnabledParams{
		AgentID: "a2", ServerID: "gh", Enabled: 1,
	}); err != nil {
		t.Fatalf("gate second agent: %v", err)
	}

	started := make(chan struct{}, 2)
	finish := make(chan struct{})
	victim := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}}
	spared := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		started <- struct{}{}
		select {
		case <-finish:
			return "ok-b", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	sparedOpts := h.opts
	sparedOpts.AgentID = "a2"

	done := make(chan structural, 2)
	for _, call := range []struct {
		fake *fakeTool
		opts tooladmission.Options
	}{{victim, h.opts}, {spared, sparedOpts}} {
		go func(fake *fakeTool, opts tooladmission.Options) {
			result, err := h.admitter.Wrap(fake, fake.toolInfo(), opts).InvokableRun(context.Background(), `{}`)
			done <- structural{result: result, err: err}
		}(call.fake, call.opts)
	}
	<-started
	<-started
	if got := h.registry.Len(); got != 2 {
		t.Fatalf("registry holds %d calls, want 2", got)
	}

	if n := h.registry.CancelAgentServer("a1", "gh"); n != 1 {
		t.Fatalf("agent-scoped cancel hit %d calls, want exactly a1's", n)
	}
	close(finish)

	outcomes := []structural{<-done, <-done}
	var canceledOutcome, sparedOutcome *structural
	for i := range outcomes {
		if outcomes[i].err != nil && strings.Contains(outcomes[i].err.Error(), "remote outcome unknown") {
			canceledOutcome = &outcomes[i]
			continue
		}
		if outcomes[i].err == nil && outcomes[i].result == "ok-b" {
			sparedOutcome = &outcomes[i]
		}
	}
	if canceledOutcome == nil || sparedOutcome == nil {
		t.Fatalf("outcomes = (%q, %v) and (%q, %v), want one canceled and one completed",
			outcomes[0].result, outcomes[0].err, outcomes[1].result, outcomes[1].err)
	}
	if got := h.registry.Len(); got != 0 {
		t.Fatalf("registry holds %d calls after both returned", got)
	}
}

// The registry carries builtins with an empty server id, so an
// agent-scoped withdrawal reaches them too.
func TestBuiltinCancellation(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	opts := h.seedBuiltin("web_fetch")
	started := make(chan struct{})
	fake := &fakeTool{run: func(ctx context.Context, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}}

	done := make(chan structural, 1)
	go func() {
		result, err := h.admitter.Wrap(fake, fake.toolInfo(), opts).InvokableRun(context.Background(), `{}`)
		done <- structural{result: result, err: err}
	}()
	<-started
	deadline := time.Now().Add(2 * time.Second)
	for h.registry.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := h.registry.CancelAgentServer("a1", ""); n != 1 {
		t.Fatalf("agent-scoped cancel hit %d calls, want the builtin's", n)
	}
	select {
	case out := <-done:
		if out.err == nil || !strings.Contains(out.err.Error(), "remote outcome unknown") {
			t.Fatalf("canceled builtin returned (%q, %v)", out.result, out.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not reach the builtin call")
	}
}

// Escaped spellings are inspected in their decoded form — a
// \u-escaped secret in the arguments is still the secret, and the call
// is still denied before dispatch.
func TestEscapedCredentialArgumentDenied(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	h.opts.KnownSecrets = []string{"canary-secret-value"}
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return "ran", nil }}

	result, err := h.wrap(fake).InvokableRun(t.Context(), `{"q":"\u0063anary-secret-value"}`)
	if err != nil {
		t.Fatalf("denial errored: %v", err)
	}
	if code := denialCode(t, result); code != tooladmission.CodeCredentialUseDenied {
		t.Fatalf("code = %q, want credential_use_denied", code)
	}
	if fake.calls.Load() != 0 {
		t.Fatal("escaped-credential call dispatched")
	}
}

// The delivered-output boundary: exactly MaxDeliveredBytes passes through
// untouched — the truncation marker is reserved for anything past it.
func TestOutcomeExactLimitPasses(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	want := strings.Repeat("x", tooladmission.MaxDeliveredBytes)
	fake := &fakeTool{run: func(context.Context, string) (string, error) { return want, nil }}

	result, err := h.wrap(fake).InvokableRun(t.Context(), `{}`)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result != want {
		t.Fatalf("ceiling-sized result altered: %d bytes, marker=%v, want %d unchanged",
			len(result), strings.Contains(result, "[truncated by output limit]"), len(want))
	}
	if strings.Contains(result, "[truncated by output limit]") {
		t.Fatal("ceiling-sized result carries the truncation marker")
	}
}

// The failure channel is bounded and scrubbed like a result. An
// oversized plain-text error truncates inside the ceiling without
// leaking the secret (and the redaction is recorded); a JSON-shaped
// oversized error comes back as the output_limit_exceeded denial shape.
func TestErrorPathBounded(t *testing.T) {
	h := newHarness(t, tooladmission.DefaultLimits())
	h.opts.KnownSecrets = []string{"canary-secret-value"}

	plain := &fakeTool{run: func(context.Context, string) (string, error) {
		return "", errors.New("boom canary-secret-value " + strings.Repeat("e", tooladmission.MaxDeliveredBytes))
	}}
	result, err := h.wrap(plain).InvokableRun(t.Context(), `{}`)
	if err == nil {
		t.Fatalf("plain error path returned %q with no error", result)
	}
	if got := err.Error(); len(got) > tooladmission.MaxDeliveredBytes {
		t.Fatalf("error text is %d bytes, over the ceiling", len(got))
	}
	if strings.Contains(err.Error(), "canary-secret-value") {
		t.Fatal("error text leaked the secret")
	}
	if !strings.HasSuffix(err.Error(), "[truncated by output limit]") {
		t.Fatalf("error tail = %q, want the truncation marker", err.Error()[len(err.Error())-40:])
	}

	// A JSON-valid oversized error body takes the structured overflow
	// arm: the denial shape is both the result and the error text.
	structured := &fakeTool{run: func(context.Context, string) (string, error) {
		return "", errors.New(`"` + strings.Repeat("x", tooladmission.MaxDeliveredBytes) + `"`)
	}}
	result, err = h.wrap(structured).InvokableRun(t.Context(), `{}`)
	if err == nil {
		t.Fatalf("structured error path returned %q with no error", result)
	}
	if code := denialCode(t, err.Error()); code != tooladmission.CodeOutputLimitExceeded {
		t.Fatalf("error text = %q, want the output_limit_exceeded denial", err.Error())
	}
	if result != err.Error() {
		t.Fatalf("result %q and error %q disagree", result, err.Error())
	}

	var sawRedaction, sawLimit bool
	for _, event := range h.auditEvents() {
		switch {
		case event.Event == tooladmission.EventRedaction:
			sawRedaction = true
		case event.Event == tooladmission.EventAdmission && event.ReasonCode == tooladmission.CodeOutputLimitExceeded:
			sawLimit = true
		}
	}
	if !sawRedaction || !sawLimit {
		t.Fatal("audit lacks the error-path redaction or the output-limit outcome")
	}
}
