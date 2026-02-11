# LLM Agent Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Integrate LLM clients into Agent so it can call real AI models instead of using mock responses.

**Architecture:**
1. Add `LLMClient` interface to `AgentOptions` and `Agent` struct
2. Replace mock `runToolCallingLoop()` with real LLM streaming via `llmClient.Stream()`
3. Agent manages tool execution internally while delegating LLM calls to injected client
4. Preserve all existing event emission behavior

**Tech Stack:**
- Go 1.24
- Existing `internal/llm` package with official SDKs
- Existing `internal/agent` package

---

## Implementation Status (Before This Plan)

**Completed:**
- ✅ All 3 LLM clients with official SDKs (OpenAI, Anthropic, Google)
- ✅ Factory pattern with auto-detection from model names
- ✅ Tool format conversion for all providers
- ✅ Streaming with event emission
- ✅ All tests passing (agent: 89.2%, llm: passing)

**Left Off:**
- ❌ Agent still uses mock `runToolCallingLoop()` returning "Simulated response to: ..."
- ❌ Agent has no LLM client dependency
- ❌ No real AI responses in agent

---

## Task 1: Add LLMClient Interface to AgentOptions

**Files:**
- Modify: `internal/agent/agent.go:65-70`

**Step 1: Write the failing test**

```go
// Add to internal/agent/agent_test.go

func TestNewAgentWithLLMClient(t *testing.T) {
    mockClient := &MockLLMClient{}
    agent := NewAgent(AgentOptions{
        SystemPrompt: "You are helpful",
        LLMClient:   mockClient,
    })

    if agent.llmClient == nil {
        t.Error("Expected llmClient to be set, got nil")
    }

    if agent.llmClient != mockClient {
        t.Error("Expected llmClient to be the mock instance")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run TestNewAgentWithLLMClient -v`
Expected: FAIL with "unknown field llmClient"

**Step 3: Write minimal implementation**

Modify `internal/agent/agent.go`:

```go
// AgentOptions configures a new Agent.
type AgentOptions struct {
    ID           string
    SystemPrompt string
    SessionID    string
    LLMClient    LLMClient // <-- ADD THIS FIELD
}

// Agent manages agent state with thread-safe access and event broadcasting.
type Agent struct {
    mu            sync.RWMutex
    state         AgentState
    id            string
    subscribers   map[string]chan AgentEvent
    subscribersMu sync.Mutex
    ctx           context.Context
    cancel        context.CancelFunc
    llmClient     LLMClient // <-- ADD THIS FIELD
}
```

**Step 4: Update NewAgent to set LLMClient**

Modify `internal/agent/agent.go:85-105`:

```go
// NewAgent creates a new Agent with the given options.
// If no ID is provided, a UUID will be generated.
func NewAgent(opts AgentOptions) *Agent {
    id := opts.ID
    if id == "" {
        id = uuid.New().String()
    }

    ctx, cancel := context.WithCancel(context.Background())

    state := NewAgentState(AgentStateOptions{
        SystemPrompt: opts.SystemPrompt,
        SessionID:    opts.SessionID,
    })

    return &Agent{
        id:          id,
        state:       state,
        subscribers: make(map[string]chan AgentEvent),
        ctx:         ctx,
        cancel:      cancel,
        llmClient:   opts.LLMClient, // <-- ADD THIS LINE
    }
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/agent -run TestNewAgentWithLLMClient -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): add LLMClient field to AgentOptions and Agent"
```

---

## Task 2: Define LLMClient Interface in Agent Package

**Files:**
- Create: `internal/agent/client.go`

**Step 1: Write the failing test**

```go
// Create internal/agent/client_test.go

package agent

import (
    "context"
    "testing"
)

func TestLLMClientInterface(t *testing.T) {
    // Verify interface exists and has correct method signature
    var _ LLMClient = (*MockLLMClient)(nil)

    ctx := context.Background()
    mockClient := &MockLLMClient{
        StreamFunc: func(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan StreamEvent, error) {
            ch := make(chan StreamEvent)
            close(ch)
            return ch, nil
        },
    }

    ch, err := mockClient.Stream(ctx, []AgentMessage{}, []Tool{})
    if err != nil {
        t.Fatalf("Stream returned error: %v", err)
    }

    if ch == nil {
        t.Error("Stream returned nil channel")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run TestLLMClientInterface -v`
Expected: FAIL with "undefined type LLMClient"

**Step 3: Write minimal implementation**

Create `internal/agent/client.go`:

```go
package agent

import "context"

// StreamEvent represents a single event from LLM streaming response.
type StreamEvent struct {
    // Type indicates the event kind: "message_start", "content_block_delta", "tool_use", etc.
    Type string

    // Content is the text content for text_delta events
    Content string

    // ToolCall contains tool call information for tool_use events
    ToolCall *ToolCall

    // Done indicates streaming is complete
    Done bool

    // Error contains any error that occurred
    Error error
}

// LLMClient defines the interface for LLM providers.
// The agent uses this interface to call different LLM implementations (OpenAI, Anthropic, Google).
type LLMClient interface {
    // Stream sends messages to the LLM and returns a channel of stream events.
    // The caller must consume the channel until it's closed.
    // The channel emits events as they arrive from the LLM provider.
    Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan StreamEvent, error)
}
```

**Step 4: Update mock.go to implement LLMClient interface**

Modify `internal/agent/mock.go:30-50`:

```go
// MockLLMClient is a mock LLM client for testing.
type MockLLMClient struct {
    // StreamFunc is the function to call when Stream is invoked
    StreamFunc func(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan StreamEvent, error)

    // ResponseText is the simple text response to return (used if StreamFunc is nil)
    ResponseText string

    // ResponseToolCalls are tool calls to return (used if StreamFunc is nil)
    ResponseToolCalls []ToolCall
}

// Stream sends mock responses to the LLM stream.
func (m *MockLLMClient) Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan StreamEvent, error) {
    if m.StreamFunc != nil {
        return m.StreamFunc(ctx, messages, tools)
    }

    // Default mock behavior: return simple text response
    ch := make(chan StreamEvent, 1)
    go func() {
        defer close(ch)

        // Emit text response
        if m.ResponseText != "" {
            ch <- StreamEvent{Type: "content", Content: m.ResponseText}
        }

        // Emit tool calls if present
        for _, tc := range m.ResponseToolCalls {
            ch <- StreamEvent{Type: "tool_use", ToolCall: &tc}
        }

        ch <- StreamEvent{Done: true}
    }()

    return ch, nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/agent -run TestLLMClientInterface -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/agent/client.go internal/agent/client_test.go internal/agent/mock.go
git commit -m "feat(agent): define LLMClient interface and update mock"
```

---

## Task 3: Replace Mock Loop with Real LLM Streaming

**Files:**
- Modify: `internal/agent/agent.go:193-260` (Prompt method and runToolCallingLoop)

**Step 1: Write the failing test**

```go
// Add to internal/agent/agent_prompt_test.go

func TestAgentPromptWithLLMClient(t *testing.T) {
    ctx := context.Background()

    mockClient := &MockLLMClient{
        ResponseText: "Hello from real LLM!",
    }

    agent := NewAgent(AgentOptions{
        SystemPrompt: "You are helpful",
        LLMClient:   mockClient,
    })

    // Add a user message
    state := agent.State()
    state.AddMessage(NewTextMessage(RoleUser, "Hello"))

    // Subscribe to events
    _, events := agent.Subscribe()
    defer agent.Unsubscribe(events)

    // Prompt should use LLM client, not mock loop
    go agent.Prompt(ctx)

    // Collect message events
    var gotContent string
    for event := range events {
        if e, ok := event.(MessageUpdateEvent); ok && e.ContentDelta != "" {
            gotContent = e.ContentDelta
            break
        }
    }

    if gotContent != "Hello from real LLM!" {
        t.Errorf("Expected 'Hello from real LLM!', got '%s'", gotContent)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run TestAgentPromptWithLLMClient -v`
Expected: FAIL or timeout (still uses mock loop)

**Step 3: Replace runToolCallingLoop with real LLM streaming**

Modify `internal/agent/agent.go`:

First, remove the mock `runToolCallingLoop` function (lines 218-252).

Then update `Prompt()` method:

```go
// Prompt sends a user message to the agent and triggers LLM streaming.
// The agent emits events as the LLM streams responses and executes tools.
func (a *Agent) Prompt(ctx context.Context) error {
    // Emit turn start event
    a.emit(TurnStartEvent{AgentID: a.id})

    defer func() {
        // Emit turn end event
        a.emit(TurnEndEvent{AgentID: a.id})
    }()

    // Check if LLM client is available
    a.mu.RLock()
    hasLLM := a.llmClient != nil
    messages := a.state.Messages
    tools := a.state.Tools
    a.mu.RUnlock()

    if !hasLLM {
        // Fallback to mock behavior for backward compatibility
        return a.runMockLoop(ctx)
    }

    // Call LLM stream
    stream, err := a.llmClient.Stream(ctx, messages, tools)
    if err != nil {
        return err
    }

    // Process stream events
    var currentMessage *AgentMessage
    var currentToolCalls []ToolCallContent

    for event := range stream {
        if event.Error != nil {
            return event.Error
        }

        switch event.Type {
        case "message_start":
            a.emit(MessageStartEvent{AgentID: a.id})
            currentMessage = &AgentMessage{
                Role:    RoleAssistant,
                Content: []ContentBlock{},
            }

        case "content":
            // Text delta from LLM
            a.emit(MessageUpdateEvent{
                AgentID:      a.id,
                ContentDelta: event.Content,
            })

            // Append to current message
            if currentMessage != nil {
                currentMessage.Content = append(currentMessage.Content,
                    TextContent{Text: event.Content})
            }

        case "tool_use":
            // Tool call from LLM
            tc := event.ToolCall
            if tc != nil {
                currentToolCalls = append(currentToolCalls, *tc)
                a.emit(ToolExecutionStartEvent{
                    AgentID:  a.id,
                    ToolName:  tc.ToolName,
                    ToolID:    tc.ToolCallID,
                })
            }

        case "done":
            // Streaming complete, finalize message
            if currentMessage != nil {
                // Add tool calls if any
                if len(currentToolCalls) > 0 {
                    currentMessage.Content = append(currentMessage.Content,
                        ToolCallContents{ToolCalls: currentToolCalls})
                }

                a.state.AddMessage(*currentMessage)
                a.emit(MessageEndEvent{AgentID: a.id})

                // Execute tools if we have tool calls
                if len(currentToolCalls) > 0 {
                    if err := a.executeTools(ctx, currentToolCalls); err != nil {
                        return err
                    }
                }
            }
        }
    }

    return nil
}

// runMockLoop provides fallback mock behavior for backward compatibility.
func (a *Agent) runMockLoop(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    a.mu.Lock()
    defer a.mu.Unlock()

    // Get last message (user message) for response
    lastMsg := a.state.Messages[len(a.state.Messages)-1]
    if len(lastMsg.Content) == 0 {
        return nil
    }

    // Extract text from user message for simulated response
    var userText string
    if tc, ok := lastMsg.Content[0].(TextContent); ok {
        userText = tc.Text
    } else {
        userText = "(message with non-text content)"
    }

    responseMsg := NewTextMessage(RoleAssistant, "Simulated response to: "+userText)
    a.state.AddMessage(responseMsg)

    return nil
}

// executeTools runs tool calls and adds their results to the conversation.
func (a *Agent) executeTools(ctx context.Context, toolCalls []ToolCallContent) error {
    for _, tc := range toolCalls {
        // Execute tool (uses existing ExecuteToolCall function)
        result, err := ExecuteToolCall(ctx, tc, a.state.Tools)
        if err != nil {
            a.emit(ToolExecutionErrorEvent{
                AgentID:  a.id,
                ToolName:  tc.ToolName,
                Error:     err,
            })
            // Add error result to conversation
            a.state.AddMessage(NewToolResultMessage(
                tc.ToolCallID,
                "",
                err,
            ))
            continue
        }

        a.emit(ToolExecutionUpdateEvent{
            AgentID:  a.id,
            ToolName:  tc.ToolName,
            Result:    result,
        })

        a.emit(ToolExecutionEndEvent{
            AgentID:  a.id,
            ToolName:  tc.ToolName,
        })

        // Add tool result to conversation
        a.state.AddMessage(NewToolResultMessage(
            tc.ToolCallID,
            result.Result,
            nil,
        ))
    }

    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent -run TestAgentPromptWithLLMClient -v`
Expected: PASS

**Step 5: Run all agent tests to ensure no regression**

Run: `go test ./internal/agent -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_prompt_test.go
git commit -m "feat(agent): replace mock loop with real LLM streaming"
```

---

## Task 4: Create LLM-to-Agent Adapter

**Files:**
- Create: `internal/llm/adapter.go`

**Step 1: Write the failing test**

```go
// Create internal/llm/adapter_test.go

package llm

import (
    "context"
    "testing"

    "github.com/nevaria-ai/gateway/internal/agent"
)

func TestNewAgentClient(t *testing.T) {
    cfg := Config{
        OpenAIAPIKey: "test-key",
    }

    client := NewAgentClient(cfg, "gpt-4o")

    if client == nil {
        t.Fatal("Expected non-nil client")
    }

    // Verify it implements agent.LLMClient
    var _ agent.LLMClient = client
}

func TestAgentClientStream(t *testing.T) {
    cfg := Config{
        OpenAIAPIKey: "test-key",
        OpenAIBaseURL: "https://api.openai.com/v1", // Use real base URL for mock
    }

    client := NewAgentClient(cfg, "gpt-4o")
    ctx := context.Background()

    messages := []agent.AgentMessage{
        agent.NewTextMessage(agent.RoleUser, "Hello"),
    }

    ch, err := client.Stream(ctx, messages, []agent.Tool{})
    if err != nil {
        t.Fatalf("Stream failed: %v", err)
    }

    if ch == nil {
        t.Fatal("Expected non-nil channel")
    }

    // Channel should be closed after streaming completes
    for range ch {
        // Consume events
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm -run TestAgentClient -v`
Expected: FAIL with "undefined: NewAgentClient"

**Step 3: Write minimal implementation**

Create `internal/llm/adapter.go`:

```go
package llm

import (
    "context"
   "fmt"

    "github.com/nevaria-ai/gateway/internal/agent"
)

// AgentClient adapts the LLM Client interface to the agent.LLMClient interface.
// It translates between agent message types and LLM client stream events.
type AgentClient struct {
    client *Client
    model  string
}

// NewAgentClient creates a new adapter for the agent.
func NewAgentClient(cfg Config, model string) *AgentClient {
    client := NewClient(cfg)
    return &AgentClient{
        client: client,
        model:  model,
    }
}

// Stream sends messages to the LLM and returns agent-compatible stream events.
func (a *AgentClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.StreamEvent, error) {
    // Call underlying LLM client
    llmStream, err := a.client.Stream(ctx, a.model, messages, tools)
    if err != nil {
        return nil, fmt.Errorf("LLM stream failed: %w", err)
    }

    // Adapt LLM stream events to agent stream events
    agentStream := make(chan agent.StreamEvent)

    go func() {
        defer close(agentStream)

        for llmEvent := range llmStream {
            agentEvent := agent.StreamEvent{
                Type:  llmEvent.Type,
                Done:  llmEvent.Done,
                Error: llmEvent.Error,
            }

            // Map content
            if llmEvent.ContentDelta != "" {
                agentEvent.Content = llmEvent.ContentDelta
            }

            // Map tool calls
            if llmEvent.ToolCall != nil {
                agentEvent.ToolCall = llmEvent.ToolCall
            }

            agentStream <- agentEvent

            if llmEvent.Done {
                break
            }
        }
    }()

    return agentStream, nil
}
```

**Note:** This adapter assumes the existing `Client.Stream()` signature. If it differs, adjust the mapping logic accordingly.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/llm -run TestAgentClient -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/adapter.go internal/llm/adapter_test.go
git commit -m "feat(llm): add AgentClient adapter for agent integration"
```

---

## Task 5: Update WebSocket Handler to Create Agent with LLM Client

**Files:**
- Modify: `internal/websocket/handler.go`
- Modify: `internal/websocket/agent_manager.go`

**Step 1: Check current agent creation**

```bash
grep -n "NewAgent" internal/websocket/agent_manager.go
```

**Step 2: Add LLM client configuration**

Modify `internal/websocket/handler.go` to accept LLM config:

```go
// Handler manages WebSocket connections.
type Handler struct {
    mu            sync.RWMutex
    conns         map[string]*connection
    maxConns      int
    activeConns    int
    agentManager   *AgentManager
    llmConfig     *llm.Config  // <-- ADD THIS FIELD
}

// NewHandler creates a new WebSocket handler.
func NewHandler(opts HandlerOptions) *Handler {
    return &Handler{
        conns:       make(map[string]*connection),
        maxConns:    opts.MaxConns,
        agentManager: NewAgentManager(),
        llmConfig:   opts.LLMConfig,  // <-- ADD THIS LINE
    }
}
```

**Step 3: Update agent creation to pass LLM client**

Modify `internal/websocket/agent_manager.go`:

```go
// AgentManager creates and manages per-connection agents.
type AgentManager struct {
    mu     sync.RWMutex
    agents map[string]*agent.Agent
    config *llm.Config  // <-- ADD THIS FIELD
}

// NewAgentManager creates a new agent manager.
func NewAgentManager(llmConfig *llm.Config) *AgentManager {  // <-- ADD PARAM
    return &AgentManager{
        agents: make(map[string]*agent.Agent),
        config: llmConfig,  // <-- ADD THIS LINE
    }
}

// GetOrCreateAgent gets an existing agent or creates a new one.
func (m *AgentManager) GetOrCreateAgent(connID string, sessionID string) *agent.Agent {
    m.mu.RLock()
    existingAgent, exists := m.agents[connID]
    m.mu.RUnlock()

    if exists {
        return existingAgent
    }

    // Create LLM client for this agent
    var llmClient agent.LLMClient
    if m.config != nil {
        // Get default model from environment or use sensible default
        model := os.Getenv("DEFAULT_LLM_MODEL")
        if model == "" {
            model = "gpt-4o" // Default to OpenAI
        }
        llmClient = llm.NewAgentClient(*m.config, model)
    }

    newAgent := agent.NewAgent(agent.AgentOptions{
        SystemPrompt: os.Getenv("SYSTEM_PROMPT"),
        SessionID:    sessionID,
        LLMClient:    llmClient,  // <-- PASS LLM CLIENT
    })

    m.mu.Lock()
    m.agents[connID] = newAgent
    m.mu.Unlock()

    return newAgent
}
```

**Step 4: Update HandlerOptions**

Modify `internal/websocket/handler.go` HandlerOptions struct:

```go
// HandlerOptions configures the WebSocket handler.
type HandlerOptions struct {
    MaxConns  int
    LLMConfig  *llm.Config  // <-- ADD THIS FIELD
}
```

**Step 5: Update server.go to pass LLM config**

Modify `internal/server/server.go`:

```go
// New creates a new server with the given configuration.
func New(cfg config.Config) *Server {
    // Create LLM config
    llmCfg := &llm.Config{
        OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
        AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),
        GoogleAPIKey:     os.Getenv("GOOGLE_API_KEY"),
    }
    llmCfg.ApplyDefaults()

    wsHandler := websocket.NewHandler(websocket.HandlerOptions{
        MaxConns: cfg.MaxConns,
        LLMConfig: llmCfg,  // <-- PASS LLM CONFIG
    })

    // ... rest of setup
}
```

**Step 6: Run tests**

Run: `go test ./internal/websocket -v`
Run: `go test ./internal/server -v`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/websocket/handler.go internal/websocket/agent_manager.go internal/server/server.go
git commit -m "feat(websocket): wire LLM client into agent creation"
```

---

## Task 6: Add Environment Variable Configuration

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Add LLM configuration to Config**

```go
// Config holds application configuration.
type Config struct {
    // Server configuration
    ServerAddr     string
    LogLevel       string
    AllowedOrigins []string
    MaxConns       int

    // LLM configuration <-- ADD THIS SECTION
    OpenAIAPIKey     string
    AnthropicAPIKey  string
    GoogleAPIKey      string
    DefaultModel      string
    SystemPrompt      string
}
```

**Step 2: Update Load() to read LLM env vars**

Modify `internal/config/config.go` Load function:

```go
// Load reads configuration from environment variables.
func Load() (Config, error) {
    cfg := Config{
        // Server config
        ServerAddr:     getEnv("GATEWAY_ADDR", ":8080"),
        LogLevel:       getEnv("GATEWAY_LOG_LEVEL", "info"),
        AllowedOrigins: strings.Split(getEnv("GATEWAY_ALLOWED_ORIGINS", ""), ","),
        MaxConns:       parseInt(getEnv("GATEWAY_MAX_CONNS", "0")),

        // LLM config <-- ADD THIS SECTION
        OpenAIAPIKey:     getEnv("OPENAI_API_KEY", ""),
        AnthropicAPIKey:  getEnv("ANTHROPIC_API_KEY", ""),
        GoogleAPIKey:      getEnv("GOOGLE_API_KEY", ""),
        DefaultModel:      getEnv("DEFAULT_LLM_MODEL", "gpt-4o"),
        SystemPrompt:      getEnv("SYSTEM_PROMPT", "You are a helpful assistant."),
    }

    return cfg, nil
}
```

**Step 3: Run tests**

Run: `go test ./internal/config -v`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add LLM configuration from environment"
```

---

## Task 7: End-to-End Integration Test

**Files:**
- Create: `internal/integration/llm_agent_test.go`

**Step 1: Write the integration test**

```go
// Create internal/integration/llm_agent_test.go

package integration

import (
    "context"
    "os"
    "testing"

    "github.com/nevaria-ai/gateway/internal/agent"
    "github.com/nevaria-ai/gateway/internal/agent/tools"
    "github.com/nevaria-ai/gateway/internal/config"
    "github.com/nevaria-ai/gateway/internal/llm"
)

func TestAgentLLMIntegration(t *testing.T) {
    // Skip if no API key is set
    if os.Getenv("OPENAI_API_KEY") == "" {
        t.Skip("OPENAI_API_KEY not set, skipping integration test")
    }

    ctx := context.Background()

    // Load config
    cfg, err := config.Load()
    if err != nil {
        t.Fatalf("Failed to load config: %v", err)
    }

    // Create LLM client
    llmClient := llm.NewAgentClient(llm.Config{
        OpenAIAPIKey: cfg.OpenAIAPIKey,
    }, cfg.DefaultModel)

    // Create agent with LLM client
    a := agent.NewAgent(agent.AgentOptions{
        SystemPrompt: cfg.SystemPrompt,
        LLMClient:   llmClient,
    })

    // Add a test tool
    state := a.State()
    state.Tools = []agent.Tool{
        tools.ExampleEchoTool(),
    }

    // Subscribe to events
    _, events := a.Subscribe()
    defer a.Unsubscribe(events)

    // Add user message
    state.AddMessage(agent.NewTextMessage(agent.RoleUser, "Say hello"))

    // Prompt the agent
    err = a.Prompt(ctx)
    if err != nil {
        t.Fatalf("Prompt failed: %v", err)
    }

    // Verify we got a response
    var gotResponse bool
    for event := range events {
        switch e := event.(type) {
        case agent.MessageUpdateEvent:
            if e.ContentDelta != "" {
                gotResponse = true
                t.Logf("Got response: %s", e.ContentDelta)
            }
        case agent.MessageEndEvent:
            // Message streaming complete
            goto Done
        case agent.TurnEndEvent:
            // Turn complete
            goto Done
        }
    }

Done:
    if !gotResponse {
        t.Error("No response received from LLM")
    }
}
```

**Step 2: Run test (with API key set)**

```bash
export OPENAI_API_KEY=sk-test-key
go test ./internal/integration -run TestAgentLLMIntegration -v
```

Expected: PASS (or SKIP if no API key)

**Step 3: Commit**

```bash
git add internal/integration/llm_agent_test.go
git commit -m "test(integration): add LLM agent integration test"
```

---

## Task 8: Update Documentation

**Files:**
- Modify: `docs/agent/agent.md`
- Modify: `docs/llm/README.md`

**Step 1: Update agent.md with LLM usage**

Add to `docs/agent/agent.md`:

```markdown
## Using with LLM Clients

The agent can be configured with an LLM client to enable real AI responses:

```go
import (
    "github.com/nevaria-ai/gateway/internal/agent"
    "github.com/nevaria-ai/gateway/internal/llm"
)

// Create LLM client
llmClient := llm.NewAgentClient(llm.Config{
    OpenAIAPIKey: os.Getenv("OPENAI_API_KEY"),
}, "gpt-4o")

// Create agent with LLM client
a := agent.NewAgent(agent.AgentOptions{
    SystemPrompt: "You are a helpful assistant",
    LLMClient:   llmClient,
})

// Prompt will now call real LLM
ctx := context.Background()
a.State().AddMessage(agent.NewTextMessage(agent.RoleUser, "Hello"))
err := a.Prompt(ctx)
```

If no LLM client is provided, the agent uses a mock loop for backward compatibility.
```

**Step 2: Update llm/README.md with agent integration**

Add to `docs/llm/README.md`:

```markdown
## Agent Integration

The `NewAgentClient` function creates an LLM client adapter for use with the agent:

```go
import "github.com/nevaria-ai/gateway/internal/llm"

client := llm.NewAgentClient(cfg, "gpt-4o")
```

This adapter implements `agent.LLMClient` interface and translates between:
- Agent message types and LLM client stream events
- Agent tool format and provider-specific tool formats

See `docs/agent/agent.md` for complete agent usage examples.
```

**Step 3: Commit**

```bash
git add docs/agent/agent.md docs/llm/README.md
git commit -m "docs: add LLM agent integration documentation"
```

---

## Summary

After completing all tasks:
1. Agent accepts LLM client via `AgentOptions`
2. Agent calls real LLM streaming instead of mock loop
3. WebSocket handler creates agents with LLM clients
4. Configuration reads LLM API keys from environment
5. Integration test verifies end-to-end functionality
6. Documentation updated with usage examples

**Estimated Time:** 8 tasks × ~10 minutes = ~80 minutes

**Verification:**
- All tests pass: `go test ./...`
- Integration test passes with real API key
- Agent generates real AI responses
