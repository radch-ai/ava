package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/coder/websocket"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

const (
	// defaultIdleTimeout is how long a worker survives its last turn
	// before the goroutine exits. The queue and its subscribers are
	// manager-owned and survive the exit.
	defaultIdleTimeout = 15 * time.Minute
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

// TurnSink appends a turn's messages to an agent's thread. An alias
// of backend TurnService.AppendThreadMessages, so the bound method
// value needs no adapter.
type TurnSink = func(ctx context.Context, agentID string, msgs []apitypes.ThreadMessageDraft) error

// AgentConfigSource reads an agent's editable config — the prompt
// texts and tool list a turn needs. An alias of backend
// TurnService.GetAgentConfig, so the bound method value needs no
// adapter.
type AgentConfigSource = func(ctx context.Context, agentID string) (*apitypes.AgentConfig, error)

// AgentToolsFunc fetches an agent's effective tools for a turn —
// permissions applied, ready for the capture wrapper. An alias of
// backend TurnService.AgentTools, so the bound method value needs
// no adapter.
type AgentToolsFunc = func(ctx context.Context, agentID string) ([]tool.InvokableTool, error)

// turnOutcome classifies a settled turn for the queue: only
// outcomeMessageFailed pauses automatic dispatch.
type turnOutcome int

const (
	outcomeSucceeded turnOutcome = iota
	outcomeStopped
	outcomeToolFailed
	outcomeMessageFailed
)

// messageFailed reports whether the outcome gates the queue.
func (o turnOutcome) messageFailed() bool { return o == outcomeMessageFailed }

type UserAgentOptions struct {
	AgentID     string
	State       *agentState
	IdleTimeout time.Duration
	OnExit      func(*UserAgent)
	// ResolveModel maps a selection to a callable endpoint for
	// every lane; bound by the gateway at boot.
	ResolveModel ModelResolver
	// PersistTurn appends the turn's messages to the thread; bound
	// by the gateway at boot.
	PersistTurn TurnSink
	// GetAgentConfig reads the agent's config for each turn's
	// prompt; bound by the gateway at boot.
	GetAgentConfig AgentConfigSource
	// Tools fetches the agent's effective tools for each turn —
	// permissions applied; bound by the gateway at boot. A failed
	// read fails the turn; a user stop during it settles as a stop.
	Tools  AgentToolsFunc
	Logger zerolog.Logger
}

// UserAgent is one agent's disposable runtime worker: it executes
// queue items one at a time and owns nothing the queue depends on.
// Subscriptions and pending work live on the manager-owned state.
type UserAgent struct {
	agentID string
	ctx     context.Context

	state *agentState
	mu    sync.Mutex

	// working reports a turn in flight; reasoning reports the model
	// streaming reasoning tokens within it. Both guarded by mu.
	working   bool
	reasoning bool

	// turnMsgID and turnPrefix track the live turn's streamed text —
	// the prefix a mid-turn join replays as one chunk. Guarded by
	// mu; cleared at turn end before the closing event.
	turnMsgID  string
	turnPrefix strings.Builder

	idleTimeout time.Duration
	onExit      func(*UserAgent)
	logger      zerolog.Logger

	resolveModel   ModelResolver
	persistTurn    TurnSink
	getAgentConfig AgentConfigSource
	tools          AgentToolsFunc
}

// newUserAgent creates a worker and starts its claim loop. The
// turn-context mint is wired to the worker's context so cancelling
// the worker ends any turn still running.
func newUserAgent(ctx context.Context, opts UserAgentOptions) *UserAgent {
	a := &UserAgent{
		// The worker's context is the manager's root: cancelling it
		// ends the worker. Turn settlement rides the emit context,
		// which outlives the turn's own cancel.
		ctx:            ctx,
		agentID:        opts.AgentID,
		state:          opts.State,
		idleTimeout:    opts.IdleTimeout,
		logger:         opts.Logger,
		resolveModel:   opts.ResolveModel,
		persistTurn:    opts.PersistTurn,
		getAgentConfig: opts.GetAgentConfig,
		tools:          opts.Tools,
		onExit:         opts.OnExit,
	}

	if a.idleTimeout == 0 {
		a.idleTimeout = defaultIdleTimeout
	}
	if a.state != nil {
		a.state.setTurnCtxMint(func() (context.Context, context.CancelFunc) {
			return context.WithCancel(a.ctx)
		})
	}

	go a.loop()
	return a
}

// Join attaches a connection mid-life: the queue snapshot first, so
// a joiner cannot mistake reserved or active work for lost, then the
// live turn's accumulated text as one chunk ordered before whatever
// still streams. Both are captured under data locks inside the
// broadcaster's backlog callback; no path may take pubMu there.
func (a *UserAgent) Join(connID string, conn *websocket.Conn) {
	a.state.conns.AddWithBacklog(connID, conn, func() []any {
		snapshot := a.state.stateSnapshot()

		a.mu.Lock()
		working := a.working
		msgID := a.turnMsgID
		var prefix string
		if working {
			prefix = a.turnPrefix.String()
		}
		a.mu.Unlock()

		backlog := []any{wstypes.Event{
			Type:    wstypes.EventTypeQueueState,
			AgentID: a.agentID,
			Data:    snapshot,
		}}
		if working && prefix != "" {
			backlog = append(backlog, wstypes.Event{
				Type:    wstypes.EventTypeMessageChunk,
				AgentID: a.agentID,
				Data:    wstypes.MessageChunkEvent{ID: msgID, Delta: prefix},
			})
		}
		return backlog
	})
}

// Interrupt is the Stop button: cancel the active item's turn and
// hold the queue behind it. Send-now reservations and drain use the
// bare interruptActive — they never arm the gate.
func (a *UserAgent) Interrupt() {
	a.state.stopNow()
}

// RemoveConn drops a connection from the shared subscriber set.
func (a *UserAgent) RemoveConn(connID string) {
	a.state.conns.Remove(connID)
}

func (a *UserAgent) loop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		claim := a.state.claim()
		if claim != nil {
			a.setWorking(true)
			outcome := a.processTurn(claim)
			a.setWorking(false)
			a.state.finish(outcome.messageFailed())
			// Reset after the turn so a long query never races the
			// idle timer.
			idleTimer.Reset(a.idleTimeout)
			continue
		}

		select {
		case <-a.state.wake:
		case <-idleTimer.C:
			// Idle exit releases the worker only: pending work and
			// subscribers stay with the manager-owned state.
			if a.state.hasWork() {
				idleTimer.Reset(a.idleTimeout)
				continue
			}
			a.exit()
			return
		case <-a.ctx.Done():
			a.exit()
			return
		}
	}
}

// exit runs the worker's exit hook exactly once.
func (a *UserAgent) exit() {
	if a.onExit != nil {
		a.onExit(a)
	}
}

// processTurn runs exactly one claimed item: resolve its captured
// model selection, query, and settle — assembled, or whatever
// streamed when the item was stopped. The returned outcome drives
// the queue's pause gate.
func (a *UserAgent) processTurn(claim *turnClaim) turnOutcome {
	msgID := claim.item.id
	entry := claim.item.model
	input := claim.item.content

	// Events outlive the turn's context: an interrupted turn still
	// reports why it ended.
	emitCtx := context.WithoutCancel(claim.ctx)

	// The turn's replay state starts now: joins during this turn
	// replay whatever streams from here on.
	a.beginTurn(msgID)

	// The user's words persist before anything can fail: every
	// failure mode below keeps the prompt in the thread — failures
	// append their own error row, and a crash before either leaves
	// the user row standing alone. The row carries the claim's user
	// id, minted apart from the stream id.
	if a.persistTurn != nil {
		if err := a.persistTurn(emitCtx, a.agentID,
			[]apitypes.ThreadMessageDraft{{ID: claim.userMsgID, Role: apitypes.ThreadRoleUser, Content: input}}); err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.agentID).Msg("persist user message")
		}
	}

	// The resolver seam is immutable after construction, read
	// directly like every other seam.
	if a.resolveModel == nil {
		a.logger.Error().Str("agent_id", a.agentID).Msg("lane has no model resolver")
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
		cfg, err := a.getAgentConfig(claim.ctx, a.agentID)
		if err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.agentID).
				Msg("fetch agent config; turn proceeds without persona")
		} else {
			instruction = BuildSystemPrompt(cfg)
		}
	}
	// Tools arrive through their own seam — permissions and global
	// switches already applied, ready to wrap. A failed read or an
	// unreadable manifest fails the turn; an empty set is an honest
	// tool-less turn. A stop during setup is not a failure.
	var turnTools []tool.BaseTool
	if a.tools != nil {
		impls, err := a.tools(claim.ctx, a.agentID)
		if err != nil {
			return a.setupFailure(emitCtx, claim, msgID, "resolve tools", err)
		}
		if turnTools, err = a.wrapTools(claim.ctx, emitCtx, msgID, impls); err != nil {
			return a.setupFailure(emitCtx, claim, msgID, "wrap tools", err)
		}
	}
	einoAgent, err := buildEinoAgent(claim.ctx, a.agentID, instruction, chatModel, turnTools)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "build agent", err)
	}
	runner, err := buildEinoRunner(claim.ctx, einoAgent)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "build runner", err)
	}

	var answer strings.Builder
	iterator := runner.Query(claim.ctx, input)
	for event, ok := iterator.Next(); ok; event, ok = iterator.Next() {
		if event.Err != nil {
			if turnStopped(claim.ctx) {
				a.finishInterrupted(emitCtx, msgID, &answer)
				return outcomeStopped
			}
			a.logger.Error().Err(event.Err).Str("agent_id", a.agentID).Msg("model query")
			text, toolName := turnError(event.Err)
			a.failTurn(emitCtx, msgID, text, toolName)
			return a.failureOutcome(toolName)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		// Tool rounds surface as Role=Tool outputs here and are
		// skipped; wire events and thread rows for them come from the
		// tool wrappers, not this loop.
		out := event.Output.MessageOutput
		if out.Role != schema.Assistant {
			continue
		}
		if out.MessageStream != nil {
			if err := a.streamAnswer(emitCtx, out.MessageStream, msgID, &answer); err != nil {
				if turnStopped(claim.ctx) {
					a.finishInterrupted(emitCtx, msgID, &answer)
					return outcomeStopped
				}
				a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("model stream")
				text, toolName := turnError(err)
				a.failTurn(emitCtx, msgID, text, toolName)
				return a.failureOutcome(toolName)
			}
			continue
		}
		if out.Message != nil {
			answer.WriteString(out.Message.Content)
		}
	}
	// The answer settles: persist first so the UI's refetch cannot
	// outrun the write, then broadcast. A failed turn settles through
	// failTurn instead; a crash between the two leaves the user row
	// standing alone.
	a.settleTurn(emitCtx, msgID, answer.String())
	return outcomeSucceeded
}

// setupFailure classifies a pre-stream failure: an intentional
// cancel (a send-now reservation landed before the first token) is a
// stop, not a pause-worthy error; anything else fails the message.
func (a *UserAgent) setupFailure(emitCtx context.Context, claim *turnClaim, msgID, site string, err error) turnOutcome {
	if turnStopped(claim.ctx) {
		a.finishInterrupted(emitCtx, msgID, &strings.Builder{})
		return outcomeStopped
	}
	a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg(site)
	a.failTurn(emitCtx, msgID, err.Error(), "")
	return outcomeMessageFailed
}

// failureOutcome maps a turn failure to its queue outcome: a
// tool-attributed failure already shows on its own row and must not
// pause the queue.
func (a *UserAgent) failureOutcome(toolName string) turnOutcome {
	if toolName != "" {
		return outcomeToolFailed
	}
	return outcomeMessageFailed
}

// turnStopped separates a user stop from a real failure: the turn's
// context has exactly one cancel source (the queue's interrupt), so a
// canceled context alone identifies a stop.
func turnStopped(queryCtx context.Context) bool {
	return queryCtx.Err() != nil
}

// finishInterrupted ends a user-stopped turn. Whatever streamed
// settles like a completed answer — history keeps it. An empty stop
// emits nothing at all — no error, no draft row: a stop is not a
// failure, the user message already stands in the transcript, and
// the next prompt is simply another user turn.
func (a *UserAgent) finishInterrupted(emitCtx context.Context, msgID string, answer *strings.Builder) {
	if answer.Len() > 0 {
		a.settleTurn(emitCtx, msgID, answer.String())
	}
}

// settleTurn persists the assistant row, then broadcasts the final
// message event — the persist-before-emit ordering is what keeps the
// UI's settle refetch from racing the write. The replay prefix
// retires first so a join racing the settle never replays a
// finished turn.
func (a *UserAgent) settleTurn(emitCtx context.Context, msgID, content string) {
	if a.persistTurn != nil {
		if err := a.persistTurn(emitCtx, a.agentID,
			[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: content}}); err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.agentID).Msg("persist assistant message")
		}
	}
	a.clearTurnPrefix()
	_ = a.state.conns.WriteJSON(emitCtx, wstypes.Event{
		Type:    wstypes.EventTypeMessage,
		AgentID: a.agentID,
		Data:    wstypes.MessageEvent{ID: msgID, Content: content},
	})
}

// beginTurn resets the replay state for a new turn; joins during the
// turn replay whatever streams from here on.
func (a *UserAgent) beginTurn(msgID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turnMsgID = msgID
	a.turnPrefix.Reset()
}

// clearTurnPrefix retires the replay state at turn end, before the
// closing event goes out.
func (a *UserAgent) clearTurnPrefix() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turnPrefix.Reset()
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
	recv := make(chan recvResult)
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
	flush := func() {
		if pending.Len() == 0 {
			return
		}
		delta := pending.String()
		pending.Reset()
		a.mu.Lock()
		a.turnPrefix.WriteString(delta)
		a.mu.Unlock()
		_ = a.state.conns.WriteJSON(ctx, wstypes.Event{
			Type:    wstypes.EventTypeMessageChunk,
			AgentID: a.agentID,
			Data:    wstypes.MessageChunkEvent{ID: msgID, Delta: delta},
		})
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

	modelID := a.state.activeModelID()
	return wstypes.LaneStatus{
		Active:    a.ctx.Err() == nil,
		Working:   working,
		Reasoning: reasoning,
		Model:     modelID,
		QueueLen:  a.state.queueLen(),
		ConnCount: a.state.conns.Count(),
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
	_ = a.state.conns.WriteJSON(a.ctx, wstypes.Event{
		Type:    wstypes.EventTypeLaneStatus,
		AgentID: a.agentID,
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
	_ = a.state.conns.WriteJSON(a.ctx, wstypes.Event{
		Type:    wstypes.EventTypeLaneStatus,
		AgentID: a.agentID,
		Data:    a.LaneStatus(),
	})
}
