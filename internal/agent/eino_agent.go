package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type EinoEventType string

const (
	EinoEventTypeText  EinoEventType = "text"
	EinoEventTypeTool  EinoEventType = "tool"
	EinoEventTypeEnd   EinoEventType = "end"
	EinoEventTypeError EinoEventType = "error"
)

type EinoEvent struct {
	Type       EinoEventType
	Content    string
	ToolName   string
	ToolArgs   map[string]any
	ToolResult string
	Error      error
}

type EinoRunResult struct {
	Content string
}

type EinoAgent interface {
	Query(ctx context.Context, input string, opts ...interface{}) (EinoRunResult, error)
}

type EinoRunner interface {
	Query(ctx context.Context, input string, handler interface{}) (<-chan EinoEvent, error)
}

type EinoAgentManager interface {
	Evict(agentID string)
}

type HistorySaver interface {
	SaveHistory(ctx context.Context, sessionID string, history []AgentMessage) error
}

type Notifier interface {
	Notify(userID, agentID, msgID, message string) error
}

type Mem0Client interface {
	GetMemories(ctx context.Context, userID string) ([]string, error)
}

type QuotaMiddlewareAdapter interface {
	BeforeLLM(ctx context.Context, session interface{}) error
	AfterLLM(ctx context.Context, session interface{}, promptTokens, completionTokens int)
}

type EinoAgentOptions struct {
	AgentID      string
	UserID       string
	SystemPrompt string
	Runner       EinoRunner
	Manager      EinoAgentManager
	HistorySaver HistorySaver
	Notifier     Notifier
	Mem0Client   Mem0Client
	Quota        QuotaMiddlewareAdapter
	IdleTimeout  time.Duration
}

type QueueStats struct {
	Queued   int
	Running  bool
	Position int
}

type EinoAgentImpl struct {
	agentID          string
	userID           string
	ctx              context.Context
	cancel           context.CancelFunc
	laneQueue        chan Task
	runner           EinoRunner
	manager          EinoAgentManager
	historySaver     HistorySaver
	notifier         Notifier
	mem0Client       Mem0Client
	systemPrompt     string
	goroutineStarted bool
	mu               sync.Mutex
	idleTimeout      time.Duration
	buffer           []AgentMessage
	bufferMu         sync.Mutex
}

func NewEinoAgent(opts EinoAgentOptions) *EinoAgentImpl {
	ctx, cancel := context.WithCancel(context.Background())

	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}

	return &EinoAgentImpl{
		agentID:      opts.AgentID,
		userID:       opts.UserID,
		ctx:          ctx,
		cancel:       cancel,
		laneQueue:    make(chan Task, defaultLaneQueueSize),
		runner:       opts.Runner,
		manager:      opts.Manager,
		historySaver: opts.HistorySaver,
		notifier:     opts.Notifier,
		mem0Client:   opts.Mem0Client,
		systemPrompt: opts.SystemPrompt,
		idleTimeout:  idleTimeout,
		buffer:       make([]AgentMessage, 0),
	}
}

func (a *EinoAgentImpl) GetAgentID() string {
	return a.agentID
}

func (a *EinoAgentImpl) GetUserID() string {
	return a.userID
}

func (a *EinoAgentImpl) GetContext() context.Context {
	return a.ctx
}

func (a *EinoAgentImpl) IsGoroutineStarted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.goroutineStarted
}

func (a *EinoAgentImpl) GetQueueStats() (int, QueueStats) {
	a.mu.Lock()
	defer a.mu.Unlock()

	queued := len(a.laneQueue)
	return queued, QueueStats{
		Queued:  queued,
		Running: a.goroutineStarted,
	}
}

func (a *EinoAgentImpl) Cancel() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *EinoAgentImpl) Submit(task Task) {
	a.maybeStartGoroutine()

	select {
	case a.laneQueue <- task:
	default:
	}
}

func (a *EinoAgentImpl) maybeStartGoroutine() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.goroutineStarted {
		return
	}

	a.goroutineStarted = true
	go a.runLoop()
}

func (a *EinoAgentImpl) runLoop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return

		case task := <-a.laneQueue:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(a.idleTimeout)

			messages := a.drainBufferedMessages(task)
			a.runTurn(messages)

		case <-idleTimer.C:
			if a.manager != nil {
				a.manager.Evict(a.agentID)
			}
			return
		}
	}
}

func (a *EinoAgentImpl) drainBufferedMessages(firstTask Task) []Task {
	tasks := []Task{firstTask}

	for {
		select {
		case task := <-a.laneQueue:
			tasks = append(tasks, task)
		default:
			return tasks
		}
	}
}

func (a *EinoAgentImpl) runTurn(tasks []Task) {
	if a.runner == nil {
		return
	}

	var conn WSConn
	var msgID string
	var combinedMessage string

	for _, task := range tasks {
		if conn == nil && task.Conn != nil {
			conn = task.Conn
		}
		if msgID == "" {
			msgID = task.MsgID
		}
		if combinedMessage != "" {
			combinedMessage += "\n"
		}
		combinedMessage += task.Message
	}

	if combinedMessage == "" {
		return
	}

	progressHandler := BuildProgressHandler(conn)

	eventCh, err := a.runner.Query(a.ctx, combinedMessage, progressHandler)
	if err != nil {
		return
	}

	var responseBuffer strings.Builder
	connAlive := conn != nil

	for event := range eventCh {
		switch event.Type {
		case EinoEventTypeText:
			responseBuffer.WriteString(event.Content)
			if connAlive && conn != nil {
				if err := conn.WriteJSON(ProgressEvent{
					Type:    "text_delta",
					Payload: map[string]interface{}{"delta": event.Content},
				}); err != nil {
					connAlive = false
					conn = nil
				}
			}
		case EinoEventTypeTool:
			if connAlive && conn != nil {
				if err := conn.WriteJSON(ProgressEvent{
					Type: "tool_start",
					Payload: map[string]interface{}{
						"tool_name": event.ToolName,
						"args":      event.ToolArgs,
					},
				}); err != nil {
					connAlive = false
					conn = nil
				}
			}
		case EinoEventTypeError:
			if event.Error != nil {
				responseBuffer.WriteString(fmt.Sprintf("Error: %v", event.Error))
			}
		}
	}

	responseText := responseBuffer.String()
	assistantMsg := NewTextMessage(RoleAssistant, responseText)

	a.bufferMu.Lock()
	a.buffer = append(a.buffer, assistantMsg)
	a.bufferMu.Unlock()

	if a.historySaver != nil {
		history := make([]AgentMessage, 0)
		for _, task := range tasks {
			history = append(history, NewTextMessage(RoleUser, task.Message))
		}
		history = append(history, assistantMsg)
		_ = a.historySaver.SaveHistory(a.ctx, a.agentID, history)
	}

	if !connAlive && a.notifier != nil && msgID != "" {
		_ = a.notifier.Notify(a.userID, a.agentID, msgID, responseText)
	}
}

func (a *EinoAgentImpl) BuildSystemPrompt(memories []string) string {
	prompt := a.systemPrompt

	if len(memories) > 0 {
		prompt += "\n\nRelevant memories:\n"
		for _, mem := range memories {
			prompt += "- " + mem + "\n"
		}
	}

	return prompt
}
