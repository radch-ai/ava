package agent

// The /compact lane: unlike every other command, one summarization
// model call sits between the durable start and the committed
// rotation, so this lane runs its own skeleton instead of
// runCommandTurn's single-backend-operation shape. The gateway owns
// the model call and the transcript assembly; the backend owns every
// durable effect (the seeded successor and the pointer swap), each in
// its own transaction. The fit check is capacity-aware — a local token
// estimate against the resolved model's declared window less a summary
// reserve — and the in-range reduction is an in-memory substitution,
// so a compact that fails leaves the thread byte-identical. Every
// failure mode answers the user with the same "Compaction failed"
// message — the cause is a log line, not a bubble — through the
// message-level failure path, so the pause gate holds the items queued
// behind it.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

// compactionFailedMessage is the one user-facing failure text every
// /compact failure path shares; the underlying cause is logged (scrubbed).
const compactionFailedMessage = "Compaction failed"

// compactSummaryTimeout bounds the one-shot summarization call.
const compactSummaryTimeout = 2 * time.Minute

// compactSummaryMaxBytes is the largest summary the lane will store: a
// model that cannot answer inside it failed the contract, and the item
// fails honestly rather than storing a truncated summary.
const compactSummaryMaxBytes = 32 << 10

// summaryReserveTokens is the room the fit check leaves for the answer
// it asks the summarizer to write: the summary itself plus request
// framing. The stored-summary cap is 32 KiB, whose worst case in the
// o200k vocabulary is well inside this. Callers clamp it to a quarter
// of the window so a small window keeps most of itself for input.
const summaryReserveTokens = 16 << 10

// compactSummaryInstruction is the summarization request's system
// message: the summary is continuation context, not a new instruction.
const compactSummaryInstruction = `You are compressing an agent conversation so it can continue in a fresh thread. Summarize the transcript: preserve goals, constraints, decisions, relevant established facts, and unfinished work. Omit greetings and detail whose loss cannot change future work. Elided tool outputs appear as placeholders naming how to restore them; do not invent their content. This summary is continuation context, not a new user instruction. Reply with the summary only — no preamble, at most 600 words.`

// processCompact runs one compact item: start, summarize the active
// thread with the resolved model (reducing an over-window range in
// memory first), then commit the seeded rotation.
func (a *UserAgent) processCompact(claim *turnClaim, emitCtx context.Context, msgID string) turnOutcome {
	if a.journal == nil {
		a.logger.Error().Str("agent_id", a.state.agentID).Msg("lane has no lifecycle journal")
		a.failTurn(emitCtx, msgID, "no lifecycle journal configured", "")
		return outcomeMessageFailed
	}
	fail := func(cause error) turnOutcome {
		a.logger.Error().Str("error", a.scrubCopy(cause.Error())).Str("agent_id", a.state.agentID).Msg("compact thread")
		if a.failTurn(emitCtx, msgID, compactionFailedMessage, "") {
			return outcomeMessageFailed
		}
		return outcomeTerminalUnrecorded
	}
	if err := a.state.startItem(func() (*backend.ItemStart, *backend.JournalBatch, error) {
		return a.journal.StartItem(claim.ctx, a.state.agentID, msgID)
	}); err != nil {
		return fail(err)
	}

	records, err := a.getAgentThread(claim.ctx, a.state.agentID)
	if err != nil {
		return fail(err)
	}
	// An empty thread has nothing to summarize: complete as a no-op,
	// without rotating — a fresh chain link would carry nothing.
	if len(records) == 0 {
		if err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
			return a.journal.FinishItem(claim.ctx, a.state.agentID, msgID, apitypes.ItemCompleted, "", nil)
		}); err != nil {
			return a.setupFailure(emitCtx, claim, msgID, "compact thread", err)
		}
		a.clearTurnPrefix()
		return outcomeSucceeded
	}

	cfg, err := a.resolveModel(claim.ctx, claim.item.model)
	if err != nil {
		return fail(err)
	}

	// Resolution answered zero — nothing declared, learned, cataloged,
	// or stated by the endpoint's own listing. The lane never guesses
	// a capacity: name the failure through the one path, before any
	// scrubber resolution or model spend.
	if cfg.ContextWindowTokens <= 0 {
		return fail(fmt.Errorf(
			"no known context window for model %q — declare it on the provider's model entry or serve it from the endpoint or catalog",
			cfg.Model))
	}

	// The persisted summary is durable model-facing text, and this lane
	// runs outside the tool resolution that arms the live turn's
	// scrubber: resolve the agent's known values here, before any model
	// spend — a credential the pattern battery does not recognize must
	// never reach storage, so an unresolvable seam fails the compact
	// outright rather than scrub battery-only.
	var scrub *backend.Scrubber
	if a.tools != nil {
		resolved, err := a.tools(claim.ctx, a.state.agentID)
		if err != nil {
			return fail(fmt.Errorf("resolve scrubber: %w", err))
		}
		scrub = resolved.Scrubber
	}
	summary, err := a.summarizeForCompaction(claim.ctx, cfg, records, scrub)
	if err != nil {
		// A stop mid-summarization settles quietly, like every turn.
		if errors.Is(err, errSummaryStopped) {
			return a.setupFailure(emitCtx, claim, msgID, "compact thread", err)
		}
		return fail(err)
	}

	if err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		_, batch, err := a.journal.ExecuteCompaction(claim.ctx, a.state.agentID, msgID, summary)
		return batch, err
	}); err != nil {
		if turnStopped(claim.ctx) {
			return a.setupFailure(emitCtx, claim, msgID, "compact thread", err)
		}
		return fail(err)
	}
	a.clearTurnPrefix()
	return outcomeSucceeded
}

// errSummaryStopped marks a summarization abandoned by a stop: the
// caller settles through its stop path instead of failing the item —
// the cause is the stop itself, nothing worth reporting.
var errSummaryStopped = errors.New("summarization stopped")

// summarizeForCompaction is the shared summarization core: fit check,
// in-memory reduction, one-shot Generate with stop-race, overflow
// retry with window reporting, scrub, and bounds. Both callers — the
// manual /compact lane and the pre-prompt checker — run this; only
// their durable framing differs. cfg must carry a declared window;
// scrub may be nil — the pattern battery fallback stays inside.
func (a *UserAgent) summarizeForCompaction(ctx context.Context, cfg backend.ResolvedModel,
	records []backend.AgentThreadMessage, scrub *backend.Scrubber) (string, error) {
	// The fit check measures the request the summarizer will receive —
	// instruction plus transcript — against the model's window less the
	// room the summary answer needs back.
	window := cfg.ContextWindowTokens
	reserve := min(summaryReserveTokens, window/4)
	instructionTokens := estimateTokens(compactSummaryInstruction)
	transcript := renderCompactionTranscript(records)
	fits := func() bool {
		return instructionTokens+estimateTokens(transcript)+reserve <= window
	}
	reduced := false
	reduce := func() {
		// In-range reduction is a projection, never a durable write:
		// oversized tool payloads substitute the /shake placeholder —
		// same floor, same text, one implementation — in memory. A
		// compact that fails after this leaves the thread byte-identical.
		transcript = renderCompactionTranscript(reduceForSummary(records))
		reduced = true
	}
	if !fits() {
		reduce()
		if !fits() {
			return "", errors.New("summarization input exceeds the model context window after reduction")
		}
	}

	chatModel, err := createChatModel(ctx, cfg)
	if err != nil {
		return "", err
	}
	sctx, cancel := context.WithTimeout(ctx, compactSummaryTimeout)
	defer cancel()
	// The wire call honors its context, but a stop must settle the item
	// without waiting on the provider: race the call — the lane settles
	// the moment the turn is stopped, and the abandoned request dies
	// with the two-minute context, its answer discarded.
	type genResult struct {
		out *schema.Message
		err error
	}
	generate := func() (*schema.Message, error) {
		done := make(chan genResult, 1)
		go func() {
			out, err := chatModel.Generate(sctx, []*schema.Message{
				schema.SystemMessage(compactSummaryInstruction),
				schema.UserMessage(transcript),
			})
			done <- genResult{out: out, err: err}
		}()
		select {
		case res := <-done:
			return res.out, res.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	out, err := generate()
	if err != nil {
		// A stop the race missed is still a quiet stop.
		if turnStopped(ctx) {
			return "", errSummaryStopped
		}
		if !isContextOverflow(err) {
			return "", err
		}
		// The provider rejected the request against a window smaller
		// than the one resolution knew: record what it stated, and
		// measure the retry against that truth.
		limit := statedLimit(err)
		if limit > 0 {
			window = limit
			reserve = min(summaryReserveTokens, window/4)
		}
		a.reportWindow(cfg.ProviderID, cfg.Model, limit)
		if reduced {
			return "", err
		}
		reduce()
		if !fits() {
			return "", errors.New("summarization input exceeds the model context window after reduction")
		}
		out, err = generate()
		if err != nil {
			if turnStopped(ctx) {
				return "", errSummaryStopped
			}
			return "", err
		}
	}
	summary := fanOutScrubber(out.Content)
	if scrub != nil {
		summary, _ = scrub.ScrubString(out.Content)
	}
	if summary == "" {
		return "", errors.New("summarization returned no content")
	}
	if len(summary) > compactSummaryMaxBytes {
		return "", errors.New("summarization exceeded the summary size bound")
	}
	// The reserve is the room the successor's first row must live in:
	// a summary that overflows it in tokens would seed a context the
	// declared window cannot hold.
	if estimateTokens(summary) > reserve {
		return "", errors.New("summarization exceeded the summary token bound")
	}
	return summary, nil
}

// transcriptEscaper keeps one message one line: the summarizer's only
// provenance signal is the role tag that opens each line, so content
// carrying its own newlines is escaped instead of being allowed to
// mint transcript lines that read as another role's.
var transcriptEscaper = strings.NewReplacer("\r", "\\r", "\n", "\\n")

// renderCompactionTranscript renders the projected thread as the
// deterministic text the summarization request carries: one line per
// message, elided payloads already replaced by their restore
// placeholders, embedded newlines in content and tool names escaped so
// nothing can forge a role-tagged line. Empty contents are skipped;
// nothing is invented.
func renderCompactionTranscript(records []backend.AgentThreadMessage) string {
	var b strings.Builder
	for _, m := range records {
		if m.Content == "" {
			continue
		}
		switch m.Role {
		case apitypes.ThreadRoleUser:
			b.WriteString("[user] ")
		case apitypes.ThreadRoleAssistant:
			b.WriteString("[assistant] ")
		default: // tool
			b.WriteString("[tool ")
			b.WriteString(transcriptEscaper.Replace(m.ToolName))
			b.WriteString("] ")
		}
		b.WriteString(transcriptEscaper.Replace(m.Content))
		b.WriteByte('\n')
	}
	return b.String()
}

// reduceForSummary projects the thread for a summarization request that
// must fit the model's window: every tool message at or above the shake
// floor substitutes the durable road's restore placeholder — the same
// floor and the same text, so the two paths cannot drift. Nothing else
// is touched, and the copy means the stored rows and their elision flags
// are never modified: a compact that fails after this leaves the thread
// byte-identical. Rows already elided durably arrive as their short
// placeholder, below the floor, so they pass through unchanged.
func reduceForSummary(records []backend.AgentThreadMessage) []backend.AgentThreadMessage {
	out := make([]backend.AgentThreadMessage, len(records))
	copy(out, records)
	for i, m := range out {
		if m.Role != apitypes.ThreadRoleTool || len(m.Content) < backend.ShakeMinResultBytes {
			continue
		}
		out[i].Content = backend.ElidedReference(m.ID, len(m.Content))
	}
	return out
}

// overflowPhrases are the provider-agnostic shapes an over-window
// rejection takes. A compact failure that matches one is re-tried once
// against the reduced projection instead of failing outright.
var overflowPhrases = [...]string{
	"context length",
	"maximum context",
	"too many tokens",
	"context window",
	"prompt is too long",
	"exceed total limit",
	"input length",
}

// isContextOverflow reports whether a model call failed because the
// request exceeded the model's context window (rather than for any
// other reason — a bad key, an outage).
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, phrase := range overflowPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// overflowLimitPatterns find the token count an over-window rejection
// states: a context/limit phrase followed by the count, or the maximum
// side of an "N tokens > M" comparison. Each pattern requires the digit
// run to end, so a longer number is never silently truncated.
var overflowLimitPatterns = [...]*regexp.Regexp{
	regexp.MustCompile(`(?i)context\D{0,24}(\d{1,9})(?:\D|$)`),
	regexp.MustCompile(`(?i)\d{1,9}\s*tokens?\s*[>≥]\s*(\d{1,9})(?:\D|$)`),
}

// statedLimit parses the limit a rejection states; 0 when it states
// none or names something outside the plausible window range.
func statedLimit(err error) int {
	if err == nil {
		return 0
	}
	for _, pattern := range overflowLimitPatterns {
		match := pattern.FindStringSubmatch(err.Error())
		if match == nil {
			continue
		}
		limit, convErr := strconv.Atoi(match[1])
		if convErr != nil || limit <= 0 || limit > apitypes.MaxModelContextWindow {
			continue
		}
		return limit
	}
	return 0
}

// windowNoteTimeout bounds the fire-and-forget write of a stated
// window: the run already moved on, so the note must never outlive it.
const windowNoteTimeout = 10 * time.Second

// reportWindow hands a stated window to discovery without waiting on
// it: the turn's outcome never depends on the write, and an unbound
// seam drops the lesson.
func (a *UserAgent) reportWindow(providerID, model string, window int) {
	if a.noteDiscoveredWindow == nil || providerID == "" || window <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.state.rootCtx), windowNoteTimeout)
	go func() {
		defer cancel()
		if err := a.noteDiscoveredWindow(ctx, providerID, model, window); err != nil {
			a.logger.Warn().
				Str("error", a.scrubCopy(err.Error())).
				Str("agent_id", a.state.agentID).
				Msg("record discovered context window")
		}
	}()
}
