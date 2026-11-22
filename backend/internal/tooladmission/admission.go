// Package tooladmission is the host's invocation boundary: every tool call
// — builtin or MCP — passes authorize → credential scan → capacity wait →
// re-authorize → audit → dispatch → scrub → bound → audit outcome here.
// A call denied cheaply never occupies a slot; a call that waited past a
// permission change is denied before dispatch.
//
// The gateway cannot own this: agent identity, the catalog, the vault and
// the audit store live in the backend, and the gateway may import only the
// backend's public surface. The turn engine keeps its own turn-scoped
// attempt budget and a defense-in-depth scrub.
package tooladmission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/security"
)

// Policy codes carried in a structured denial outcome. They are static:
// never interpolated with arguments, results or remote text.
const (
	CodePolicyDenied        = "policy_denied"
	CodeCredentialUseDenied = "credential_use_denied"
	CodeAuditUnavailable    = "audit_unavailable"
	CodeDeadlineExceeded    = "deadline_exceeded"
	CodeOutputLimitExceeded = "output_limit_exceeded"
)

// Delivered-output ceiling: the sanitized outcome, across all
// content blocks, with the truncation marker counted inside the budget.
const (
	MaxDeliveredBytes = 64 << 10
	truncationMarker  = "…[truncated by output limit]"
	// MaxCallTimeout is the administrator ceiling.
	MaxCallTimeout = 10 * time.Minute
)

// Limits is the admission capacity configuration. These are
// operator-set starting defaults, never model- or server-influenced.
type Limits struct {
	ConcurrencyPerServer int
	ConcurrencyGlobal    int
}

// DefaultLimits is the approved starting policy.
func DefaultLimits() Limits {
	return Limits{
		ConcurrencyPerServer: 4,
		ConcurrencyGlobal:    16,
	}
}

// DefaultCallTimeout bounds one invocation when the server sets none.
const DefaultCallTimeout = 30 * time.Second

// Options is what one wrapped invocation knows about its tool.
type Options struct {
	AgentID      string
	ServerID     string // "" for built-ins
	CatalogID    string // "{slug}:{remote}" or the built-in id
	KnownSecrets []string
	Timeout      time.Duration
}

// Scrubber is the secret scrubber re-exported for backend's public
// surface: the gateway applies the same pattern battery at its fan-out.
type Scrubber = security.Scrubber

// NewPatternScrubber returns the pattern-only scrubber (no known values).
func NewPatternScrubber() *Scrubber { return security.NewPatternScrubber() }

// Admitter wraps invokables with the admission chain. One instance is
// shared by every turn: the budgets are process-local by design.
type Admitter struct {
	limits   Limits
	queries  *db.Queries
	registry *CancelRegistry
	logger   zerolog.Logger

	mu          sync.Mutex
	pending     map[int64]pendingOutcome
	serverSlots map[string]chan struct{}
	globalSlot  chan struct{}

	inserts atomic.Uint64
}

// New builds the admitter over the query layer.
func New(queries *db.Queries, limits Limits, registry *CancelRegistry, logger zerolog.Logger) *Admitter {
	if registry == nil {
		registry = NewCancelRegistry()
	}
	admitter := &Admitter{
		limits:      limits,
		queries:     queries,
		registry:    registry,
		logger:      logger,
		pending:     make(map[int64]pendingOutcome),
		serverSlots: make(map[string]chan struct{}),
		globalSlot:  make(chan struct{}, limits.ConcurrencyGlobal),
	}
	admitter.closeOrphans()
	return admitter
}

// closeOrphans reconciles the trail after a restart: an 'admitted'
// row has an unknown remote outcome, so it is closed as unknown rather
// than replayed.
func (a *Admitter) closeOrphans() {
	if a.queries == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	closed, err := a.queries.CloseOrphanAdmissions(ctx)
	if err != nil {
		a.logger.Error().Err(err).Msg("tool audit restart reconciliation failed")
		return
	}
	if closed > 0 {
		a.logger.Warn().Int64("rows", closed).Msg("tool audit closed orphaned admissions")
	}
}

// Registry exposes the cancellation registry (the API's revocation
// triggers call it).
func (a *Admitter) Registry() *CancelRegistry { return a.registry }

// Wrap rides one invokable on the admission chain.
func (a *Admitter) Wrap(inner tool.InvokableTool, info *schema.ToolInfo, opts Options) tool.InvokableTool {
	return &admittingTool{admitter: a, inner: inner, info: info, opts: opts}
}

// admittingTool is one tool call under the admission chain.
type admittingTool struct {
	admitter *Admitter
	inner    tool.InvokableTool
	info     *schema.ToolInfo
	opts     Options
}

// Info passes the turn's snapshot through unchanged.
func (t *admittingTool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }

// InvokableRun runs the ordered chain. Authorization, budget and audit
// failures become structured denial outcomes — a normal tool result, so
// the turn survives: a safe policy denial is an outcome, not an
// escalation prompt. A failed dispatch stays a Go error: the turn fails
// at message level, as before.
func (t *admittingTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	a := t.admitter
	timeout := t.opts.Timeout
	if timeout <= 0 || timeout > MaxCallTimeout {
		timeout = DefaultCallTimeout
	}
	// The budget starts before authorization and credential
	// lookup, and no stage may restart it.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	callID := callIDFrom(ctx)

	// Registration is linearized with revocation. A withdrawal that
	// lands after authorization must still reach this call, so the call is
	// registered before the policy read and unregistered on every exit.
	unregister := a.registry.Register(t.opts.AgentID, t.opts.ServerID, t.opts.CatalogID, cancel)
	defer unregister()

	// Authorization before capacity: a denied call never queues behind
	// admitted work, and denials stay fast even with a full budget.
	if reason := a.authorize(ctx, t.opts); reason != "" {
		a.record(ctx, t.opts, callID, "denial", reason, nil)
		return denialOutcome(reason), nil
	}

	// Caller-supplied data is checked before any authorized
	// credential injection. Arguments are never rewritten.
	scrubber := security.NewScrubber(t.opts.KnownSecrets)
	if len(argumentFindings(argumentsInJSON, scrubber)) > 0 {
		a.record(ctx, t.opts, callID, "denial", CodeCredentialUseDenied, nil)
		return denialOutcome(CodeCredentialUseDenied), nil
	}

	// Capacity is awaited, not rejected: excess calls wait for a slot and
	// are admitted as one frees up, bounded by this call's own deadline.
	lease, reason, err := a.acquire(ctx, t.opts.ServerID)
	if err != nil {
		// A local cancel is not a policy decision: nobody denied it.
		return "", errors.New("canceled locally while waiting for capacity; remote never called")
	}
	if reason != "" {
		a.recordDetached(t.opts, callID, "denial", reason)
		return denialOutcome(reason), nil
	}
	defer a.release(lease)

	// The wait may have straddled a permission change, so authority is
	// read again once capacity is held: the check that counts is the one
	// immediately before dispatch.
	if reason := a.authorize(ctx, t.opts); reason != "" {
		a.record(ctx, t.opts, callID, "denial", reason, nil)
		return denialOutcome(reason), nil
	}

	// While a previous outcome write is unrecorded, new
	// admissions stay blocked until the trail recovers.
	if err := a.recoverAudit(ctx); err != nil {
		a.logger.Error().Err(err).Str("tool", t.opts.CatalogID).Msg("tool audit still degraded")
		return denialOutcome(CodeAuditUnavailable), nil
	}

	// Durable admission before any external dispatch.
	auditID, err := a.insertAdmission(ctx, t.opts, callID)
	if err != nil {
		a.logger.Error().Err(err).Str("tool", t.opts.CatalogID).Msg("tool admission audit unavailable")
		return denialOutcome(CodeAuditUnavailable), nil
	}

	result, dispatchErr := t.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	if dispatchErr != nil {
		// Cancellation and expiry are host decisions with a known shape;
		// the remote outcome is unknown, and nothing is retried.
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			a.closeAudit(auditID, "error", CodeDeadlineExceeded, 0)
			return denialOutcome(CodeDeadlineExceeded), nil
		case errors.Is(ctx.Err(), context.Canceled):
			a.closeAudit(auditID, "error", "canceled", 0)
			return "", errors.New("canceled locally; remote outcome unknown")
		}
		// The error path is bounded too — a hostile remote cannot
		// push an unbounded body through the failure channel — and its
		// text is scrubbed structure-preservingly, like a result.
		scrubbed, errFindings := scrubResult(scrubber, dispatchErr.Error())
		if len(errFindings) > 0 {
			a.record(ctx, t.opts, callID, "redaction", "redacted", nil)
		}
		bounded, overflowed := boundOutcome(scrubbed)
		if overflowed {
			a.closeAudit(auditID, "error", CodeOutputLimitExceeded, 0)
			out := denialOutcome(CodeOutputLimitExceeded)
			return out, errors.New(out)
		}
		if len(bounded) != len(scrubbed) {
			a.closeAudit(auditID, "error", CodeOutputLimitExceeded, 0)
			return "", errors.New(bounded)
		}
		a.closeAudit(auditID, "error", "failed", 0)
		return "", errors.New(scrubbed)
	}

	// Sanitize first, then bound, so clipping never splits a
	// value before whole-secret matching.
	sanitized, findings := scrubResult(scrubber, result)
	bounded, overflowed := boundOutcome(sanitized)
	if overflowed {
		a.closeAudit(auditID, "error", CodeOutputLimitExceeded, 0)
		if len(findings) > 0 {
			a.record(ctx, t.opts, callID, "redaction", "redacted", nil)
		}
		return denialOutcome(CodeOutputLimitExceeded), nil
	}
	if len(findings) > 0 {
		a.record(ctx, t.opts, callID, "redaction", "redacted", nil)
	}
	a.closeAudit(auditID, "complete", "ok", int64(len(bounded)))
	return bounded, nil
}

// argumentFindings reports the redactions found inside a tool-call
// argument payload. Escaped spellings are inspected in their
// decoded form — `"\u0063anary"` is the value it decodes to — and object
// keys are inspected too. The payload itself is never rewritten.
func argumentFindings(args string, s *security.Scrubber) []security.Finding {
	if !json.Valid([]byte(args)) {
		_, findings := s.ScrubString(args)
		return findings
	}
	dec := json.NewDecoder(strings.NewReader(args))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		_, findings := s.ScrubString(args)
		return findings
	}
	return s.ScrubFindingsIn(tree)
}

// scrubResult sanitizes one dispatch result: structured output is
// scrubbed per string, so numeric tokens survive and the envelope stays
// valid JSON; anything else takes the flat-text path.
func scrubResult(s *security.Scrubber, in string) (string, []security.Finding) {
	if out, findings, ok := s.ScrubJSON(in); ok {
		return out, findings
	}
	return s.ScrubString(in)
}

// callIDKey is the context key carrying the model's tool-call id.
type callIDKey struct{}

// WithCallID stamps the model's tool-call id onto an invocation context,
// so audit rows pair an admission with its outcome without storing any
// payload.
func WithCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callIDKey{}, id)
}

// callIDFrom reads the stamped call id; an unknown id is recorded as
// "unknown", never minted.
func callIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(callIDKey{}).(string); ok && id != "" {
		return id
	}
	return "unknown"
}

// authorize folds the per-invocation policy check: the current global
// switches, the owning server's switch and catalog state, the agent's
// parent gate and its explicit opt-out. An unreadable policy fails closed.
func (a *Admitter) authorize(ctx context.Context, opts Options) string {
	row, err := a.queries.AuthorizeToolCall(ctx, db.AuthorizeToolCallParams{
		AgentID: opts.AgentID, AgentID_2: opts.AgentID, ID: opts.CatalogID,
	})
	if err != nil {
		a.logger.Error().Err(err).Str("tool", opts.CatalogID).Msg("tool authorization unreadable")
		return CodePolicyDenied
	}
	switch {
	case row.ToolEnabled != 1:
		return CodePolicyDenied
	case row.ServerID != nil && row.ServerEnabled != 1:
		return CodePolicyDenied
	case row.ServerID != nil && row.CatalogState != "active":
		return CodePolicyDenied
	case row.GateOn != 1:
		return CodePolicyDenied
	case row.LocallyDisabled:
		return CodePolicyDenied
	}
	return ""
}

// lease is one call's held capacity: the semaphore slots it occupies.
type lease struct {
	serverSlot chan struct{}
	global     bool
}

// acquire waits for this call's capacity: a per-server slot first (MCP
// calls only — built-ins carry no server), then a global one. Waiting is
// bounded by the invocation context, so a full budget delays a call until
// its own deadline instead of failing it at once.
func (a *Admitter) acquire(ctx context.Context, serverID string) (lease, string, error) {
	l := lease{}
	if serverID != "" {
		slot := a.serverSlot(serverID)
		select {
		case slot <- struct{}{}:
			l.serverSlot = slot
		case <-ctx.Done():
			return a.waitStopped(ctx, l)
		}
	}
	select {
	case a.globalSlot <- struct{}{}:
		l.global = true
	case <-ctx.Done():
		return a.waitStopped(ctx, l)
	}
	return l, "", nil
}

// waitStopped classifies a stopped capacity wait and gives back the
// server slot it may already hold: an expired deadline is the deadline
// denial; a local cancel is an error, not a policy decision.
func (a *Admitter) waitStopped(ctx context.Context, l lease) (lease, string, error) {
	if l.serverSlot != nil {
		<-l.serverSlot
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return lease{}, CodeDeadlineExceeded, nil
	}
	return lease{}, "", ctx.Err()
}

// release frees the capacity a call held. The receives are unconditional:
// the lease holds exactly the tokens it took, so a blocking receive
// surfaces an accounting bug instead of silently leaking capacity.
func (a *Admitter) release(l lease) {
	if l.global {
		<-a.globalSlot
	}
	if l.serverSlot != nil {
		<-l.serverSlot
	}
}

// serverSlot returns (creating on first use) one server's semaphore.
func (a *Admitter) serverSlot(serverID string) chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	slot, ok := a.serverSlots[serverID]
	if !ok {
		slot = make(chan struct{}, a.limits.ConcurrencyPerServer)
		a.serverSlots[serverID] = slot
	}
	return slot
}

// denial is the structured policy outcome the model sees.
type denial struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// denialOutcome renders one structured denial as a normal tool result.
func denialOutcome(code string) string {
	var out denial
	out.Error.Code = code
	encoded, err := json.Marshal(out)
	if err != nil {
		// A marshal failure of a static struct cannot carry payload; the
		// code alone is the honest answer.
		return fmt.Sprintf(`{"error":{"code":%q}}`, code)
	}
	return string(encoded)
}

// boundOutcome enforces the delivered-output ceiling. Structured
// output is never byte-sliced into malformed JSON: it is reported as an
// overflow instead, and the caller returns a safe outcome.
func boundOutcome(s string) (bounded string, overflowed bool) {
	if len(s) <= MaxDeliveredBytes {
		return s, false
	}
	if json.Valid([]byte(s)) {
		return "", true
	}
	keep := MaxDeliveredBytes - len(truncationMarker)
	for keep > 0 && !isRuneStart(s[keep]) {
		keep--
	}
	return s[:keep] + truncationMarker, false
}

// isRuneStart reports whether b begins a UTF-8 rune.
func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// ResolveTimeout maps a configured deadline to the effective one: the
// default when unset, clamped to the administrator ceiling.
func ResolveTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultCallTimeout
	}
	if d > MaxCallTimeout {
		return MaxCallTimeout
	}
	return d
}
