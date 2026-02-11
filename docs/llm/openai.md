# OpenAI Client

This document describes the OpenAI provider implementation for the LLM client framework.

## Overview

The OpenAI client integrates with the OpenAI API using the official `openai-go` SDK. It supports:

- GPT-3.5, GPT-4, GPT-4o, GPT-4 Turbo, and o1-series models
- Streaming responses with server-sent events (SSE)
- Function/tool calling
- Custom base URLs and organization support
- Context-aware message conversion

## OpenAIClientConfig

```go
type OpenAIClientConfig struct {
    Client openai.Client
    Model  agent.Model
}
```

Configuration for the OpenAI client. The `Client` field is an instance of the official OpenAI SDK client.

## OpenAIClient

```go
type OpenAIClient struct {
    config OpenAIClientConfig
}
```

The OpenAI provider implementation.

### NewOpenAIClient

```go
func NewOpenAIClient(config OpenAIClientConfig) *OpenAIClient
```

Creates a new OpenAI client with the given configuration.

**Example:**
```go
import (
    "github.com/openai/openai-go"
    "github.com/openai/openai-go/option"
)

sdkClient := openai.NewClient(
    option.WithAPIKey(apiKey),
    option.WithBaseURL(baseURL),
    option.WithRequestTimeout(timeout),
)

client := llm.NewOpenAIClient(llm.OpenAIClientConfig{
    Client: sdkClient,
    Model:  agent.Model{ID: "gpt-4o"},
})
```

## Stream

```go
func (c *OpenAIClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error)
```

Sends messages to the OpenAI API and returns a channel of events.

### Process

1. Convert agent messages to OpenAI format
2. Convert tools to OpenAI function format
3. Call the streaming Chat Completions API
4. Parse SSE stream and emit events

### Event Flow

```
MessageStartEvent -> MessageUpdateEvent(s) -> MessageEndEvent
```

## Message Conversion

### convertMessages

```go
func (c *OpenAIClient) convertMessages(messages []agent.AgentMessage) []openai.ChatCompletionMessageParamUnion
```

Converts agent messages to OpenAI format.

#### Role Mapping

| Agent Role | OpenAI Role |
|------------|-------------|
| `RoleUser` | `user` |
| `RoleAssistant` | `assistant` |
| `RoleToolResult` | `tool` |

#### Tool Call Handling

Assistant messages with tool calls include both content and tool call parameters:

```go
assistantMsgStruct := openai.ChatCompletionAssistantMessageParam{
    ToolCalls: toolCalls,
    Content: openai.ChatCompletionAssistantMessageParamContentUnion{
        OfString: openai.String(content),
    },
}
```

Tool result messages include the tool call ID:

```go
toolMsg := openai.ToolMessage(content, msg.ToolCallID)
```

## Tool Conversion

### convertTools

```go
func (c *OpenAIClient) convertTools(tools []agent.Tool) []openai.ChatCompletionToolParam
```

Converts agent tools to OpenAI function format using `ToOpenAITools` and further transforms parameters for the SDK.

## Tool Call Extraction

### extractToolCalls

```go
func (c *OpenAIClient) extractToolCalls(msg agent.AgentMessage) []openai.ChatCompletionMessageToolCallParam
```

Extracts tool calls from an assistant message's content blocks.

#### Process

1. Iterates through message content blocks
2. Identifies `agent.ToolCallContent` blocks
3. Marshals tool call arguments to JSON string
4. Constructs `ChatCompletionMessageToolCallParam` with ID, function name, and arguments

#### Example

```go
toolCallParam := openai.ChatCompletionMessageToolCallParam{
    ID:       toolCall.ID,
    Function: openai.ChatCompletionMessageToolCallFunctionParam{
        Name:      toolCall.Name,
        Arguments: string(argsJSON),
    },
}
```

### getContentText

```go
func getContentText(msg agent.AgentMessage) string
```

Extracts text content from a message by iterating through content blocks and returning the first `TextContent` block's text. Returns empty string if no text content is found.

## Stream Parsing

### parseOpenAIStream

```go
func (c *OpenAIClient) parseOpenAIStream(ctx context.Context, stream *ssestream.Stream[openai.ChatCompletionChunk]) (<-chan agent.AgentEvent, error)
```

Parses OpenAI streaming response and emits events.

#### State Management

The parser maintains:
- `currentMessage` - The accumulating message
- `currentText` - Text content builder
- `currentBlocks` - Accumulated content blocks
- `toolCallBuffers` - Tool call data accumulated across chunks

#### Event Types Emitted

- `MessageStartEvent` - On first chunk
- `MessageUpdateEvent` - For each text delta
- `MessageEndEvent` - When `finishReason` is received

#### Tool Call Accumulation

Tool calls are accumulated across chunks using `toolCallBuffer`:

```go
type toolCallBuffer struct {
    Index   int64
    ID       string
    Name     string
    ArgsJSON strings.Builder
}
```

When `finishReason` is received, accumulated tool calls are parsed and added to the message.

## Supported Models

| Model Pattern | Examples |
|---------------|-----------|
| `gpt-4` | gpt-4, gpt-4-0613 |
| `gpt-3.5` | gpt-3.5-turbo, gpt-3.5-turbo-16k |
| `gpt-4o` | gpt-4o, gpt-4o-2024-08-06 |
| `gpt-4-turbo` | gpt-4-turbo, gpt-4-turbo-2024-04-09 |
| `o1-` | o1-preview, o1-mini |

## Environment Variables

```bash
OPENAI_API_KEY=sk-...
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_ORG_ID=org-...
```

## Usage Example

```go
config := llm.Config{
    OpenAIAPIKey: os.Getenv("OPENAI_API_KEY"),
    Timeout:      120 * time.Second,
}

model := agent.Model{
    ID:       "gpt-4o",
    Provider:  "openai",
}

client, err := llm.NewClient(config, model)
if err != nil {
    log.Fatal(err)
}

messages := []agent.AgentMessage{
    agent.NewTextMessage(agent.RoleUser, "Hello, GPT!"),
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

Stream errors are handled by closing the event channel early.

## Related

- [Factory](factory.md) - Client creation and NewClient wrapper
- [Types](types.md) - Provider enum and Config struct
- [Tools](tools.md) - Tool format conversion
- [Anthropic Client](anthropic.md) - Anthropic implementation comparison
- [Google Client](google.md) - Google implementation comparison
