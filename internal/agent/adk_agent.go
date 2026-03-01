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
)

type QuotaChatModelInterface interface {
	GetInnerModel() model.ToolCallingChatModel
}

type ADKAgentOptions struct {
	AgentID        string
	UserID         string
	Description    string
	SystemPrompt   string
	QuotaChatModel QuotaChatModelInterface
	Memories       []string
	IdleTimeout    time.Duration
}

type ADKAgent struct {
	agentID string
	userID  string
	ctx     context.Context
	cancel  context.CancelFunc

	laneCh chan Task
	mu     sync.Mutex

	connRef *WSConnRef

	einoAgent  *adk.ChatModelAgent
	einoRunner *adk.Runner

	idleTimeout    time.Duration
	processTasksFn func([]Task)
	onExit         func()
}

func NewADKAgent(ctx context.Context, firstTask Task, opts ADKAgentOptions) *ADKAgent {
	ctx, cancel := context.WithCancel(ctx)

	a := &ADKAgent{
		agentID:     opts.AgentID,
		userID:      opts.UserID,
		ctx:         ctx,
		cancel:      cancel,
		laneCh:      make(chan Task, defaultLaneQueueSize),
		connRef:     NewWSConnRef(),
		idleTimeout: opts.IdleTimeout,
	}

	if a.idleTimeout == 0 {
		a.idleTimeout = defaultIdleTimeout
	}

	a.laneCh <- firstTask

	return a
}

func (a *ADKAgent) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *ADKAgent) GetContext() context.Context {
	return a.ctx
}

func (a *ADKAgent) GetAgentID() string {
	return a.agentID
}

func (a *ADKAgent) GetUserID() string {
	return a.userID
}

func (a *ADKAgent) Submit(task Task) {
	a.laneCh <- task
}

func (a *ADKAgent) GetConnRef() *WSConnRef {
	return a.connRef
}

func (a *ADKAgent) SetProcessTasksFn(fn func([]Task)) {
	a.processTasksFn = fn
}

func (a *ADKAgent) SetOnExit(fn func()) {
	a.onExit = fn
}

func (a *ADKAgent) Start(opts ADKAgentOptions) error {
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

func (a *ADKAgent) loop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case task := <-a.laneCh:
			idleTimer.Reset(a.idleTimeout)

			tasks := a.drainMessages(task)

			if a.processTasksFn != nil {
				a.processTasksFn(tasks)
			} else {
				a.processTasks(tasks)
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

func (a *ADKAgent) drainMessages(first Task) []Task {
	tasks := []Task{first}

	for {
		select {
		case t := <-a.laneCh:
			tasks = append(tasks, t)
		default:
			return tasks
		}
	}
}

func (a *ADKAgent) processTasks(tasks []Task) {
	if len(tasks) == 0 {
		return
	}

	var input strings.Builder
	var msgID string

	for i, t := range tasks {
		if i > 0 {
			input.WriteString("\n")
		}
		input.WriteString(t.Message)
		if msgID == "" && t.MsgID != "" {
			msgID = t.MsgID
		}
	}

	conn := a.connRef.Get()
	_ = BuildProgressHandler(conn)

	if a.einoRunner != nil {
		_ = a.einoRunner.Query(a.ctx, input.String())
	}

	a.saveToSession(msgID, input.String())

	if !a.connRef.IsSet() {
		a.notifyUser(msgID, input.String())
	}
}

func (a *ADKAgent) saveToSession(msgID, content string) {
}

func (a *ADKAgent) notifyUser(msgID, content string) {
}
