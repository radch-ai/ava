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
)

// insertAdmission writes the pre-dispatch record and returns its id. A
// failure here denies the call: no execution without a durable record.
func (a *Admitter) insertAdmission(ctx context.Context, opts Options, callID string, scrub *security.Scrubber) (int64, error) {
	id, err := a.queries.InsertAuditEvent(ctx,
		auditParams(opts.AgentID, opts.ServerID, opts.CatalogID, callID, EventAdmission, "admit", "ok", stateAdmitted, scrub))
	if err != nil {
		return 0, err
	}
	return id, nil
}

// auditCallID bounds and validates a model-supplied call id: the id
// is a correlation handle, never trusted text. Anything longer than 64
// characters, outside [A-Za-z0-9_.-], or carrying a known secret value is
// replaced by a deterministic host-derived handle, so a correlation is
// observable but a payload is not storable. An empty id means no
// invocation exists (an operator decision) and stays NULL — the
// invocation boundary (callIDFrom) is what records "unknown" for a
// missing id during a call.
func auditCallID(id string, scrubber *security.Scrubber) string {
	if id == "" {
		return ""
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

// closeAudit records a failed call's observed outcome: no result count
// exists to record.
func (a *Admitter) closeAudit(id int64, decision, reason string) {
	a.writeOutcome(id, decision, reason, sql.NullInt64{})
}

// closeAuditComplete records a completed call's outcome with its bounded
// result count.
func (a *Admitter) closeAuditComplete(id int64, count int64) {
	a.writeOutcome(id, "complete", "ok", sql.NullInt64{Int64: count, Valid: true})
}

// writeOutcome lands one outcome row. A failure here degrades the audit
// trail but must not rewrite what happened: the caller keeps the truthful
// result, and the pending write blocks further admissions.
func (a *Admitter) writeOutcome(id int64, decision, reason string, bounded sql.NullInt64) {
	if id == 0 {
		return
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

// auditParams assembles the payload-free record shape every audit write
// shares — admission, point-in-time events, and operator decisions:
// bounded identifiers, one static code, a state.
func auditParams(agent, server, tool, callID, event, decision, reason, state string, scrub *security.Scrubber) db.InsertAuditEventParams {
	return db.InsertAuditEventParams{
		OccurredAt: time.Now().UnixMicro(),
		Event:      event,
		AgentID:    nullString(agent),
		ServerID:   nullString(server),
		ToolID:     nullString(tool),
		ToolCallID: nullString(auditCallID(callID, scrub)),
		Decision:   decision,
		ReasonCode: reason,
		State:      state,
	}
}

// record writes one point-in-time event (denial, redaction) with the same
// payload-free shape.
func (a *Admitter) record(ctx context.Context, opts Options, callID, event, reason string, scrub *security.Scrubber) {
	if _, err := a.queries.InsertAuditEvent(ctx,
		auditParams(opts.AgentID, opts.ServerID, opts.CatalogID, callID, event, "deny", reason, stateTerminal, scrub)); err != nil {
		a.logger.Error().Err(err).Str("event", event).Msg("tool audit event failed")
	}
}

// recordDetached writes one point-in-time event through its own bounded
// context, independent of the invocation: capacity waits that hit their
// deadline, and redactions recorded after the result is delivered. The
// record is part of the trail, not of the call.
func (a *Admitter) recordDetached(opts Options, callID, event, reason string, scrub *security.Scrubber) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.record(ctx, opts, callID, event, reason, scrub)
}

// pruneAudit is the retention sweep, run only by the StartPrune
// ticker: age expiry first, then the cap on completed records.
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
				pruneAudit(sweepCtx, a.queries, a.logger)
				cancel()
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
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
}

// RecordDecision writes one point-in-time security decision the API
// makes: approval changes, revocations and plaintext exceptions, in the
// admission records' shape. A failure is returned for the caller to
// answer: a privilege increase that cannot be audited must not be
// reported as committed. Retention runs only on the prune ticker —
// operator writes never pay the sweep inline.
func RecordDecision(ctx context.Context, queries *db.Queries, decision Decision) error {
	_, err := queries.InsertAuditEvent(ctx, auditParams(
		decision.AgentID, decision.ServerID, decision.ToolID, "",
		decision.Event, decision.Outcome, decision.Reason, stateTerminal, nil))
	return err
}

// nullString maps an empty identifier to a NULL column.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
