package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	duckduckgo "github.com/cloudwego/eino-ext/components/tool/duckduckgo"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/coder/websocket"
	"github.com/nevaria-ai/gateway/internal/wsconn"
)

const (
	defaultIdleTimeout   = 5 * time.Minute
	defaultLaneQueueSize = 100
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
	AgentID        string
	UserID         string
	Description    string
	SystemPrompt   string
	QuotaChatModel QuotaChatModelInterface
	Memories       []string
	IdleTimeout    time.Duration
	Conn           *wsconn.Wrapper
}

type UserAgent struct {
	agentID string
	userID  string
	ctx     context.Context
	cancel  context.CancelFunc

	laneCh chan Message
	mu     sync.Mutex

	conn *wsconn.Wrapper

	einoAgent  *adk.ChatModelAgent
	einoRunner *adk.Runner

	idleTimeout       time.Duration
	processMessagesFn func([]Message)
	onExit            func()
}

func NewUserAgent(ctx context.Context, firstMessage Message, opts UserAgentOptions) *UserAgent {
	ctx, cancel := context.WithCancel(ctx)

	a := &UserAgent{
		agentID:     opts.AgentID,
		userID:      opts.UserID,
		ctx:         ctx,
		cancel:      cancel,
		laneCh:      make(chan Message, defaultLaneQueueSize),
		conn:        opts.Conn,
		idleTimeout: opts.IdleTimeout,
	}

	if a.idleTimeout == 0 {
		a.idleTimeout = defaultIdleTimeout
	}

	a.laneCh <- firstMessage

	return a
}

func (a *UserAgent) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *UserAgent) GetContext() context.Context {
	return a.ctx
}

func (a *UserAgent) GetAgentID() string {
	return a.agentID
}

func (a *UserAgent) GetUserID() string {
	return a.userID
}

func (a *UserAgent) Submit(msg Message) {
	a.laneCh <- msg
}

func (a *UserAgent) GetConn() *wsconn.Wrapper {
	return a.conn
}

func (a *UserAgent) UpdateConn(conn *websocket.Conn) {
	if a.conn != nil {
		a.conn.UpdateConn(conn)
	}
}

func (a *UserAgent) SetProcessMessagesFn(fn func([]Message)) {
	a.processMessagesFn = fn
}

func (a *UserAgent) SetOnExit(fn func()) {
	a.onExit = fn
}

func (a *UserAgent) Start(opts UserAgentOptions) error {
	go a.loop()

	return nil
}

func (a *UserAgent) StartWithModel(opts UserAgentOptions) error {
	if opts.QuotaChatModel == nil {
		go a.loop()
		return nil
	}

	chatModel := opts.QuotaChatModel.GetInnerModel()

	tools := GetGlobalTools()

	var err error
	a.einoAgent, err = adk.NewChatModelAgent(a.ctx, &adk.ChatModelAgentConfig{
		Name:        a.agentID,
		Description: opts.Description,
		Instruction: buildInstruction(opts.SystemPrompt, opts.Memories),
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
	})
	if err != nil {
		return err
	}

	a.einoRunner = adk.NewRunner(a.ctx, adk.RunnerConfig{
		Agent: a.einoAgent,
	})

	go a.loop()

	return nil
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

func GetGlobalTools() []tool.BaseTool {
	ctx := context.Background()
	ddgTool, err := duckduckgo.NewTool(ctx, &duckduckgo.Config{}) //nolint:staticcheck // NewTextSearchTool not yet available
	if err != nil {
		return []tool.BaseTool{}
	}
	return []tool.BaseTool{ddgTool}
}

func (a *UserAgent) loop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case msg := <-a.laneCh:
			idleTimer.Reset(a.idleTimeout)

			messages := a.drainMessages(msg)

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

	if a.einoRunner != nil {
		_ = a.einoRunner.Query(a.ctx, input.String())
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
