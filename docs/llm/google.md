# Google Client

This document describes the Google provider implementation for the LLM client framework.

## Overview

The Google client integrates with the Google Generative AI API using the `google.golang.org/genai` SDK. It supports:

- Gemini 2.5 models (Pro, Flash) and earlier Gemini models
- Streaming responses
- Function/tool calling
- Custom base URLs
- Multi-turn conversations

## GoogleClientConfig

```go
type GoogleClientConfig struct {
    APIKey  string
    BaseURL string
    Model   string
    Timeout time.Duration
}
```

Configuration for the Google client.

## GoogleClient

```go
type GoogleClient struct {
    config GoogleClientConfig
    client *genai.Client
    model  agent.Model
}
```

The Google provider implementation.

### NewGoogleClient

```go
func NewGoogleClient(config GoogleClientConfig) *GoogleClient
```

Creates a new Google client with the given configuration.

**Example:**
```go
client := llm.NewGoogleClient(llm.GoogleClientConfig{
    APIKey:  os.Getenv("GOOGLE_API_KEY"),
    BaseURL: "https://generativelanguage.googleapis.com",
    Model:   "gemini-2.5-pro",
    Timeout: 120 * time.Second,
})
```

## Lazy Initialization

The GenAI client is initialized lazily on the first API call:

```go
func (c *GoogleClient) initClient(ctx context.Context) error
```

This allows creating the client struct before the context is available.

## Stream

```go
func (c *GoogleClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error)
```

Sends messages to the Google GenAI API and returns a channel of events.

### Process

1. Initialize the GenAI client (if needed)
2. Convert agent messages to `genai.Content` format
3. Convert tools to `genai.Tool` format
4. Call the streaming `GenerateContent` API
5. Parse stream and emit events

### Event Flow

```
MessageStartEvent -> MessageUpdateEvent(s) -> MessageEndEvent
```

## Message Conversion

### convertMessagesToGenai

```go
func (c *GoogleClient) convertMessagesToGenai(messages []agent.AgentMessage) []*genai.Content
```

Converts agent messages to `genai.Content` format.

#### Role Mapping

| Agent Role | Google Role |
|------------|--------------|
| `RoleUser` | `user` |
| `RoleAssistant` | `model` |
| `RoleToolResult` | `user` (with function response part) |

#### Tool Result Messages

Tool results are sent as user messages with a `FunctionResponse` part:

```go
content := &genai.Content{
    Role:  "user",
    Parts: []*genai.Part{
        {
            FunctionResponse: &genai.FunctionResponse{
                Name:     msg.ToolName,
                Response: convertToolResultContent(msg.Content),
            },
        },
    },
}
```

## Content Block Conversion

### convertContentToGenaiParts

```go
func (c *GoogleClient) convertContentToGenaiParts(msg agent.AgentMessage) []*genai.Part
```

Converts message content to `genai.Part` format.

#### Supported Block Types

| Agent Block | GenAI Part |
|--------------|-------------|
| `TextContent` | `Part{Text: ...}` |
| `ImageContent` | `Part{InlineData: Blob{...}}` |
| `ToolCallContent` | `Part{FunctionCall: ...}` |

#### Image Support

Images are sent as inline data with MIME type:

```go
parts = append(parts, &genai.Part{
    InlineData: &genai.Blob{
        MIMEType: b.MimeType,
        Data:     []byte(b.Data),
    },
})
```

## Tool Conversion

### convertToolsToGenai

```go
func (c *GoogleClient) convertToolsToGenai(tools []agent.Tool) []*genai.Tool
```

Converts agent tools to `genai.Tool` format.

First converts to `GoogleTools` format, then transforms to SDK format using `convertJSONSchemaToGenai`.

### convertJSONSchemaToGenai

```go
func convertJSONSchemaToGenai(schema *JSONSchema) *genai.Schema
```

Converts `JSONSchema` to `genai.Schema` format.

#### Process

1. Returns `nil` if schema is `nil`
2. Converts the schema type string to `genai.Type`
3. Converts properties map to `map[string]*genai.Schema` with type and description for each
4. Copies required array

#### Example

```go
genaiSchema := &genai.Schema{
    Type: genai.Type("object"),
    Properties: map[string]*genai.Schema{
        "location": &genai.Schema{
            Type:        genai.Type("string"),
            Description: "City name",
        },
    },
    Required: []string{"location"},
}
```

### convertToolResultContent

```go
func convertToolResultContent(content []agent.ContentBlock) map[string]any
```

Converts tool result content to a format suitable for `FunctionResponse`.

Extracts the result from content blocks:

- If first block is `TextContent`, returns `{"result": tc.Text}`
- Otherwise returns `{"result": nil}`

This ensures tool results are properly formatted for Google's API which expects a map with a result field.

## Stream Parsing

The stream parser handles the iterator returned by `GenerateContentStream`:

### State Management

- `currentMessage` - The accumulating message
- `currentText` - Text content builder
- `currentBlocks` - Accumulated content blocks
- `functionCallBuffers` - Function call data accumulated across chunks
- `sentMessageStart` - Tracks whether `MessageStartEvent` was sent

### Event Types Emitted

- `MessageStartEvent` - On first chunk
- `MessageUpdateEvent` - For each text delta
- `MessageEndEvent` - When `FinishReason` is received

### Function Call Buffer

```go
type functionCallBuffer struct {
    name string
    args map[string]any
}
```

Accumulates function call data during streaming.

## Tool Call ID Generation

```go
func generateToolCallID() string
```

Generates a unique ID for tool calls using timestamp nanoseconds:

```go
return fmt.Sprintf("call_%d", time.Now().UnixNano())
```

## Supported Models

| Model Pattern | Examples |
|---------------|-----------|
| `gemini-2.5` | gemini-2.5-pro, gemini-2.5-flash |
| `gemini-1.5` | gemini-1.5-pro, gemini-1.5-flash |
| `gemini-1.0` | gemini-pro, gemini-ultra |

## Environment Variables

```bash
GOOGLE_API_KEY=...
GOOGLE_BASE_URL=https://generativelanguage.googleapis.com
```

## Usage Example

```go
config := llm.Config{
    GoogleAPIKey: os.Getenv("GOOGLE_API_KEY"),
    Timeout:      120 * time.Second,
}

model := agent.Model{
    ID:       "gemini-2.5-pro",
    Provider:  "google",
}

client, err := llm.NewClient(config, model)
if err != nil {
    log.Fatal(err)
}

messages := []agent.AgentMessage{
    agent.NewTextMessage(agent.RoleUser, "Hello, Gemini!"),
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

## Utility Functions

### ConvertContentToText

```go
func ConvertContentToText(content []agent.ContentBlock) string
```

Converts content blocks to a concatenated text string.

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
- [Anthropic Client](anthropic.md) - Anthropic implementation comparison
