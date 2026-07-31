package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radch-ai/ava/services/gateway/internal/agent/tools"
	"github.com/radch-ai/ava/services/gateway/internal/wsconn"
	"github.com/rs/zerolog"
)

const (
	defaultIdleTimeout = 5 * time.Minute
)

// Message represents a message submitted to an agent.
type Message struct {
	ID      string
	Content string
	Model   string // Model to use, passed to Bifrost (e.g., "auto", "gpt-4o")
}

type UserAgentOptions struct {
	AgentID     string
	UserID      string
	Conn        *wsconn.Wrapper
	Model       string       // Model to use, required (e.g., "auto", "gpt-4o")
	MsgCh       chan Message // passed from UserAgentManager
	IdleTimeout time.Duration
	OnExit      func()

	// Bifrost configuration
	BifrostBaseURL string // Bifrost gateway URL (from BIFROST_BASEURL env)

	// Database pool for loading user config (virtual keys)
	DBPool *pgxpool.Pool
	Logger zerolog.Logger
}

type UserAgent struct {
	agentID string
	userID  string
	ctx     context.Context
	cancel  context.CancelFunc

	MsgCh chan Message // owned by UserAgentManager, passed via options
	mu    sync.Mutex

	queryCtx    context.Context
	queryCancel context.CancelFunc

	conn *wsconn.Wrapper

	einoAgent  *adk.ChatModelAgent
	einoRunner *adk.Runner

	idleTimeout time.Duration
	onExit      func()

	// Bifrost configuration
	bifrostBaseURL string

	// Database pool for loading user config
	dbPool *pgxpool.Pool
	logger zerolog.Logger

	// Virtual keys (loaded from database)
	subscriptionVK string // Rate-limited VK, resets monthly with plan
	topupVK        string // Budget-limited VK, drawn down as they spend

	// Current model (updated via updateStats)
	model string
}

// userConfig holds user-specific configuration loaded from the database.
type userConfig struct {
	SubscriptionVK string // Rate-limited VK, rate-limited x hourly, resets monthly with plan
	TopupVK        string // Budget-limited VK, drawn down as they spend
}

// loadUserConfig fetches user configuration including virtual keys.
//
// Expected database schema:
//
//	CREATE TABLE users (
//	    user_id         TEXT PRIMARY KEY,
//	    subscription_vk TEXT,  -- rate-limited VK, resets monthly with plan
//	    topup_vk        TEXT   -- budget-limited VK, drawn down as they spend
//	);
func (a *UserAgent) loadUserConfig(ctx context.Context) error {
	if a.dbPool == nil {
		return nil
	}

	cfg := &userConfig{}
	query := `
		SELECT subscription_vk, topup_vk
		FROM users
		WHERE user_id = $1
	`
	err := a.dbPool.QueryRow(ctx, query, a.userID).Scan(&cfg.SubscriptionVK, &cfg.TopupVK)
	if err != nil {
		a.logger.Warn().Err(err).Str("user_id", a.userID).Msg("failed to load user config")
		return err
	}

	a.subscriptionVK = cfg.SubscriptionVK
	a.topupVK = cfg.TopupVK
	return nil
}

// newUserAgent creates a new agent, initializes it with the LLM model if provided,
// and starts its message processing loop. The returned agent is fully ready to use.
func newUserAgent(ctx context.Context, opts UserAgentOptions) *UserAgent {
	ctx, cancel := context.WithCancel(ctx)

	if opts.MsgCh == nil {
		panic("MsgCh is required")
	}

	a := &UserAgent{
		ctx:    ctx,
		cancel: cancel,

		agentID: opts.AgentID,
		userID:  opts.UserID,

		conn:  opts.Conn,
		model: opts.Model,
		MsgCh: opts.MsgCh,

		idleTimeout: opts.IdleTimeout,

		bifrostBaseURL: opts.BifrostBaseURL,
		dbPool:         opts.DBPool,
		logger:         opts.Logger,

		onExit: opts.OnExit,
	}

	if a.idleTimeout == 0 {
		a.idleTimeout = defaultIdleTimeout
	}

	// Load user config (virtual keys) from database
	if err := a.loadUserConfig(ctx); err != nil {
		a.logger.Warn().Err(err).Str("user_id", a.userID).Msg("agent will work without VK")
	}

	// Initialize LLM agent if Bifrost is configured with a virtual key
	vk := a.getVirtualKey()
	if a.bifrostBaseURL != "" && vk != "" {
		chatModel, err := a.createChatModel(ctx, a.model, vk)
		if err == nil {
			globalTools := tools.GetGlobalTools()

			a.einoAgent, _ = adk.NewChatModelAgent(a.ctx, &adk.ChatModelAgentConfig{
				Name:  a.agentID,
				Model: chatModel,
				ToolsConfig: adk.ToolsConfig{
					ToolsNodeConfig: compose.ToolsNodeConfig{
						Tools: globalTools,
					},
				},
			})

			if a.einoAgent != nil {
				a.einoRunner = adk.NewRunner(a.ctx, adk.RunnerConfig{
					Agent: a.einoAgent,
				})
			}
		}
	}

	// Start the message processing loop
	go a.loop()

	return a
}

// getVirtualKey returns the preferred virtual key.
// Prefers subscription VK, falls back to topup VK.
func (a *UserAgent) getVirtualKey() string {
	if a.subscriptionVK != "" {
		return a.subscriptionVK
	}
	return a.topupVK
}

// createChatModel creates a new ChatModel connected to Bifrost.
func (a *UserAgent) createChatModel(ctx context.Context, modelName, virtualKey string) (model.ToolCallingChatModel, error) {
	chatCfg := &openai.ChatModelConfig{
		BaseURL: a.bifrostBaseURL,
		APIKey:  virtualKey,
		Model:   modelName,
	}

	return openai.NewChatModel(ctx, chatCfg)
}

// Interrupt cancels the current LLM query mid-stream.
// The loop continues and will process buffered messages
//
// TODO: Add option to either:
//   - Interrupt current run only (buffered messages stay) - current behavior
//   - Interrupt and drain (discard buffered messages from MsgCh)
func (a *UserAgent) Interrupt() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.queryCancel != nil {
		a.queryCancel()
	}
}

// updateStats handles connection and model updates.
// Called by UserAgentManager when a message arrives for an existing agent.
// Always updates both connection and model to the latest values.
func (a *UserAgent) updateStats(ctx context.Context, conn *websocket.Conn, model string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Always update connection
	if a.conn != nil {
		a.conn.UpdateConn(conn)
	}

	// Always update model (will be used in next query via WithChatModelOptions)
	if model != "" {
		a.model = model
	}

	return nil
}

func (a *UserAgent) loop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case msg := <-a.MsgCh:
			idleTimer.Reset(a.idleTimeout)

			messages := a.drainMessages(msg)

			// Create fresh context for this query (can be cancelled by Interrupt)
			a.mu.Lock()
			a.queryCtx, a.queryCancel = context.WithCancel(a.ctx)
			a.mu.Unlock()

			a.processMessages(messages)

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

func (a *UserAgent) processMessages(messages []Message) {
	if len(messages) == 0 {
		return
	}

	var input strings.Builder
	var msgID string
	var modelOverride string

	for i, m := range messages {
		if i > 0 {
			input.WriteString("\n")
		}
		input.WriteString(m.Content)
		if msgID == "" && m.ID != "" {
			msgID = m.ID
		}
		// Use last non-empty model from messages
		if m.Model != "" {
			modelOverride = m.Model
		}
	}

	// Get current model (prefer message override, else use agent's current model)
	a.mu.Lock()
	queryCtx := a.queryCtx
	currentModel := a.model
	a.mu.Unlock()

	if modelOverride != "" {
		currentModel = modelOverride
	}

	if a.einoRunner != nil && queryCtx != nil {
		// Pass model override via WithChatModelOptions
		_ = a.einoRunner.Query(queryCtx, input.String(),
			adk.WithChatModelOptions([]model.Option{
				model.WithModel(currentModel),
			}),
		)
	}

	if a.conn == nil || a.conn.Conn() == nil {
		a.notifyUser(msgID, input.String())
	}
}

func (a *UserAgent) GetStatus() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	return map[string]interface{}{
		"agent_id":  a.agentID,
		"user_id":   a.userID,
		"queue_len": len(a.MsgCh),
		"active":    a.ctx.Err() == nil,
		"model":     a.model,
	}
}

func (a *UserAgent) notifyUser(msgID, content string) {
}
