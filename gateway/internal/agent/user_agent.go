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
	"github.com/radch-ai/ava/gateway/internal/wsconn"
	"github.com/radch-ai/ava/gateway/wstypes"
)

const (
	// defaultIdleTimeout is how long a lane survives its last message
	// before the goroutine exits and the lane is deleted.
	defaultIdleTimeout = 15 * time.Minute
)

// Message represents a message submitted to an agent. A nil Model
// follows the default routing.
type Message struct {
	ID      string
	Content string
	Model   *apitypes.ModelEntry
}

// ModelResolver maps a selection to a callable endpoint. An alias of
// the backend.ResolveModel signature, so the bound function needs no
// adapter.
type ModelResolver = func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error)

// TurnSink appends a turn's messages to the agent's thread. An alias
// of the backend.AppendThreadMessages signature, so the bound
// function needs no adapter.
type TurnSink = func(ctx context.Context, agentID string, msgs []apitypes.ThreadMessageDraft) error

// AgentConfigSource reads an agent's editable config — the prompt
// texts and tool list a turn needs. An alias of the
// backend.GetAgentConfig signature, so the bound function needs no
// adapter.
type AgentConfigSource = func(ctx context.Context, agentID string) (*apitypes.AgentConfig, error)

// ToolSource lists the tools a turn may attach — the registry the
// config's enabled list filters.
type ToolSource = func() []tool.BaseTool

type UserAgentOptions struct {
	AgentID     string
	Model       *apitypes.ModelEntry
	MsgCh       chan Message // passed from UserAgentManager
	IdleTimeout time.Duration
	OnExit      func()
	// ResolveModel maps a selection to a callable endpoint for
	// every lane; bound by the gateway at boot.
	ResolveModel ModelResolver
	// PersistTurn appends the turn's messages to the thread; bound
	// by the gateway at boot.
	PersistTurn TurnSink
	// GetAgentConfig reads the agent's config for each turn's
	// prompt; bound by the gateway at boot.
	GetAgentConfig AgentConfigSource
	// ToolSource lists the registered tools every turn filters its
	// enabled list against; the manager defaults it to the global
	// registry.
	ToolSource ToolSource
	Logger     zerolog.Logger
}

type UserAgent struct {
	agentID string
	ctx     context.Context

	MsgCh chan Message // owned by UserAgentManager, passed via options
	mu    sync.Mutex

	conns *wsconn.Broadcaster // Broadcasts to all connected devices

	// The running turn's cancellable context; Interrupt cancels it.
	queryCtx    context.Context
	queryCancel context.CancelFunc

	idleTimeout time.Duration
	onExit      func()
	logger      zerolog.Logger

	resolveModel   ModelResolver
	persistTurn    TurnSink
	getAgentConfig AgentConfigSource
	toolSource     ToolSource

	model *apitypes.ModelEntry
}

// newUserAgent creates a lane agent and starts its message loop.
// Returns nil if MsgCh is nil (graceful degradation per lane, doesn't crash the app).
func newUserAgent(ctx context.Context, opts UserAgentOptions) *UserAgent {
	// MsgCh is required for agent operation.
	// This should never happen in practice since UserAgentManager.SubmitMsg always
	// creates the channel before calling newUserAgent. The check exists for defensive
	// programming and for tests that may call newUserAgent directly.
	// Gracefully return nil rather than panic (per-lane error).
	if opts.MsgCh == nil {
		opts.Logger.Error().
			Str("agent_id", opts.AgentID).
			Msg("MsgCh is nil, cannot create agent")
		return nil
	}

	a := &UserAgent{
		ctx:     ctx,
		agentID: opts.AgentID,

		conns:          wsconn.NewBroadcaster(opts.Logger),
		model:          opts.Model,
		MsgCh:          opts.MsgCh,
		idleTimeout:    opts.IdleTimeout,
		logger:         opts.Logger,
		resolveModel:   opts.ResolveModel,
		persistTurn:    opts.PersistTurn,
		getAgentConfig: opts.GetAgentConfig,
		toolSource:     opts.ToolSource,

		onExit: opts.OnExit,
	}

	if a.idleTimeout == 0 {
		a.idleTimeout = defaultIdleTimeout
	}

	// Start the message processing loop
	go a.loop()

	return a
}

// AddConn adds a connection to the broadcaster.
// Called when a device sends a message to this agent.
func (a *UserAgent) addConn(connID string, conn *websocket.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conns.Add(connID, conn)
}

// Interrupt cancels the running turn, if any.
func (a *UserAgent) Interrupt() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.queryCancel != nil {
		a.queryCancel()
	}
}

// updateModel sets the model for the next query; nil follows the
// default routing.
func (a *UserAgent) updateModel(entry *apitypes.ModelEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = entry
}

// RemoveConn removes a connection from the broadcaster.
// Called when a device disconnects.
func (a *UserAgent) RemoveConn(connID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.conns.Remove(connID)
}

// ConnCount returns the number of active connections.
func (a *UserAgent) ConnCount() int {
	return a.conns.Count()
}

func (a *UserAgent) loop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case msg := <-a.MsgCh:
			// Coalesce the batch behind the first message so the
			// queue is empty between turns.
			messages := a.drainMessages(msg)
			queryCtx := a.startTurn()
			a.processTurn(queryCtx, messages)
			// Reset after the turn so a long query never races the
			// idle timer.
			idleTimer.Reset(a.idleTimeout)

		case <-idleTimer.C:
			a.mu.Lock()
			if len(a.MsgCh) == 0 {
				a.mu.Unlock()
				if a.onExit != nil {
					a.onExit()
				}
				return
			}
			a.mu.Unlock()
			idleTimer.Reset(a.idleTimeout)

		case <-a.ctx.Done():
			if a.onExit != nil {
				a.onExit()
			}
			return
		}
	}
}

func (a *UserAgent) drainMessages(first Message) []Message {
	messages := []Message{first}

	for {
		select {
		case m := <-a.MsgCh:
			messages = append(messages, m)
		default:
			return messages
		}
	}
}

// startTurn opens the turn's cancellable context, ending any turn
// that is still running.
func (a *UserAgent) startTurn() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.queryCancel != nil {
		a.queryCancel()
	}
	a.queryCtx, a.queryCancel = context.WithCancel(a.ctx)
	return a.queryCtx
}

// enabledTools keeps the registered tools the config enables. The
// registry lists the candidates; the enabled list decides, and an
// empty one attaches nothing — fresh agents never surprise with
// tool calls.
func (a *UserAgent) enabledTools(ctx context.Context, enabled *[]string) []tool.BaseTool {
	if enabled == nil || len(*enabled) == 0 || a.toolSource == nil {
		return nil
	}
	registered := a.toolSource()
	want := make(map[string]struct{}, len(*enabled))
	for _, name := range *enabled {
		want[name] = struct{}{}
	}
	var kept []tool.BaseTool
	for _, t := range registered {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		if _, ok := want[info.Name]; ok {
			kept = append(kept, t)
		}
	}
	return kept
}

// processTurn runs one turn: resolve the batch's model selection,
// query the model, and broadcast the assembled answer — or the
// failure — to the lane's connections.
func (a *UserAgent) processTurn(queryCtx context.Context, messages []Message) {
	if len(messages) == 0 {
		return
	}
	// Events outlive the turn's context: an interrupted turn still
	// reports why it ended.
	emitCtx := context.WithoutCancel(queryCtx)

	var input strings.Builder
	var msgID string
	var entry *apitypes.ModelEntry
	for i, m := range messages {
		if i > 0 {
			input.WriteString("\n")
		}
		input.WriteString(m.Content)
		if msgID == "" && m.ID != "" {
			msgID = m.ID
		}
		// The last message carrying a model wins.
		if m.Model != nil {
			entry = m.Model
		}
	}

	a.mu.Lock()
	resolveModel := a.resolveModel
	a.mu.Unlock()

	// The user's words persist before anything can fail: every
	// failure mode below keeps the prompt in the thread, and a crash
	// before the answer lands leaves an orphan user row — a failed
	// turn reads the same either way.
	if a.persistTurn != nil {
		if err := a.persistTurn(emitCtx, a.agentID,
			[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleUser, Content: input.String()}}); err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.agentID).Msg("persist user message")
		}
	}

	if resolveModel == nil {
		a.logger.Error().Str("agent_id", a.agentID).Msg("lane has no model resolver")
		a.emitError(emitCtx, "no model resolver configured")
		return
	}
	cfg, err := resolveModel(queryCtx, entry)
	if err != nil {
		a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("resolve model")
		a.emitError(emitCtx, err.Error())
		return
	}
	chatModel, err := createChatModel(queryCtx, cfg)
	if err != nil {
		a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("build chat model")
		a.emitError(emitCtx, err.Error())
		return
	}
	// The persona is fetched per turn: config edits apply to the next
	// prompt without lane churn. A missing config or a failed read
	// fails open — a persona-less answer beats none, unlike the
	// resolver, where no model means no answer at all. The same
	// fetch decides the turn's tools: no config means no tools.
	instruction := ""
	var turnTools []tool.BaseTool
	if a.getAgentConfig != nil {
		cfg, err := a.getAgentConfig(queryCtx, a.agentID)
		if err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.agentID).
				Msg("fetch agent config; turn proceeds without persona")
		} else {
			instruction = BuildSystemPrompt(cfg)
			turnTools = a.enabledTools(queryCtx, cfg.ToolsEnabled)
		}
	}
	einoAgent, err := buildEinoAgent(queryCtx, a.agentID, instruction, chatModel, turnTools)
	if err != nil {
		a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("build agent")
		a.emitError(emitCtx, err.Error())
		return
	}
	runner, err := buildEinoRunner(queryCtx, einoAgent)
	if err != nil {
		a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("build runner")
		a.emitError(emitCtx, err.Error())
		return
	}

	var answer strings.Builder
	iterator := runner.Query(queryCtx, input.String())
	for event, ok := iterator.Next(); ok; event, ok = iterator.Next() {
		if event.Err != nil {
			a.logger.Error().Err(event.Err).Str("agent_id", a.agentID).Msg("model query")
			a.emitError(emitCtx, event.Err.Error())
			return
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		// Tool rounds surface as Role=Tool outputs on the same
		// stream; only assistant rounds are the answer. Tool
		// activity has no wire or thread surface yet.
		out := event.Output.MessageOutput
		if out.Role != schema.Assistant {
			continue
		}
		if out.MessageStream != nil {
			if err := a.streamAnswer(emitCtx, out.MessageStream, msgID, &answer); err != nil {
				a.logger.Error().Err(err).Str("agent_id", a.agentID).Msg("model stream")
				a.emitError(emitCtx, err.Error())
				return
			}
			continue
		}
		if out.Message != nil {
			answer.WriteString(out.Message.Content)
		}
	}
	// The answer persists before the wire event: the UI's settle
	// refetch cannot outrun the write. Success only — a failed turn
	// leaves the drain-written user row standing alone.
	if a.persistTurn != nil {
		if err := a.persistTurn(emitCtx, a.agentID,
			[]apitypes.ThreadMessageDraft{{Role: apitypes.ThreadRoleAssistant, Content: answer.String()}}); err != nil {
			a.logger.Warn().Err(err).Str("agent_id", a.agentID).Msg("persist assistant message")
		}
	}
	_ = a.conns.WriteJSON(emitCtx, wstypes.Event{
		Type:    wstypes.EventTypeMessage,
		AgentID: a.agentID,
		Data:    wstypes.MessageEvent{ID: msgID, Content: answer.String()},
	})
}

// streamAnswer relays one streamed answer piece by piece and
// accumulates the text for the final message event.
func (a *UserAgent) streamAnswer(ctx context.Context, stream *schema.StreamReader[*schema.Message], msgID string, answer *strings.Builder) error {
	defer stream.Close()
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if chunk == nil || chunk.Content == "" {
			continue
		}
		answer.WriteString(chunk.Content)
		_ = a.conns.WriteJSON(ctx, wstypes.Event{
			Type:    wstypes.EventTypeMessageChunk,
			AgentID: a.agentID,
			Data:    wstypes.MessageChunkEvent{ID: msgID, Delta: chunk.Content},
		})
	}
}

// emitError reports a failed turn to the lane's connections.
func (a *UserAgent) emitError(ctx context.Context, message string) {
	_ = a.conns.WriteJSON(ctx, wstypes.Event{
		Type:    wstypes.EventTypeError,
		AgentID: a.agentID,
		Data:    wstypes.ErrorEvent{Message: message},
	})
}

func (a *UserAgent) GetStatus() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	modelID := ""
	if a.model != nil {
		modelID = a.model.ID
	}

	return map[string]interface{}{
		"agent_id":   a.agentID,
		"queue_len":  len(a.MsgCh),
		"active":     a.ctx.Err() == nil,
		"model":      modelID,
		"conn_count": a.conns.Count(),
	}
}
