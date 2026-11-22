package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// streamCoalesceWindow and streamCoalesceBytes bound one answer-chunk
// frame: buffered deltas flush every window at the latest, or sooner
// once they reach the byte cap. A long answer costs a handful of
// frames per second instead of one per token; per-connection FIFO
// keeps the chunks in order.
const (
	streamCoalesceWindow = 40 * time.Millisecond
	streamCoalesceBytes  = 4 << 10
)

// ModelResolver maps a selection to a callable endpoint. An alias of
// backend TurnService.ResolveModel, so the bound method value needs
// no adapter.
type ModelResolver = func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error)

// LifecycleJournal is the durable half of a turn: item bookkeeping, the
// conversation rows each transition commits, and the ordered event feed
// every subscriber receives. *backend.TurnService satisfies it, so the
// binary binds the service directly. The backend owns item, thread, and
// row identities — the gateway never mints them.
type LifecycleJournal interface {
	// AcceptItem records one admitted submission under its stable item id.
	AcceptItem(ctx context.Context, agentID, itemID, content, kind string, model *apitypes.ModelEntry) (*backend.JournalBatch, error)
	// StartItem resolves the execution thread and commits a prompt's
	// user row with the transition.
	StartItem(ctx context.Context, agentID, itemID string) (*backend.ItemStart, *backend.JournalBatch, error)
	// RecordToolStarted records an invocation's intent before it runs.
	RecordToolStarted(ctx context.Context, agentID, itemID, invocationID, callID, name, args string) (*backend.JournalBatch, error)
	// RecordToolFinished commits an invocation's outcome row.
	RecordToolFinished(ctx context.Context, agentID, itemID, invocationID, result string, errText *string) (*backend.JournalBatch, error)
	// AppendItemMessages commits a started item's nonterminal rows.
	AppendItemMessages(ctx context.Context, agentID, itemID string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error)
	// FinishItem settles the item with its one terminal outcome and any
	// final rows.
	FinishItem(ctx context.Context, agentID, itemID, outcome, reason string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error)
	// ExecuteThreadSwitch runs a command's durable thread switch.
	ExecuteThreadSwitch(ctx context.Context, agentID, itemID string) (*backend.ActivatedThread, *backend.JournalBatch, error)
	// ReplayAfter answers one bounded replay page, validating the
	// cursor's history inside the same read.
	ReplayAfter(ctx context.Context, agentID, historyID string, afterPos int64, limit int) (*backend.JournalPage, error)
	// JournalStatus reports the history identity and the agent's
	// committed coordinates.
	JournalStatus(ctx context.Context, agentID string) (historyID string, head, floor int64, err error)
	// SyncSnapshot answers the authoritative baseline.
	SyncSnapshot(ctx context.Context, agentID string, pageLimit int64) (*backend.SyncSnapshot, error)
}

// AgentConfigSource reads an agent's editable config — the prompt
// texts a turn needs. An alias of backend TurnService.GetAgentConfig,
// so the bound method value needs no adapter.
type AgentConfigSource = func(ctx context.Context, agentID string) (*apitypes.AgentConfig, error)

// AgentToolsFunc fetches an agent's effective tools for a turn —
// permissions applied, ready for the capture wrapper. An alias of
// backend TurnService.AgentTools, so the bound method value needs no
// adapter.
type AgentToolsFunc = func(ctx context.Context, agentID string) ([]tool.InvokableTool, error)

// ToolCopyScrubber sanitizes one tool-call copy for persistence and
// emission. An alias of backend TurnService.ScrubToolCopy, so the bound
// method value needs no adapter. A nil scrubber falls back to the shared
// pattern battery.
type ToolCopyScrubber = func(ctx context.Context, agentID, content string) string

// AgentThreadSource reads the agent's active thread as the turn's
// model input. An alias of backend TurnService.GetAgentThread, so the
// bound method value needs no adapter.
type AgentThreadSource = func(ctx context.Context, agentID string) ([]backend.AgentThreadMessage, error)

// turnOutcome classifies a settled turn for the queue: only
// outcomeMessageFailed pauses automatic dispatch.
type turnOutcome int

const (
	outcomeSucceeded turnOutcome = iota
	outcomeStopped
	outcomeToolFailed
	outcomeMessageFailed
	// outcomeTerminalUnrecorded marks a started item whose terminal
	// outcome could not be committed: the lane halts rather than let
	// anything else run ahead of an outcome nobody recorded.
	outcomeTerminalUnrecorded
)

type UserAgentOptions struct {
	State  *agentState
	OnExit func(*UserAgent)
	// ResolveModel maps a selection to a callable endpoint for
	// every lane; bound by the gateway at boot.
	ResolveModel ModelResolver
	// Journal is the durable lifecycle feed every transition commits
	// through; bound by the gateway at boot. An unbound journal fails
	// every turn and command closed.
	Journal LifecycleJournal
	// GetAgentConfig reads the agent's config for each turn's
	// prompt; bound by the gateway at boot.
	GetAgentConfig AgentConfigSource
	// Tools fetches the agent's effective tools for each turn —
	// permissions applied; bound by the gateway at boot. A failed
	// read fails the turn; a user stop during it settles as a stop.
	Tools AgentToolsFunc
	// GetAgentThread reads one thread as the turn's model input;
	// bound by the gateway at boot.
	GetAgentThread AgentThreadSource
	// ScrubToolCopy sanitizes every observable copy of a tool call —
	// arguments, results, error text — with the agent's known secrets
	// plus the pattern battery; bound by the gateway at boot (nil keeps
	// the pattern battery alone).
	ScrubToolCopy ToolCopyScrubber
	Logger        zerolog.Logger
}

// UserAgent is one agent's disposable runtime worker: it executes
// queue items one at a time and owns nothing the queue depends on.
// Subscriptions and pending work live on the manager-owned state. A
// worker exits as soon as no runnable work remains — the transcript
// lives in the backend's thread store, not a live worker.
type UserAgent struct {
	state *agentState
	mu    sync.Mutex

	// working reports a turn in flight; reasoning reports the model
	// streaming reasoning tokens within it. Both guarded by mu.
	working   bool
	reasoning bool

	// turnMsgID, turnPrefix, and recorder describe the live turn: its
	// streamed text and invocation states, the prefix a synchronizing
	// device receives before the chunks still to come. turnSeq is the
	// stream's own increasing sequence, read atomically by the prefix
	// snapshot. turnPrefixGen bumps when a round's rows commit — the
	// committed round rides the journal, so a stream whose generation
	// is behind stops extending the prefix. Guarded by mu except
	// turnSeq.
	turnMsgID     string
	turnPrefix    strings.Builder
	turnPrefixGen uint64
	turnSeq       atomic.Int64
	recorder      *turnRecorder

	onExit         func(*UserAgent)
	logger         zerolog.Logger
	resolveModel   ModelResolver
	getAgentConfig AgentConfigSource
	tools          AgentToolsFunc
	getAgentThread AgentThreadSource
	journal        LifecycleJournal
	// scrubToolCopy sanitizes observable copies; nil keeps the shared
	// pattern battery alone.
	scrubToolCopy ToolCopyScrubber
}

// newUserAgent creates a worker and starts its claim loop. Turn
// contexts are minted from the state's root context, so cancelling the
// root ends any turn still running.
func newUserAgent(opts UserAgentOptions) *UserAgent {
	a := &UserAgent{
		state:          opts.State,
		logger:         opts.Logger,
		resolveModel:   opts.ResolveModel,
		getAgentConfig: opts.GetAgentConfig,
		tools:          opts.Tools,
		getAgentThread: opts.GetAgentThread,
		journal:        opts.Journal,
		scrubToolCopy:  opts.ScrubToolCopy,
		onExit:         opts.OnExit,
	}

	go a.loop()
	return a
}

// scrubCopy sanitizes one observable copy of tool-call content:
// the agent's known secrets plus the pattern battery when the backend
// seam is bound, the pattern battery alone otherwise. The copy is detached
// from the invocation's cancel — a stopped call's text still needs its
// credentials removed — and never fails the turn.
func (a *UserAgent) scrubCopy(ctx context.Context, content string) string {
	if content == "" {
		return content
	}
	if a.scrubToolCopy == nil {
		return fanOutScrubber(content)
	}
	return a.scrubToolCopy(context.WithoutCancel(ctx), a.state.agentID, content)
}

// prefixSnapshotLocked captures the live turn's replayable state, or
// nil when no turn runs, for the final synchronization hand-off: a
// synchronizing device receives the text and invocations that precede
// the chunks still to come. Caller holds the agent state's snapMu
// across the whole hand-off step, so the cut and the connection
// registration are atomic — a chunk flushed after the snapshot follows
// the registration instead of falling between the two. The streaming
// flush shares snapMu: a prefix never includes a chunk whose frame has
// not been written, and never omits one that has.
func (a *UserAgent) prefixSnapshotLocked() *wstypes.TurnPrefix {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.turnMsgID == "" {
		return nil
	}
	prefix := &wstypes.TurnPrefix{
		ItemID:  a.turnMsgID,
		Text:    a.turnPrefix.String(),
		LastSeq: a.turnSeq.Load(),
	}
	if a.recorder != nil {
		prefix.Invocations = a.recorder.snapshotInvocations()
	}
	return prefix
}

func (a *UserAgent) loop() {
	for {
		claim := a.state.claim()
		if claim == nil {
			// Nothing may run now — paused, drained, or empty. Exit
			// outright: the transcript lives in the backend's thread
			// store, and runnable work respawns a worker on arrival.
			a.exit()
			return
		}
		a.setWorking(true)
		outcome := a.processTurn(claim)
		a.setWorking(false)
		// finish reports whether more work may run now; its false
		// arm is claim's own nil predicate, so the worker retires
		// here instead of paying another claim round to learn it.
		if !a.state.finish(outcome) {
			a.exit()
			return
		}
	}
}

// exit tells the lane's connections the worker is gone, then runs
// the exit hook exactly once. The final turn bracket leaves active
// true while the worker lives; without this broadcast the strip
// shows a live lane until the next status reply. The broadcast
// precedes the hook so a respawned worker's own events can never be
// overtaken by this one.
func (a *UserAgent) exit() {
	status := a.LaneStatus()
	status.Active = false
	_ = a.state.conns.WriteJSON(a.state.rootCtx, wstypes.Event{
		Type:    wstypes.EventTypeLaneStatus,
		AgentID: a.state.agentID,
		Data:    status,
	})
	if a.onExit != nil {
		a.onExit(a)
	}
}

// processTurn runs exactly one claimed item: persist its prompt, read
// the conversation the backend owns, run the model, and settle —
// assembled, or whatever streamed when the item was stopped. The
// returned outcome drives the queue's pause gate.
func (a *UserAgent) processTurn(claim *turnClaim) turnOutcome {
	msgID := claim.item.id
	entry := claim.item.model

	// Events outlive the turn's context: an interrupted turn still
	// reports why it ended.
	emitCtx := context.WithoutCancel(claim.ctx)

	// The turn's replay state starts now: joins during this turn
	// replay whatever streams from here on.
	a.beginTurn(msgID)

	// A command item is the whole turn: the durable thread switch and
	// its completion event, never a model call or a user row.
	if claim.item.kind == wstypes.QueueItemKindNewThread {
		return a.processCommand(claim, emitCtx, msgID)
	}

	// Starting is the first durable step: the execution thread resolves,
	// the user row commits, and the item is running before any model or
	// tool work. A prompt that cannot start never reaches the model.
	if a.journal == nil {
		a.logger.Error().Str("agent_id", a.state.agentID).Msg("lane has no lifecycle journal")
		a.failTurn(emitCtx, msgID, "no lifecycle journal configured", "")
		return outcomeMessageFailed
	}
	// The started batch and the running projection land in one
	// publication section: subscribers never see an active item whose
	// durable identity has not been published with it.
	if err := a.state.startItem(func() (*backend.ItemStart, *backend.JournalBatch, error) {
		return a.journal.StartItem(claim.ctx, a.state.agentID, msgID)
	}); err != nil {
		a.logger.Error().Err(err).Str("agent_id", a.state.agentID).Msg("start item")
		a.failTurn(emitCtx, msgID, "persist item start: "+err.Error(), "")
		return outcomeMessageFailed
	}

	// The thread source is required, like the resolver: the working
	// context is the committed user row.
	if a.getAgentThread == nil {
		a.logger.Error().Str("agent_id", a.state.agentID).Msg("lane has no thread source")
		if a.failTurn(emitCtx, msgID, "no thread source configured", "") {
			return outcomeMessageFailed
		}
		return outcomeTerminalUnrecorded
	}
	// The turn's input is the backend's read: it already includes the
	// row just committed, so the prompt appears exactly once — the
	// committed user row is the prompt, unconditionally. A failed load
	// fails the turn; a stop during it settles as a quiet stop.
	records, err := a.getAgentThread(claim.ctx, a.state.agentID)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "load conversation history", err)
	}
	runInput := mapAgentThread(records)

	// The resolver seam is immutable after construction, read
	// directly like every other seam.
	if a.resolveModel == nil {
		a.logger.Error().Str("agent_id", a.state.agentID).Msg("lane has no model resolver")
		a.failTurn(emitCtx, msgID, "no model resolver configured", "")
		return outcomeMessageFailed
	}
	cfg, err := a.resolveModel(claim.ctx, entry)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "resolve model", err)
	}
	chatModel, err := createChatModel(claim.ctx, cfg)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "build chat model", err)
	}
	// The persona is fetched per turn: config edits apply to the next
	// prompt without lane churn. A missing config or a failed read
	// fails open — a persona-less answer beats none, unlike the
	// resolver, where no model means no answer at all.
	instruction := ""
	if a.getAgentConfig != nil {
		cfg, err := a.getAgentConfig(claim.ctx, a.state.agentID)
		if err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.state.agentID).
				Msg("fetch agent config; turn proceeds without persona")
		} else {
			instruction = BuildSystemPrompt(cfg)
		}
	}
	// The recorder owns this turn's replay rows: the native assistant
	// hook writes call groups, the tool wrapper writes outcomes.
	rec := newTurnRecorder(a, msgID, emitCtx)
	a.setRecorder(rec)
	// Tools arrive through their own seam — permissions and global
	// switches already applied, ready to wrap. A failed read or an
	// unreadable manifest fails the turn; an empty set is an honest
	// tool-less turn. A stop during setup is not a failure.
	var turnTools []tool.BaseTool
	if a.tools != nil {
		impls, err := a.tools(claim.ctx, a.state.agentID)
		if err != nil {
			return a.setupFailure(emitCtx, claim, msgID, "resolve tools", err)
		}
		if turnTools, err = a.wrapTools(claim.ctx, rec, impls); err != nil {
			return a.setupFailure(emitCtx, claim, msgID, "wrap tools", err)
		}
	}
	handlers := []adk.TypedChatModelAgentMiddleware[*schema.Message]{rec}
	einoAgent, err := buildEinoAgent(claim.ctx, a.state.agentID, instruction, chatModel, turnTools, handlers)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "build agent", err)
	}
	runner, err := buildEinoRunner(claim.ctx, einoAgent)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "build runner", err)
	}

	var answer strings.Builder
	// The model receives the conversation the backend returned — prior
	// turns' replay-eligible history plus this prompt exactly once. The
	// system instruction rides the agent, so stale personas never
	// accumulate in the history itself.
	iterator := runner.Run(claim.ctx, runInput)
	for event, ok := iterator.Next(); ok; event, ok = iterator.Next() {
		if event.Err != nil {
			return a.failEvent(emitCtx, claim, msgID, event.Err, &answer, rec)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		// Tool rounds surface as Role=Tool outputs here and are
		// skipped; wire events and replay rows for them come from the
		// recorder and its tool wrappers, which run beside this loop.
		out := event.Output.MessageOutput
		if out.Role != schema.Assistant {
			continue
		}
		if out.MessageStream != nil {
			if err := a.streamAnswer(emitCtx, out.MessageStream, msgID, &answer); err != nil {
				return a.failEvent(emitCtx, claim, msgID, err, &answer, rec)
			}
			continue
		}
		if out.Message != nil {
			answer.WriteString(out.Message.Content)
		}
	}
	// Recording is over before settlement: a stale invocation may
	// still report its wire event, but cannot add a durable row after
	// this turn's closing row.
	rec.close()
	// The answer settles: persist first so the UI's refetch cannot
	// outrun the write, then broadcast. A failed turn settles through
	// failTurn instead; a crash between the two leaves the user row
	// standing alone. The terminal row carries only the terminal
	// round's text — earlier rounds are their own recorded rows.
	return a.settleTurn(emitCtx, msgID, rec.terminalContent(answer.String()))
}

// failEvent classifies one turn error. A required-write failure wins
// over the stop check: a write racing a user stop is still a message
// failure. A stopped turn settles quietly; anything else fails the
// turn (tool-attributed failures do not pause the queue).
func (a *UserAgent) failEvent(emitCtx context.Context, claim *turnClaim, msgID string, err error, answer *strings.Builder, rec *turnRecorder) turnOutcome {
	rec.close()
	var rwe *requiredWriteError
	if errors.As(err, &rwe) {
		a.logger.Error().Err(rwe.err).Str("agent_id", a.state.agentID).Msg("persist turn message")
		if a.failTurn(emitCtx, msgID, "persist turn message: "+rwe.err.Error(), "") {
			return outcomeMessageFailed
		}
		return outcomeTerminalUnrecorded
	}
	if turnStopped(claim.ctx) {
		return a.finishInterrupted(emitCtx, msgID, answer)
	}
	a.logger.Error().Err(err).Str("agent_id", a.state.agentID).Msg("model query")
	text, toolName := a.turnError(emitCtx, err)
	if a.failTurn(emitCtx, msgID, text, toolName) {
		// A tool-attributed failure already shows on its own row and
		// must not pause the queue.
		if toolName != "" {
			return outcomeToolFailed
		}
		return outcomeMessageFailed
	}
	return outcomeTerminalUnrecorded
}

// mapAgentThread converts thread records into model messages.
// Arguments ride as the model-authored strings, unchanged. The system
// prompt is deliberately absent: each turn's instruction supplies it
// fresh, so stale personas never accumulate.
func mapAgentThread(msgs []backend.AgentThreadMessage) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case apitypes.ThreadRoleUser:
			out = append(out, schema.UserMessage(m.Content))
		case apitypes.ThreadRoleAssistant:
			msg := schema.AssistantMessage(m.Content, nil)
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
					ID:       tc.ID,
					Function: schema.FunctionCall{Name: tc.Name, Arguments: tc.Arguments},
				})
			}
			out = append(out, msg)
		default: // tool
			out = append(out, schema.ToolMessage(m.Content, m.ToolCallID, schema.WithToolName(m.ToolName)))
		}
	}
	return out
}

// setupFailure classifies a pre-stream failure: an intentional
// cancel (a send-now reservation landed before the first token) is a
// stop, not a pause-worthy error; anything else fails the message.
func (a *UserAgent) setupFailure(emitCtx context.Context, claim *turnClaim, msgID, site string, err error) turnOutcome {
	if turnStopped(claim.ctx) {
		return a.finishInterrupted(emitCtx, msgID, &strings.Builder{})
	}
	a.logger.Error().Err(err).Str("agent_id", a.state.agentID).Msg(site)
	if a.failTurn(emitCtx, msgID, err.Error(), "") {
		return outcomeMessageFailed
	}
	return outcomeTerminalUnrecorded
}

// turnStopped separates a user stop from a real failure: the turn's
// context has exactly one cancel source (the queue's interrupt), so a
// canceled context alone identifies a stop.
func turnStopped(queryCtx context.Context) bool {
	return queryCtx.Err() != nil
}

// finishInterrupted ends a user-stopped turn: the item settles as
// stopped, carrying whatever streamed as its answer row. A quiet stop
// commits no row at all — no error bubble, no empty assistant row: a
// stop is not a failure, the user message already stands in the
// transcript, and the next prompt is simply another user turn.
func (a *UserAgent) finishInterrupted(emitCtx context.Context, msgID string, answer *strings.Builder) turnOutcome {
	var drafts []backend.ThreadMessageDraft
	if answer.Len() > 0 {
		drafts = []backend.ThreadMessageDraft{{
			Role: apitypes.ThreadRoleAssistant, Content: answer.String(), TurnID: msgID,
		}}
	}
	if a.journal == nil {
		a.clearTurnPrefix()
		return outcomeStopped
	}
	err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		return a.journal.FinishItem(emitCtx, a.state.agentID, msgID, apitypes.ItemStopped, "", drafts)
	})
	if err != nil {
		a.logger.Warn().Err(err).Str("agent_id", a.state.agentID).Msg("persist stopped item")
		a.emitError(emitCtx, "persist turn outcome: "+err.Error(), "", msgID)
		return outcomeTerminalUnrecorded
	}
	a.clearTurnPrefix()
	return outcomeStopped
}

// settleTurn persists the terminal answer and broadcasts the committed
// batch — the persist-before-emit ordering is what keeps the durable
// feed evidence of a recorded outcome rather than a claim. A failed
// persist settles the item as failed through failTurn; when even that
// cannot commit, the outcome is unrecorded and the lane must halt.
func (a *UserAgent) settleTurn(emitCtx context.Context, msgID, content string) turnOutcome {
	if a.journal == nil {
		a.failTurn(emitCtx, msgID, "no lifecycle journal configured", "")
		return outcomeMessageFailed
	}
	err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		return a.journal.FinishItem(emitCtx, a.state.agentID, msgID, apitypes.ItemCompleted, "",
			[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant,
				Content: content, TurnID: msgID}})
	})
	if err != nil {
		a.logger.Error().Err(err).Str("agent_id", a.state.agentID).Msg("persist assistant message")
		if a.failTurn(emitCtx, msgID, "persist assistant message: "+err.Error(), "") {
			return outcomeMessageFailed
		}
		return outcomeTerminalUnrecorded
	}
	a.clearTurnPrefix()
	return outcomeSucceeded
}

// beginTurn resets the replay state for a new turn; a synchronizing
// device receives whatever streams from here on.
func (a *UserAgent) beginTurn(msgID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turnMsgID = msgID
	a.turnPrefix.Reset()
	a.recorder = nil
	a.turnSeq.Store(0)
}

// setRecorder installs the turn's recorder for prefix snapshots; nil at
// turn end.
func (a *UserAgent) setRecorder(rec *turnRecorder) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.recorder = rec
}

// clearTurnPrefix retires the replay state at turn end, before the
// closing event goes out.
func (a *UserAgent) clearTurnPrefix() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turnPrefix.Reset()
	a.turnMsgID = ""
	a.recorder = nil
}

// dropCommittedPrefix retires committed rounds' text from the replay
// prefix: those rows ride the journal now, so a mid-turn snapshot must
// not repeat them. The generation bump also stops the committed round's
// own stream — whose drain may still be flushing — from re-extending
// the prefix after the cut. Same locks as the streaming flush's cut.
func (a *UserAgent) dropCommittedPrefix() {
	a.state.snapMu.Lock()
	defer a.state.snapMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turnPrefix.Reset()
	a.turnPrefixGen++
}

// streamAnswer relays one streamed answer in coalesced deltas and
// accumulates the text for the final message event; each flush also
// extends the replay prefix mid-turn joins receive. Recv blocks, so
// it runs beside the loop and the flush window can fire while the
// stream is quiet. A join between a chunk's arrival and its flush is
// safe: the unflushed text can only enqueue after the join's backlog,
// so it arrives as subsequent chunks — never lost, never duplicated.
func (a *UserAgent) streamAnswer(ctx context.Context, stream *schema.StreamReader[*schema.Message], msgID string, answer *strings.Builder) error {
	defer stream.Close()

	type recvResult struct {
		chunk *schema.Message
		err   error
	}
	// Buffered so a recv never blocks the reader while the loop is
	// flushing: coalescing is the ticker's and the byte cap's job.
	recv := make(chan recvResult, 8)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			chunk, err := stream.Recv()
			select {
			case recv <- recvResult{chunk: chunk, err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var pending strings.Builder
	// The prefix carries only this round's text while it is still
	// uncommitted: a round whose rows committed mid-drain rides the
	// journal, so its chunks keep flowing to live connections but stop
	// extending the replay prefix.
	a.mu.Lock()
	gen := a.turnPrefixGen
	a.mu.Unlock()
	flush := func() {
		if pending.Len() == 0 {
			return
		}
		delta := pending.String()
		pending.Reset()
		// The prefix extension, its sequence, and the frame share one cut
		// under snapMu, so a device registering mid-turn receives text
		// and sequence that agree — never a chunk it already has in the
		// prefix, never a hole. The cut is deliberately not pubMu: a
		// journal write must never queue behind streaming frames.
		a.state.snapMu.Lock()
		a.mu.Lock()
		if a.turnPrefixGen == gen {
			a.turnPrefix.WriteString(delta)
		}
		a.mu.Unlock()
		// The sequence is stream-local: a synchronizing device compares
		// it with the prefix's last sequence and reconnects on a gap
		// instead of displaying torn text.
		seq := a.turnSeq.Add(1)
		_ = a.state.conns.WriteJSON(ctx, wstypes.Event{
			Type:    wstypes.EventTypeMessageChunk,
			AgentID: a.state.agentID,
			Data:    wstypes.MessageChunkEvent{ID: msgID, Delta: delta, Seq: seq},
		})
		a.state.snapMu.Unlock()
	}
	// The reasoning flag covers the whole stream: on while reasoning
	// tokens arrive, off once the answer or the stream ends. The
	// buffered tail flushes before that closing status.
	defer a.setReasoning(false)
	defer flush()

	ticker := time.NewTicker(streamCoalesceWindow)
	defer ticker.Stop()
	for {
		select {
		case r := <-recv:
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					return nil
				}
				return r.err
			}
			if r.chunk == nil {
				continue
			}
			// The flag is a transition signal, not a per-token pulse:
			// setReasoning broadcasts only when the value changes, so
			// these calls cost one status frame per phase change.
			if r.chunk.Content != "" {
				a.setReasoning(false)
			} else if r.chunk.ReasoningContent != "" {
				a.setReasoning(true)
			}
			if r.chunk.Content == "" {
				continue
			}
			answer.WriteString(r.chunk.Content)
			pending.WriteString(r.chunk.Content)
			if pending.Len() >= streamCoalesceBytes {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// LaneStatus snapshots the worker onto the wire type. Locks are
// taken and released in order — worker flags, then queue data — and
// never held across a broadcaster call.
func (a *UserAgent) LaneStatus() wstypes.LaneStatus {
	if a == nil {
		return wstypes.LaneStatus{}
	}
	a.mu.Lock()
	working := a.working
	reasoning := a.reasoning
	a.mu.Unlock()

	return wstypes.LaneStatus{
		// Process liveness, not a per-worker signal: a live root
		// context means this process still owns the lane. exit()
		// settles the final status by reporting the worker gone;
		// Drain is what cancels the root context.
		Active:    a.state.rootCtx.Err() == nil,
		Working:   working,
		Reasoning: reasoning,
		Model:     a.state.activeModelID(),
	}
}

// setWorking flips the turn-running flag and, on change, tells the
// lane's connections.
func (a *UserAgent) setWorking(working bool) {
	a.mu.Lock()
	if a.working == working {
		a.mu.Unlock()
		return
	}
	a.working = working
	a.mu.Unlock()
	_ = a.state.conns.WriteJSON(a.state.rootCtx, wstypes.Event{
		Type:    wstypes.EventTypeLaneStatus,
		AgentID: a.state.agentID,
		Data:    a.LaneStatus(),
	})
}

// setReasoning flips the model-reasoning flag and, on change, tells
// the lane's connections.
func (a *UserAgent) setReasoning(reasoning bool) {
	a.mu.Lock()
	if a.reasoning == reasoning {
		a.mu.Unlock()
		return
	}
	a.reasoning = reasoning
	a.mu.Unlock()
	_ = a.state.conns.WriteJSON(a.state.rootCtx, wstypes.Event{
		Type:    wstypes.EventTypeLaneStatus,
		AgentID: a.state.agentID,
		Data:    a.LaneStatus(),
	})
}
