# Anthropic Client

This document describes the Anthropic provider implementation for the LLM client framework.

## Overview

The Anthropic client integrates with the Anthropic API using the official `anthropic-sdk-go` SDK. It supports:

- Claude 3 models (Sonnet, Haiku, Opus) and Claude 3.5 models
- Streaming responses with server-sent events (SSE)
- Function/tool calling
- Thinking mode with configurable token budgets
- Custom base URLs
- Image content support (vision)

## AnthropicClientConfig

```go
type AnthropicClientConfig struct {
    APIKey        string
    BaseURL       string
    Model         string
    Timeout       time.Duration
    ThinkingLevel agent.ThinkingLevel
}
```

Configuration for the Anthropic client.

### ThinkingLevel

Controls the reasoning intensity for Claude models:

| Level | Budget Tokens | Description |
|-------|---------------|-------------|
| `ThinkingLevelOff` | - | Extended thinking disabled |
| `ThinkingLevelMinimal` | 1,000 | Minimal reasoning |
| `ThinkingLevelLow` | 3,000 | Light reasoning |
| `ThinkingLevelMedium` | 12,000 | Medium reasoning |
| `ThinkingLevelHigh` | 24,000 | Maximum reasoning |

## AnthropicClient

```go
type AnthropicClient struct {
    config AnthropicClientConfig
    client anthropic.Client
    model  agent.Model
}
```

The Anthropic provider implementation.

### NewAnthropicClient

```go
func NewAnthropicClient(config AnthropicClientConfig) *AnthropicClient
```

Creates a new Anthropic client with the given configuration.

**Example:**
```go
import "github.com/anthropics/anthropic-sdk-go/option"

client := llm.NewAnthropicClient(llm.AnthropicClientConfig{
    APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
    BaseURL: "https://api.anthropic.com",
    Model:   "claude-3-5-sonnet-20241022",
    Timeout: 120 * time.Second,
    ThinkingLevel: agent.ThinkingLevelMedium,
})
```

## Stream

```go
func (c *AnthropicClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error)
```

Sends messages to the Anthropic API and returns a channel of events.

### Process

1. Convert agent messages to Anthropic SDK format
2. Convert tools to Anthropic tool format
3. Add thinking configuration if enabled
4. Call the streaming Messages API
5. Parse SSE stream and emit events

### Event Flow

```
MessageStartEvent -> MessageUpdateEvent(s) -> MessageEndEvent
```

## Message Conversion

### convertMessagesToSDK

```go
func (c *AnthropicClient) convertMessagesToSDK(messages []agent.AgentMessage) []anthropic.MessageParam
```

Converts agent messages to Anthropic SDK format.

#### Role Mapping

| Agent Role | Anthropic Role |
|------------|----------------|
| `RoleUser` | `user` |
| `RoleAssistant` | `assistant` |
| `RoleToolResult` | `user` (with tool result block) |

#### Tool Result Messages

Tool results are sent as user messages with a `ToolResultBlock`:

```go
toolResult := anthropic.NewToolResultBlock(msg.ToolCallID, contentText, msg.IsError)

userMsg := anthropic.MessageParam{
    Role:    anthropic.MessageParamRoleUser,
    Content: []anthropic.ContentBlockParamUnion{toolResult},
}
```

## Content Block Conversion

### convertContentBlocks

```go
func (c *AnthropicClient) convertContentBlocks(blocks []agent.ContentBlock) []anthropic.ContentBlockParamUnion
```

Converts agent content blocks to Anthropic SDK content format.

#### Supported Block Types

| Agent Block | Anthropic Block |
|--------------|-----------------|
| `TextContent` | `TextBlockParam` |
| `ImageContent` | `ImageBlockParam` (with base64 source) |
| `ToolCallContent` | `ToolUseBlockParam` |

#### Image Support

Images are encoded as base64:

```go
source := anthropic.Base64ImageSourceParam{
    Data: b.Data,
}
if b.MimeType != "" {
    source.MediaType = anthropic.Base64ImageSourceMediaType(b.MimeType)
}
```

## Tool Conversion

### convertToolsToSDK

```go
func (c *AnthropicClient) convertToolsToSDK(tools []agent.Tool) []anthropic.ToolUnionParam
```

Converts agent tools to Anthropic SDK tool format using `ToAnthropicTools` and further transforms input schemas.

## Thinking Support

### convertThinkingLevelToSDK

```go
func (c *AnthropicClient) convertThinkingLevelToSDK() *anthropic.ThinkingConfigParamUnion
```

Converts thinking level to SDK format. Returns `nil` for `ThinkingLevelOff`.

**Token Budget Mapping:**

```go
switch c.config.ThinkingLevel {
case agent.ThinkingLevelMinimal:
    budget = 1000
case agent.ThinkingLevelLow:
    budget = 3000
case agent.ThinkingLevelMedium:
    budget = 12000
case agent.ThinkingLevelHigh:
    budget = 24000
}
```

## Stream Parsing

### parseSDKStream

```go
func (c *AnthropicClient) parseSDKStream(ctx context.Context, stream *ssestream.Stream[anthropic.MessageStreamEventUnion]) (<-chan agent.AgentEvent, error)
```

Parses Anthropic SDK streaming response and emits events.

#### Event Type Handling

| SDK Event Type | Action |
|----------------|---------|
| `message_start` | Initialize message, emit `MessageStartEvent` |
| `content_block_start` | Initialize text or tool use buffer |
| `content_block_delta` | Emit `MessageUpdateEvent` for text deltas |
| `content_block_stop` | Flush accumulated text or tool use |
| `message_delta` | Update final message metadata |
| `message_stop` | Emit `MessageEndEvent` |

#### Tool Call Buffer

```go
type anthropicToolCallBuffer struct {
    ID        string
    Name      string
    Arguments map[string]any
}
```

Accumulates tool call data during streaming.

## Supported Models

| Model Pattern | Examples |
|---------------|-----------|
| `claude-3-5` | claude-3-5-sonnet, claude-3-5-haiku |
| `claude-3` | claude-3-opus, claude-3-sonnet, claude-3-haiku |
| `claude-2` | claude-2, claude-2.1 |

## Environment Variables

```bash
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_BASE_URL=https://api.anthropic.com
ANTHROPIC_THINKING_LEVEL=medium
```

## Usage Example

```go
config := llm.Config{
    AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
    Timeout:        120 * time.Second,
}

model := agent.Model{
    ID:       "claude-3-5-sonnet-20241022",
    Provider:  "anthropic",
}

client, err := llm.NewClient(config, model)
if err != nil {
    log.Fatal(err)
}

messages := []agent.AgentMessage{
    agent.NewTextMessage(agent.RoleUser, "Hello, Claude!"),
}

events, err := client.Stream(ctx, messages, nil)
if err != nil {
    log.Fatal(err)
}

for event := range events {
    switch e := event.(type) {
    case agent.MessageStartEvent:
        fmt.Printf("Assistant: ")
    case agent.MessageUpdateEvent:
        fmt.Print(e.Delta)
    case agent.MessageEndEvent:
        fmt.Println("\n[Done]")
    }
}
```

## Error Handling

The client returns errors from:

- Connection failures (before streaming starts)
- API errors (4xx, 5xx status codes)
- Invalid request parameters
- Missing API keys

Stream errors are handled by closing the event channel early.

## Related

- [Factory](factory.md) - Client creation and NewClient wrapper
- [Types](types.md) - Provider enum and Config struct
- [Tools](tools.md) - Tool format conversion
- [OpenAI Client](openai.md) - OpenAI implementation comparison
- [Google Client](google.md) - Google implementation comparison
