package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/coder/websocket"
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

// TurnSink appends a turn's messages to the agent's active thread. An
// alias of backend TurnService.AppendThreadMessages, so the bound
// method value needs no adapter. The backend resolves the active
// thread; the gateway never learns thread identities.
type TurnSink = func(ctx context.Context, agentID string, msgs []backend.ThreadMessageDraft) error

// AgentConfigSource reads an agent's editable config — the prompt
// texts and tool list a turn needs. An alias of backend
// TurnService.GetAgentConfig, so the bound method value needs no
// adapter.
type AgentConfigSource = func(ctx context.Context, agentID string) (*apitypes.AgentConfig, error)

// AgentToolsFunc fetches an agent's effective tools for a turn —
// permissions applied, ready for the capture wrapper. An alias of
// backend TurnService.AgentTools, so the bound method value needs no
// adapter.
type AgentToolsFunc = func(ctx context.Context, agentID string) ([]tool.InvokableTool, error)

// AgentThreadSource reads the agent's active thread as the turn's
// model input. An alias of backend TurnService.GetAgentThread, so the
// bound method value needs no adapter.
type AgentThreadSource = func(ctx context.Context, agentID string) ([]backend.AgentThreadMessage, error)

// ThreadActivator runs the backend's durable create-and-activate
// operation for a command item, returning the committed thread
// switch. An alias of backend TurnService.CreateAndActivateThread.
type ThreadActivator = func(ctx context.Context, agentID string) (*backend.ActivatedThread, error)

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
	AgentID string
	State   *agentState
	OnExit  func(*UserAgent)
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
	Tools AgentToolsFunc
	// GetAgentThread reads one thread as the turn's model input;
	// bound by the gateway at boot.
	GetAgentThread AgentThreadSource
	// CreateThread runs a command item's durable thread switch; bound
	// by the gateway at boot. An unbound seam fails commands closed.
	CreateThread ThreadActivator
	Logger       zerolog.Logger
}

// UserAgent is one agent's disposable runtime worker: it executes
// queue items one at a time and owns nothing the queue depends on.
// Subscriptions and pending work live on the manager-owned state. A
// worker exits as soon as no runnable work remains — the transcript
// lives in the backend's thread store, not a live worker.
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

	onExit         func(*UserAgent)
	logger         zerolog.Logger
	resolveModel   ModelResolver
	persistTurn    TurnSink
	getAgentConfig AgentConfigSource
	tools          AgentToolsFunc
	getAgentThread AgentThreadSource
	createThread   ThreadActivator
}

// newUserAgent creates a worker and starts its claim loop. The turn
// context is minted from the manager's root context, so cancelling the
// root ends any turn still running.
func newUserAgent(ctx context.Context, opts UserAgentOptions) *UserAgent {
	a := &UserAgent{
		// The worker's context is the manager's root: cancelling it
		// ends the worker. Turn settlement rides the emit context,
		// which outlives the turn's own cancel.
		ctx:            ctx,
		agentID:        opts.AgentID,
		state:          opts.State,
		logger:         opts.Logger,
		resolveModel:   opts.ResolveModel,
		persistTurn:    opts.PersistTurn,
		getAgentConfig: opts.GetAgentConfig,
		tools:          opts.Tools,
		getAgentThread: opts.GetAgentThread,
		createThread:   opts.CreateThread,
		onExit:         opts.OnExit,
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
		if !a.state.finish(outcome.messageFailed()) {
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
	_ = a.state.conns.WriteJSON(a.ctx, wstypes.Event{
		Type:    wstypes.EventTypeLaneStatus,
		AgentID: a.agentID,
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
	prompt := claim.item.content

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

	// The user's words persist before anything can fail: every
	// failure mode below keeps the prompt in the thread. The row
	// carries the claim's user id, minted apart from the stream id,
	// and the replay structure the working context records.
	if a.persistTurn != nil {
		if err := a.persistTurn(emitCtx, a.agentID,
			[]backend.ThreadMessageDraft{{ID: claim.userMsgID, Role: apitypes.ThreadRoleUser,
				Content: prompt, TurnID: msgID}}); err != nil {
			// Required write: a prompt that is not durable never
			// reaches the model.
			a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("persist user message")
			a.failTurn(emitCtx, msgID, "persist user message: "+err.Error(), "")
			return outcomeMessageFailed
		}
	}

	// The thread source is required, like the resolver: the working
	// context is the committed user row.
	if a.getAgentThread == nil {
		a.logger.Error().Str("agent_id", a.agentID).Msg("lane has no thread source")
		a.failTurn(emitCtx, msgID, "no thread source configured", "")
		return outcomeMessageFailed
	}
	// The turn's input is the backend's read: it already includes the
	// row just committed, so the prompt appears exactly once — the
	// committed user row is the prompt, unconditionally. A failed load
	// fails the turn; a stop during it settles as a quiet stop.
	records, err := a.getAgentThread(claim.ctx, a.agentID)
	if err != nil {
		return a.setupFailure(emitCtx, claim, msgID, "load conversation history", err)
	}
	runInput := mapAgentThread(records)

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
	// The recorder owns this turn's replay rows: the native assistant
	// hook writes call groups, the tool wrapper writes outcomes.
	rec := newTurnRecorder(a, a.persistTurn, msgID, emitCtx)
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
		if turnTools, err = a.wrapTools(claim.ctx, rec, impls); err != nil {
			return a.setupFailure(emitCtx, claim, msgID, "wrap tools", err)
		}
	}
	handlers := []adk.TypedChatModelAgentMiddleware[*schema.Message]{rec}
	einoAgent, err := buildEinoAgent(claim.ctx, a.agentID, instruction, chatModel, turnTools, handlers)
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
	if !a.settleTurn(emitCtx, msgID, rec.terminalContent(answer.String())) {
		return outcomeMessageFailed
	}
	return outcomeSucceeded
}

// failEvent classifies one turn error. A required-write failure wins
// over the stop check: a write racing a user stop is still a message
// failure. A stopped turn settles quietly; anything else fails the
// turn (tool-attributed failures do not pause the queue).
func (a *UserAgent) failEvent(emitCtx context.Context, claim *turnClaim, msgID string, err error, answer *strings.Builder, rec *turnRecorder) turnOutcome {
	rec.close()
	var rwe *requiredWriteError
	if errors.As(err, &rwe) {
		a.logger.Error().Err(rwe.err).Str("agent_id", a.agentID).Msg("persist turn message")
		a.failTurn(emitCtx, msgID, "persist turn message: "+rwe.err.Error(), "")
		return outcomeMessageFailed
	}
	if turnStopped(claim.ctx) {
		return a.finishInterrupted(emitCtx, msgID, answer)
	}
	a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("model query")
	text, toolName := turnError(err)
	a.failTurn(emitCtx, msgID, text, toolName)
	return a.failureOutcome(toolName)
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
// settles like a completed answer — the transcript keeps it. An empty
// stop emits nothing at all — no error, no draft row: a stop is not a
// failure, the user message already stands in the transcript, and
// the next prompt is simply another user turn.
func (a *UserAgent) finishInterrupted(emitCtx context.Context, msgID string, answer *strings.Builder) turnOutcome {
	if answer.Len() > 0 {
		if !a.settleTurn(emitCtx, msgID, answer.String()) {
			return outcomeMessageFailed
		}
		return outcomeStopped
	}
	a.clearTurnPrefix()
	return outcomeStopped
}

// settleTurn persists the assistant row, then broadcasts the final
// message event — the persist-before-emit ordering is what keeps the
// UI's settle refetch from racing the write. The replay prefix
// retires first so a join racing the settle never replays a finished
// turn. A failed persist fails the turn at message level: success is
// never reported for a conversation that is not durable.
func (a *UserAgent) settleTurn(emitCtx context.Context, msgID, content string) bool {
	if a.persistTurn != nil {
		if err := a.persistTurn(emitCtx, a.agentID,
			[]backend.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant,
				Content: content, TurnID: msgID}}); err != nil {
			a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("persist assistant message")
			a.failTurn(emitCtx, msgID, "persist assistant message: "+err.Error(), "")
			return false
		}
	}
	a.clearTurnPrefix()
	_ = a.state.conns.WriteJSON(emitCtx, wstypes.Event{
		Type:    wstypes.EventTypeMessage,
		AgentID: a.agentID,
		Data:    wstypes.MessageEvent{ID: msgID, Content: content},
	})
	return true
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
