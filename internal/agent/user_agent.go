package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/coder/websocket"
	"github.com/nevaria-ai/gateway/internal/agent/tools"
	"github.com/nevaria-ai/gateway/internal/wsconn"
)

const (
	defaultIdleTimeout = 5 * time.Minute
)

// Message represents a message submitted to an agent.
type Message struct {
	ID      string
	Content string
}

type QuotaChatModelInterface interface {
	GetInnerModel() model.ToolCallingChatModel
}

type UserAgentOptions struct {
	AgentID           string
	UserID            string
	Description       string
	SystemPrompt      string
	QuotaChatModel    QuotaChatModelInterface
	Memories          []string
	IdleTimeout       time.Duration
	Conn              *wsconn.Wrapper
	LaneCh            chan Message // passed from AgentManager
	ProcessMessagesFn func([]Message)
	OnExit            func()
}

type UserAgent struct {
	agentID string
	userID  string
	ctx     context.Context
	cancel  context.CancelFunc

	laneCh chan Message // owned by AgentManager, passed via options
	mu     sync.Mutex

	queryCtx    context.Context
	queryCancel context.CancelFunc

	conn *wsconn.Wrapper

	einoAgent  *adk.ChatModelAgent
	einoRunner *adk.Runner

	idleTimeout       time.Duration
	processMessagesFn func([]Message)
	onExit            func()
}

// NewUserAgent creates a new agent, initializes it with the LLM model if provided,
// and starts its message processing loop. The returned agent is fully ready to use.
func NewUserAgent(ctx context.Context, opts UserAgentOptions) *UserAgent {
	ctx, cancel := context.WithCancel(ctx)

	if opts.LaneCh == nil {
		panic("LaneCh is required")
	}

	a := &UserAgent{
		agentID:           opts.AgentID,
		userID:            opts.UserID,
		ctx:               ctx,
		cancel:            cancel,
		laneCh:            opts.LaneCh,
		conn:              opts.Conn,
		idleTimeout:       opts.IdleTimeout,
		processMessagesFn: opts.ProcessMessagesFn,
		onExit:            opts.OnExit,
	}

	if a.idleTimeout == 0 {
		a.idleTimeout = defaultIdleTimeout
	}

	// Initialize LLM agent if model is provided
	if opts.QuotaChatModel != nil {
		chatModel := opts.QuotaChatModel.GetInnerModel()
		globalTools := tools.GetGlobalTools()

		a.einoAgent, _ = adk.NewChatModelAgent(a.ctx, &adk.ChatModelAgentConfig{
			Name:        a.agentID,
			Description: opts.Description,
			Instruction: buildInstruction(opts.SystemPrompt, opts.Memories),
			Model:       chatModel,
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

	// Start the message processing loop
	go a.loop()

	return a
}

// Interrupt cancels the current LLM query mid-stream.
// The loop continues and will process buffered messages
//
// TODO: Add option to either:
//   - Interrupt current run only (buffered messages stay) - current behavior
//   - Interrupt and drain (discard buffered messages from laneCh)
func (a *UserAgent) Interrupt() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.queryCancel != nil {
		a.queryCancel()
	}
}

func (a *UserAgent) UpdateConn(conn *websocket.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn != nil {
		a.conn.UpdateConn(conn)
	}
}

func buildInstruction(systemPrompt string, memories []string) string {
	instruction := systemPrompt
	if len(memories) > 0 {
		instruction += "\n\nContext about the user:\n"
		for _, mem := range memories {
			instruction += "- " + mem + "\n"
		}
	}
	return instruction
}

func (a *UserAgent) loop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case msg := <-a.laneCh:
			idleTimer.Reset(a.idleTimeout)

			messages := a.drainMessages(msg)

			// Create fresh context for this query (can be cancelled by Pause)
			a.mu.Lock()
			a.queryCtx, a.queryCancel = context.WithCancel(a.ctx)
			a.mu.Unlock()

			if a.processMessagesFn != nil {
				a.processMessagesFn(messages)
			} else {
				a.processMessages(messages)
			}

		case <-idleTimer.C:
			a.mu.Lock()
			if len(a.laneCh) == 0 {
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
		case m := <-a.laneCh:
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

	for i, m := range messages {
		if i > 0 {
			input.WriteString("\n")
		}
		input.WriteString(m.Content)
		if msgID == "" && m.ID != "" {
			msgID = m.ID
		}
	}

	a.mu.Lock()
	queryCtx := a.queryCtx
	a.mu.Unlock()

	if a.einoRunner != nil && queryCtx != nil {
		_ = a.einoRunner.Query(queryCtx, input.String())
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
		"queue_len": len(a.laneCh),
		"active":    a.ctx.Err() == nil,
	}
}

func (a *UserAgent) notifyUser(msgID, content string) {
}
