# Agent Runtime & LLM Proxy - Architecture & Implementation

**Date:** 2026-02-11
**Status:** Design Complete - Ready for Implementation

> **This document consolidates all agent runtime and LLM proxy design decisions.**
> **Previously this was planned as a separate pi-agent service, but we pivoted to a single Go service.**

---

## Table of Contents

1. [Architecture Pivot](#architecture-pivot)
2. [What We're Porting from pi-agent-core](#what-were-porting-from-pi-agent-core)
3. [Agent Runtime Design](#agent-runtime-design)
4. [LLM Proxy Design](#llm-proxy-design)
5. [Tool Execution Framework](#tool-execution-framework)
6. [Event Streaming](#event-streaming)
7. [Implementation Plan](#implementation-plan)
8. [References](#references)

---

## Architecture Pivot

### Original Plan: Two Services

```
Go Gateway ──HTTP/SSE──► Node.js pi-agent ──API──► LLM Providers
```

**Problems:**
- Master LLM API key in separate service (prompt injection risk)
- Inter-service latency (~0.1-0.5ms per call)
- Two runtimes to deploy and coordinate (Go + Bun/Node.js)
- More complex debugging and tracing

### New Plan: Single Go Service

```
┌─────────────────────────────────────────────────────────────┐
│  Go Gateway (Single Service)                                │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  WebSocket & Auth (Existing Phase 0)                │  │
│  │  - WebSocket server                                  │  │
│  │  - OAuth / Auth                                       │  │
│  │  - Multi-tenant session management                   │  │
│  │  - Rate limiting                                      │  │
│  └──────────────────────────────────────────────────────┘  │
│                           │                                 │
│                           ▼                                 │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Agent Runtime (Ported from pi-agent-core)          │  │
│  │  - Per-user Agent state management                  │  │
│  │  - Tool calling loop                                │  │
│  │  - Message types                                    │  │
│  │  - Event streaming                                  │  │
│  └──────────────────────────────────────────────────────┘  │
│                           │                                 │
│                           ▼                                 │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  LLM Proxy                                           │  │
│  │  - Anthropic Messages API                           │  │
│  │  - OpenAI Chat Completions API                      │  │
│  │  - Google Generative AI API                         │  │
│  │  - Usage tracking                                    │  │
│  │  - Master API key never leaves service              │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

**Benefits:**
- ✅ Master key stays in one service (no cross-service exposure)
- ✅ Zero inter-service latency (in-process calls)
- ✅ Single binary deployment
- ✅ Simpler debugging and tracing
- ✅ Go-only codebase

---

## What We're Porting from pi-agent-core

### Source Code Analysis

After analyzing `@mariozechner/pi-agent-core` source code:

| Component | Lines (TS) | Complexity | Port to Go? |
|-----------|------------|------------|-------------|
| `agent.ts` | ~400 | Medium | ✅ Yes |
| `agent-loop.ts` | ~300 | Medium | ✅ Yes |
| `types.ts` | ~180 | Low | ✅ Yes |
| `proxy.ts` | ~200 | Low | ✅ Yes (simplified) |
| `streamSimple()` + providers | ~10,000+ | **HIGH** | ❌ Not needed |

**Key finding:** Only `streamSimple()` (the LLM calling layer) is complex. Everything else is straightforward logic.

### Dependencies from pi-ai

| Import | Used For | Can Replace? |
|--------|----------|-------------|
| `getModel()` | Model config lookup | ✅ Simple struct |
| `Model` type | Model configuration | ✅ Simple struct |
| `streamSimple()` | **Call LLM APIs** | ❌ Replaced by LLM Proxy |
| `AssistantMessage`, `Message` | Message types | ✅ Simple structs |
| `Context` | LLM context | ✅ Simple struct |
| `EventStream` | Event streaming | ✅ Use Go channels |
| `validateToolArguments()` | Tool validation | ✅ JSON schema |
| `parseStreamingJson()` | Parse tool JSON | ✅ Simple utility |

---

## Agent Runtime Design

### Core Types

```go
// Message types - mirroring pi-agent-core's AgentMessage

type Role string

const (
    RoleUser         Role = "user"
    RoleAssistant    Role = "assistant"
    RoleToolResult   Role = "toolResult"
)

type ContentBlock interface {
    isContentBlock()
}

type TextContent struct {
    Text string
}

type ImageContent struct {
    Data      string // base64
    MimeType  string // "image/jpeg", "image/png"
}

type ToolCallContent struct {
    ID        string
    Name      string
    Arguments map[string]any
}

type AgentMessage struct {
    Role      Role
    Content   []ContentBlock
    Timestamp int64
    // ToolResult-specific fields
    ToolCallID string
    ToolName   string
    IsError    bool
}
```

### Agent State

```go
type AgentState struct {
    SystemPrompt      string
    Model             Model
    ThinkingLevel     ThinkingLevel
    Tools             []Tool
    Messages          []AgentMessage
    IsStreaming       bool
    StreamMessage     *AgentMessage
    PendingToolCalls  map[string]bool
    Error             string
}

type ThinkingLevel string

const (
    ThinkingOff      ThinkingLevel = "off"
    ThinkingMinimal  ThinkingLevel = "minimal"
    ThinkingLow      ThinkingLevel = "low"
    ThinkingMedium   ThinkingLevel = "medium"
    ThinkingHigh     ThinkingLevel = "high"
)

type Model struct {
    ID       string
    API      string    // "anthropic", "openai", "google"
    Provider string    // "anthropic", "openai", "google"
    BaseURL  string
    // Cost, context window, etc.
}
```

### Agent struct

```go
type Agent struct {
    mu                sync.RWMutex
    state             AgentState
    subscribers       []chan AgentEvent
    abortController   *context.CancelFunc
    steeringQueue     []AgentMessage
    followUpQueue     []AgentMessage
    sessionId         string
}

// Create a new agent instance for a user
func NewAgent(opts AgentOptions) *Agent {
    return &Agent{
        state: AgentState{
            SystemPrompt: opts.SystemPrompt,
            Model: opts.Model,
            ThinkingLevel: opts.ThinkingLevel,
            Tools: opts.Tools,
            Messages: []AgentMessage{},
            PendingToolCalls: make(map[string]bool),
        },
        subscribers: []chan AgentEvent{},
        sessionId: opts.SessionID,
    }
}

// Send a prompt and stream events
func (a *Agent) Prompt(ctx context.Context, message string) error {
    // 1. Add user message to context
    // 2. Call LLM proxy
    // 3. Stream response events
    // 4. If tool calls: execute tools, loop back to step 2
    // 5. Emit agent_end event
}
```

### Event Types

```go
type AgentEvent interface {
    Type() string
}

// Agent lifecycle
type AgentStartEvent struct{}
type AgentEndEvent struct {
    Messages []AgentMessage
}

// Turn lifecycle
type TurnStartEvent struct{}
type TurnEndEvent struct {
    Message     AgentMessage
    ToolResults []AgentMessage
}

// Message lifecycle
type MessageStartEvent struct {
    Message AgentMessage
}
type MessageUpdateEvent struct {
    Message  AgentMessage
    Delta    string
}
type MessageEndEvent struct {
    Message AgentMessage
}

// Tool execution
type ToolExecutionStartEvent struct {
    ToolCallID string
    ToolName   string
    Args       map[string]any
}
type ToolExecutionUpdateEvent struct {
    ToolCallID     string
    ToolName       string
    PartialResult  any
}
type ToolExecutionEndEvent struct {
    ToolCallID string
    ToolName   string
    Result     any
    IsError    bool
}
```

### Tool Calling Loop (The Core Logic)

```go
func (a *Agent) runLoop(ctx context.Context) error {
    for {
        // 1. Check for abort
        if ctx.Err() != nil {
            return ctx.Err()
        }

        // 2. Stream assistant response from LLM
        assistantMsg, err := a.streamAssistantResponse(ctx)
        if err != nil {
            return err
        }

        // 3. Check for tool calls
        toolCalls := a.extractToolCalls(assistantMsg)
        if len(toolCalls) == 0 {
            // No tool calls, we're done
            break
        }

        // 4. Execute each tool
        var toolResults []AgentMessage
        for _, tc := range toolCalls {
            result, err := a.executeToolCall(ctx, tc)
            toolResults = append(toolResults, result)
            a.state.Messages = append(a.state.Messages, result)
        }

        // 5. Loop back to LLM with tool results
    }

    return nil
}

func (a *Agent) streamAssistantResponse(ctx context.Context) (*AgentMessage, error) {
    // Convert AgentMessage[] to LLM-compatible format
    messages := a.convertToLLMMessages(a.state.Messages)

    // Call LLM proxy
    llmReq := LLMRequest{
        Model:       a.state.Model,
        Messages:    messages,
        SystemPrompt: a.state.SystemPrompt,
        Tools:       a.state.Tools,
    }

    stream, err := llmProxy.Stream(ctx, llmReq)
    if err != nil {
        return nil, err
    }

    // Stream events and build response
    var assistantMsg *AgentMessage
    for event := range stream {
        switch e := event.(type) {
        case TextStartEvent:
            assistantMsg = &AgentMessage{
                Role: RoleAssistant,
                Content: []ContentBlock{TextContent{Text: ""}},
            }
            a.emit(MessageStartEvent{Message: *assistantMsg})

        case TextDeltaEvent:
            // Update message content
            a.emit(MessageUpdateEvent{Delta: e.Delta})

        case ToolCallStartEvent:
            // Add tool call to content
            tc := ToolCallContent{
                ID: e.ID,
                Name: e.Name,
                Arguments: make(map[string]any),
            }
            assistantMsg.Content = append(assistantMsg.Content, tc)

        case DoneEvent:
            a.emit(MessageEndEvent{Message: *assistantMsg})
            return assistantMsg, nil
        }
    }

    return assistantMsg, nil
}

func (a *Agent) executeToolCall(ctx context.Context, tc ToolCallContent) (AgentMessage, error) {
    // Find tool
    var tool *Tool
    for _, t := range a.state.Tools {
        if t.Name == tc.Name {
            tool = &t
            break
        }
    }

    if tool == nil {
        return AgentMessage{
            Role: RoleToolResult,
            ToolCallID: tc.ID,
            ToolName: tc.Name,
            Content: []ContentBlock{TextContent{Text: fmt.Sprintf("Tool %s not found", tc.Name)}},
            IsError: true,
        }, nil
    }

    // Execute tool
    result, err := tool.Execute(ctx, tc.Arguments)
    return AgentMessage{
        Role:      RoleToolResult,
        ToolCallID: tc.ID,
        ToolName:  tc.Name,
        Content:   result.Content,
        IsError:   err != nil,
    }, nil
}
```

---

## LLM Proxy Design

### Proxy Interface

```go
type LLMProxy struct {
    anthropicKey string
    openaiKey    string
    googleKey    string
    httpClient   *http.Client
}

type LLMRequest struct {
    Model        Model
    Messages     []LLMMessage
    SystemPrompt string
    Tools        []Tool
    Temperature  float64
    MaxTokens    int
    Signal       context.Context
}

type LLMMessage struct {
    Role    string
    Content string // Can be JSON for complex content
}

// Stream returns a channel of LLM events
func (p *LLMProxy) Stream(ctx context.Context, req LLMRequest) (<-chan LLMEvent, error) {
    switch req.Model.API {
    case "anthropic":
        return p.streamAnthropic(ctx, req)
    case "openai":
        return p.streamOpenAI(ctx, req)
    case "google":
        return p.streamGoogle(ctx, req)
    default:
        return nil, fmt.Errorf("unsupported API: %s", req.Model.API)
    }
}
```

### Event Types

```go
type LLMEvent interface{}

type TextStartEvent struct {
    ContentIndex int
}

type TextDeltaEvent struct {
    ContentIndex int
    Delta        string
}

type TextEndEvent struct {
    ContentIndex int
    Content      string
}

type ToolCallStartEvent struct {
    ContentIndex int
    ID           string
    Name         string
}

type ToolCallDeltaEvent struct {
    ContentIndex int
    Delta        string // JSON fragment
}

type ToolCallEndEvent struct {
    ContentIndex int
    ID           string
    Name         string
    Arguments    map[string]any
}

type DoneEvent struct {
    Reason      string // "stop", "length", "toolUse"
    Usage       Usage
}

type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}
```

### Anthropic Provider

```go
func (p *LLMProxy) streamAnthropic(ctx context.Context, req LLMRequest) (<-chan LLMEvent, error) {
    events := make(chan LLMEvent)

    // Build request body
    body := AnthropicRequest{
        Model:       req.Model.ID,
        System:      req.SystemPrompt,
        Messages:    convertToAnthropicMessages(req.Messages),
        Tools:       convertToAnthropicTools(req.Tools),
        Stream:      true,
        MaxTokens:   req.MaxTokens,
        Temperature: req.Temperature,
    }

    // Make HTTP request
    httpReq, _ := http.NewRequestWithContext(
        ctx,
        "POST",
        "https://api.anthropic.com/v1/messages",
        jsonReader(body),
    )
    httpReq.Header.Set("x-api-key", p.anthropicKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")
    httpReq.Header.Set("content-type", "application/json")

    resp, err := p.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }

    // Stream SSE response
    go func() {
        defer close(events)
        scanner := bufio.NewScanner(resp.Body)
        for scanner.Scan() {
            line := scanner.Text()
            if strings.HasPrefix(line, "data: ") {
                data := strings.TrimPrefix(line, "data: ")
                event := p.parseAnthropicEvent(data)
                if event != nil {
                    events <- event
                }
            }
        }
    }()

    return events, nil
}

func (p *LLMProxy) parseAnthropicEvent(data string) LLMEvent {
    var raw map[string]any
    json.Unmarshal([]byte(data), &raw)

    switch raw["type"] {
    case "message_start":
        return TextStartEvent{ContentIndex: 0}
    case "content_block_start":
        delta := raw["index"].(float64)
        if raw["content_block"].(map[string]any)["type"] == "tool_use" {
            return ToolCallStartEvent{
                ContentIndex: int(delta),
                ID: raw["content_block"].(map[string]any)["id"].(string),
                Name: raw["content_block"].(map[string]any)["name"].(string),
            }
        }
    case "content_block_delta":
        // Handle text deltas or tool call JSON fragments
    case "content_block_stop":
        return TextEndEvent{}
    case "message_delta":
        return DoneEvent{
            Reason: raw["delta"].(map[string]any)["stop_reason"].(string),
            Usage:  parseUsage(raw["usage"]),
        }
    }
    return nil
}
```

### OpenAI Provider

```go
func (p *LLMProxy) streamOpenAI(ctx context.Context, req LLMRequest) (<-chan LLMEvent, error) {
    // Similar to Anthropic but with OpenAI's format
    // Endpoint: https://api.openai.com/v1/chat/completions
    // SSE format is similar but event names differ
}
```

### Google Provider

```go
func (p *LLMProxy) streamGoogle(ctx context.Context, req LLMRequest) (<-chan LLMEvent, error) {
    // Similar to Anthropic but with Google's format
    // Endpoint: https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent
}
```

---

## Tool Execution Framework

### Tool Interface

```go
type Tool struct {
    Name        string
    Description string
    Parameters  jsonschema.Schema // JSON Schema for validation
    Execute     func(ctx context.Context, args map[string]any) (*ToolResult, error)
}

type ToolResult struct {
    Content []ContentBlock
    Details any
}
```

### Example: Web Search Tool

```go
var WebSearchTool = Tool{
    Name:        "web_search",
    Description: "Search the web for current information",
    Parameters: jsonschema.Schema{
        Type: "object",
        Properties: map[string]jsonschema.Schema{
            "query": {
                Type:        "string",
                Description: "The search query string",
            },
            "count": {
                Type:        "integer",
                Description: "Number of results to return (default: 10, max: 20)",
            },
        },
        Required: []string{"query"},
    },
    Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
        query := args["query"].(string)
        count := 10
        if c, ok := args["count"].(float64); ok {
            count = int(c)
        }

        // Call Brave Search API
        results, err := braveSearch(ctx, query, count)
        if err != nil {
            return &ToolResult{
                Content: []ContentBlock{TextContent{Text: err.Error()}},
            }, nil
        }

        // Format results as text
        var text strings.Builder
        for _, r := range results {
            text.WriteString(fmt.Sprintf("- %s\n  %s\n", r.Title, r.URL))
        }

        return &ToolResult{
            Content: []ContentBlock{TextContent{Text: text.String()}},
            Details: results,
        }, nil
    },
}
```

### Tool Registration

```go
type AgentOptions struct {
    SystemPrompt  string
    Model         Model
    ThinkingLevel ThinkingLevel
    Tools         []Tool
    SessionID     string
}

// Default tools available to all agents
var DefaultTools = []Tool{
    WebSearchTool,
    // More tools to be added
}

func NewAgentWithDefaults(opts AgentOptions) *Agent {
    if opts.Tools == nil {
        opts.Tools = DefaultTools
    }
    return NewAgent(opts)
}
```

---

## Event Streaming

### Publishing Events

```go
func (a *Agent) Subscribe() chan AgentEvent {
    ch := make(chan AgentEvent, 100)
    a.mu.Lock()
    a.subscribers = append(a.subscribers, ch)
    a.mu.Unlock()
    return ch
}

func (a *Agent) Unsubscribe(ch chan AgentEvent) {
    a.mu.Lock()
    for i, sub := range a.subscribers {
        if sub == ch {
            a.subscribers = append(a.subscribers[:i], a.subscribers[i+1:]...)
            break
        }
    }
    a.mu.Unlock()
}

func (a *Agent) emit(event AgentEvent) {
    a.mu.RLock()
    subscribers := make([]chan AgentEvent, len(a.subscribers))
    copy(subscribers, a.subscribers)
    a.mu.RUnlock()

    for _, ch := range subscribers {
        select {
        case ch <- event:
        default:
            // Channel full, skip
        }
    }
}
```

### Converting to WebSocket Messages

```go
func (h *WebSocketHandler) forwardAgentEvents(ws *websocket.Conn, agent *Agent) {
    eventCh := agent.Subscribe()
    defer agent.Unsubscribe(eventCh)

    for event := range eventCh {
        msg := WebSocketMessage{
            Type: "agent_event",
            Data: event,
        }
        if err := ws.WriteJSON(msg); err != nil {
            return
        }
    }
}
```

---

## Implementation Plan

### Phase 1: Core Types and State (2 days)

**File:** `internal/agent/types.go`

**Tasks:**
1. Define message types (AgentMessage, ContentBlock, etc.)
2. Define AgentState struct
3. Define Agent struct with basic methods
4. Write unit tests for types

### Phase 2: Tool Calling Loop (3 days)

**File:** `internal/agent/loop.go`

**Tasks:**
1. Implement runLoop() core logic
2. Implement streamAssistantResponse()
3. Implement executeToolCall()
4. Write integration tests for tool calling

### Phase 3: LLM Proxy - Anthropic (2 days)

**File:** `internal/llm/anthropic.go`

**Tasks:**
1. Implement Anthropic request/response types
2. Implement streamAnthropic()
3. Handle SSE parsing
4. Test with real API

### Phase 4: LLM Proxy - OpenAI & Google (2 days)

**Files:** `internal/llm/openai.go`, `internal/llm/google.go`

**Tasks:**
1. Implement OpenAI provider
2. Implement Google provider
3. Shared utilities for SSE parsing
4. Test each provider

### Phase 5: WebSocket Integration (2 days)

**File:** `internal/websocket/agent_handler.go`

**Tasks:**
1. Connect agent events to WebSocket
2. Handle prompt requests from clients
3. Stream responses back
4. Error handling and cleanup

### Phase 6: Tool Implementations (3 days)

**Files:** `internal/tools/web_search.go`, etc.

**Tasks:**
1. Implement web_search tool (Brave API)
2. Implement tool registry
3. Tool parameter validation
4. Tool execution with timeout

### Phase 7: Testing & Polish (2 days)

**Tasks:**
1. End-to-end testing
2. Performance testing
3. Error handling
4. Documentation

**Total: ~16 days (2-3 weeks with buffer)**

---

## References

- pi-agent source analysis: See discussion from 2026-02-11
- pi-agent-core: https://github.com/badlogic/pi-mono/tree/main/packages/agent
- pi-ai (for reference, not porting): https://github.com/badlogic/pi-mono/tree/main/packages/ai
- OpenClaw: https://github.com/openclaw/openclaw
- Gateway architecture: `./gateway-architecture-decisions.md`
- Service architecture: `/Projects/nevaria/.claude/brainstorming/service-architecture-communication.md`
