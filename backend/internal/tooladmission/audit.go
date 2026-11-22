package tooladmission

// Durable, payload-free audit: admission before dispatch, outcome
// after, and point-in-time decisions for approvals, plaintext exceptions,
// denials and redactions. Every column is a validated identifier, a static
// code or a bounded count — the schema has no payload column at all.

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/security"
)

// Audit events.
const (
	EventAdmission      = "admission"
	EventApprovalChange = "approval_change"
	EventPlaintext      = "plaintext_exception"
	EventDenial         = "denial"
	EventRedaction      = "redaction"
	stateAdmitted       = "admitted"
	stateTerminal       = "terminal"
	auditRetention      = 30 * 24 * time.Hour
	auditMaxRecords     = 100_000
	auditPruneEvery     = 64
)

// insertAdmission writes the pre-dispatch record and returns its id. A
// failure here denies the call: no execution without a durable record.
func (a *Admitter) insertAdmission(ctx context.Context, opts Options, callID string) (int64, error) {
	id, err := a.queries.InsertAuditEvent(ctx, db.InsertAuditEventParams{
		OccurredAt: time.Now().UnixMicro(),
		Event:      EventAdmission,
		AgentID:    nullString(opts.AgentID),
		ServerID:   nullString(opts.ServerID),
		ToolID:     nullString(opts.CatalogID),
		ToolCallID: nullString(auditCallID(callID, security.NewScrubber(opts.KnownSecrets))),
		Decision:   "admit",
		ReasonCode: "ok",
		State:      stateAdmitted,
	})
	if err != nil {
		return 0, err
	}
	a.noteInsert(ctx)
	return id, nil
}

// noteInsert advances the admission retention cadence: every 64th
// write sweeps. The sweep runs on a detached, bounded context so it never
// eats the invocation's own deadline.
func (a *Admitter) noteInsert(ctx context.Context) {
	if a.queries == nil {
		return
	}
	if n := a.inserts.Add(1); n%auditPruneEvery == 0 {
		pruneCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		a.prune(pruneCtx)
	}
}

// auditCallID bounds and validates a model-supplied call id: the id
// is a correlation handle, never trusted text. Anything longer than 64
// characters, outside [A-Za-z0-9_.-], or carrying a known secret value is
// replaced by a deterministic host-derived handle, so a correlation is
// observable but a payload is not storable.
func auditCallID(id string, scrubber *security.Scrubber) string {
	if id == "" {
		return "unknown"
	}
	if len(id) <= 64 {
		valid := true
		for i := range len(id) {
			c := id[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
				c == '_', c == '.', c == '-':
			default:
				valid = false
			}
			if !valid {
				break
			}
		}
		if valid && len(scrubber.ScrubFindingsIn(id)) == 0 {
			return id
		}
	}
	sum := sha256.Sum256([]byte(id))
	return "h:" + hex.EncodeToString(sum[:])[:31]
}

// pendingOutcome is an outcome write that failed after dispatch: the
// observed result is preserved and the record is retried on the next
// admission, which is blocked until the write succeeds.
// One entry per outstanding call, so concurrent failures are all kept —
// the map is bounded by ConcurrencyGlobal.
type pendingOutcome struct {
	id       int64
	decision string
	reason   string
	count    sql.NullInt64
}

// closeAudit records the observed outcome. A failure here degrades the
// audit trail but must not rewrite what happened: the caller keeps the
// truthful result, and the pending write blocks further admissions.
func (a *Admitter) closeAudit(id int64, decision, reason string, count int64) {
	if id == 0 {
		return
	}
	var bounded sql.NullInt64
	if decision == "complete" {
		bounded = sql.NullInt64{Int64: count, Valid: true}
	}
	// The outcome write must survive the invocation's cancel: it is part
	// of the record, not of the call.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), 5*time.Second)
	defer cancel()
	if _, err := a.queries.UpdateAuditOutcome(ctx, db.UpdateAuditOutcomeParams{
		Decision: decision, ReasonCode: reason, Count: bounded, ID: id,
	}); err != nil {
		a.logger.Error().Err(err).Int64("audit_id", id).Msg("tool outcome audit failed")
		a.mu.Lock()
		a.pending[id] = pendingOutcome{id: id, decision: decision, reason: reason, count: bounded}
		a.mu.Unlock()
	}
}

// recoverAudit retries the unrecorded outcome. Failure keeps admissions
// blocked (audit_unavailable) — no new privilege is exercised while the
// trail is incomplete.
func (a *Admitter) recoverAudit(ctx context.Context) error {
	a.mu.Lock()
	if len(a.pending) == 0 {
		a.mu.Unlock()
		return nil
	}
	pending := make([]pendingOutcome, 0, len(a.pending))
	for _, p := range a.pending {
		pending = append(pending, p)
	}
	a.mu.Unlock()
	slices.SortFunc(pending, func(x, y pendingOutcome) int { return cmp.Compare(x.id, y.id) })

	for _, p := range pending {
		if _, err := a.queries.UpdateAuditOutcome(ctx, db.UpdateAuditOutcomeParams{
			Decision: p.decision, ReasonCode: p.reason,
			Count: p.count, ID: p.id,
		}); err != nil {
			// The remainder stays pending, so admissions stay blocked: the
			// trail lags, it never lies.
			return err
		}
		a.mu.Lock()
		delete(a.pending, p.id)
		a.mu.Unlock()
	}
	return nil
}

// record writes one point-in-time event (denial, redaction) with the same
// payload-free shape.
func (a *Admitter) record(ctx context.Context, opts Options, callID, event, reason string, count *int64) {
	var bounded sql.NullInt64
	if count != nil {
		bounded = sql.NullInt64{Int64: *count, Valid: true}
	}
	if _, err := a.queries.InsertAuditEvent(ctx, db.InsertAuditEventParams{
		OccurredAt: time.Now().UnixMicro(),
		Event:      event,
		AgentID:    nullString(opts.AgentID),
		ServerID:   nullString(opts.ServerID),
		ToolID:     nullString(opts.CatalogID),
		ToolCallID: nullString(auditCallID(callID, security.NewScrubber(opts.KnownSecrets))),
		Decision:   "deny",
		ReasonCode: reason,
		Count:      bounded,
		State:      stateTerminal,
	}); err != nil {
		a.logger.Error().Err(err).Str("event", event).Msg("tool audit event failed")
	}
	a.noteInsert(ctx)
}

// recordDetached writes a point-in-time event whose invocation context is
// already spent — a capacity wait that hit its deadline. The record is
// part of the trail, not of the call, so it gets its own bounded context.
func (a *Admitter) recordDetached(opts Options, callID, event, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.record(ctx, opts, callID, event, reason, nil)
}

// prune applies the retention bounds: age first, then the record
// cap. Admitted rows awaiting their outcome are never deleted.
func (a *Admitter) prune(ctx context.Context) {
	pruneAudit(ctx, a.queries, a.logger)
}

// pruneAudit is the shared retention sweep: age expiry first, then the
// cap on completed records.
func pruneAudit(ctx context.Context, queries *db.Queries, logger zerolog.Logger) {
	if queries == nil {
		return
	}
	cutoff := time.Now().Add(-auditRetention).UnixMicro()
	if _, err := queries.DeleteAuditOlderThan(ctx, cutoff); err != nil {
		logger.Error().Err(err).Msg("tool audit age prune failed")
	}
	if _, err := queries.DeleteAuditOverflow(ctx, int64(auditMaxRecords)); err != nil {
		logger.Error().Err(err).Msg("tool audit overflow prune failed")
	}
}

// StartPrune runs the retention sweep on a ticker, so the 30-day bound
// expires without fresh admissions. The returned stop is idempotent
// and waits for no in-flight sweep.
func (a *Admitter) StartPrune(interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = time.Hour
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sweepCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				a.prune(sweepCtx)
				cancel()
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// Auditor records the point-in-time security decisions the API makes:
// approval changes, revocations and plaintext exceptions. It shares the
// admission records' shape and durability.
type Auditor struct {
	queries *db.Queries
	logger  zerolog.Logger
}

// NewAuditor builds the decision recorder over the query layer.
func NewAuditor(queries *db.Queries) *Auditor {
	return &Auditor{queries: queries, logger: zerolog.Nop()}
}

// Decision is one point-in-time record: identifiers, a static reason code
// and, when meaningful, a bounded count. No payload fields exist to pass.
type Decision struct {
	Event    string
	AgentID  string
	ServerID string
	ToolID   string
	Outcome  string
	Reason   string
	Count    int64
	HasCount bool
}

// Record writes one decision. A failure is returned for the caller to
// answer: a privilege increase that cannot be audited must not be reported
// as committed.
func (a *Auditor) Record(ctx context.Context, decision Decision) error {
	var count sql.NullInt64
	if decision.HasCount {
		count = sql.NullInt64{Int64: decision.Count, Valid: true}
	}
	_, err := a.queries.InsertAuditEvent(ctx, db.InsertAuditEventParams{
		OccurredAt: time.Now().UnixMicro(),
		Event:      decision.Event,
		AgentID:    nullString(decision.AgentID),
		ServerID:   nullString(decision.ServerID),
		ToolID:     nullString(decision.ToolID),
		Decision:   decision.Outcome,
		ReasonCode: decision.Reason,
		Count:      count,
		State:      stateTerminal,
	})
	if err != nil {
		return err
	}
	// Approval and exception traffic drives retention too: these
	// writes are rare operator actions, so every one sweeps. The sweep
	// is detached and bounded — it cannot eat the caller's request
	// deadline.
	pruneCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	pruneAudit(pruneCtx, a.queries, a.logger)
	return nil
}

// nullString maps an empty identifier to a NULL column.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
