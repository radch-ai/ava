# Mock Testing

The mock package provides test doubles for unit testing agent components. This document covers MockLLMClient and test helpers.

## MockLLMClient

Mock implementation of LLMClient for testing. Returns canned responses in sequence.

```go
type MockLLMClient struct {
    Responses     []MockResponse
    responseIndex int
}
```

### Fields

| Field | Description |
|-------|-------------|
| `Responses` | Slice of canned responses to return in order |
| `responseIndex` | Current position in responses slice |

### MockResponse

Represents a single canned response from the mock LLM.

```go
type MockResponse struct {
    Message    AgentMessage
    BlockUntil chan struct{}
}
```

| Field | Description |
|-------|-------------|
| `Message` | The message to return |
| `BlockUntil` | If set, blocks until channel is closed before sending |

### Stream

Implements the LLMClient interface by returning canned responses as event streams.

```go
func (m *MockLLMClient) Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan AgentEvent, error)
```

#### Behavior

1. If no more responses available, returns closed channel immediately
2. Gets next response and increments index
3. If `BlockUntil` is set, waits for channel to close
4. Checks context for cancellation
5. Sends event sequence:
   - `MessageStartEvent`
   - `MessageUpdateEvent` (with delta text)
   - `MessageEndEvent`
6. Closes channel

#### Example

```go
mock := &MockLLMClient{
    Responses: []MockResponse{
        {
            Message: NewTextMessage(RoleAssistant, "Hello!"),
        },
    },
}

eventCh, err := mock.Stream(ctx, []AgentMessage{userMsg}, nil)
if err != nil {
    t.Fatal(err)
}

for event := range eventCh {
    switch e := event.(type) {
    case MessageEndEvent:
        fmt.Printf("Response: %s\n", e.Message.Content)
    }
}
```

### Reset

Resets the response index for reusing the mock.

```go
func (m *MockLLMClient) Reset()
```

#### Example

```go
mock := &MockLLMClient{
    Responses: []MockResponse{...},
}

// First test
eventCh, _ := mock.Stream(ctx, messages, tools)
// process events...

// Reset for next test
mock.Reset()
eventCh, _ = mock.Stream(ctx, messages, tools)
```

## Test Helpers

### MockToolConfig

Configuration for creating mock tools.

```go
type MockToolConfig struct {
    Name        string
    Description string
    Result      *ToolResult
    Error       error
    ExecuteFunc func(ctx context.Context, args map[string]any) (*ToolResult, error)
}
```

| Field | Description |
|-------|-------------|
| `Name` | Tool name (defaults to "mock_tool") |
| `Description` | Tool description |
| `Result` | Canned result to return |
| `Error` | Canned error to return (overrides Result) |
| `ExecuteFunc` | Custom execution function (overrides Result and Error) |

### MockTool

Creates a Tool for testing based on the provided config.

```go
func MockTool(config MockToolConfig) Tool
```

#### Execution Priority

1. If `ExecuteFunc` is set, uses that function
2. Else if `Error` is set, returns the error
3. Else if `Result` is set, returns the result
4. Else returns empty result with "ok"

#### Example

```go
// Simple result tool
tool := MockTool(MockToolConfig{
    Name:   "test_tool",
    Result: &ToolResult{
        Content: []ContentBlock{TextContent{Text: "test result"}},
    },
})

// Error tool
errorTool := MockTool(MockToolConfig{
    Name:  "failing_tool",
    Error: fmt.Errorf("test error"),
})

// Custom execution
customTool := MockTool(MockToolConfig{
    Name: "custom_tool",
    ExecuteFunc: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
        value := args["input"].(string)
        return &ToolResult{
            Content: []ContentBlock{TextContent{Text: "processed: " + value}},
        }, nil
    },
})
```

### MockEchoTool

Creates a tool that echoes back a string argument.

```go
func MockEchoTool(name string) Tool
```

Expects an "input" argument and returns it as the result.

#### Example

```go
echoTool := MockEchoTool("echo")

result, err := echoTool.Execute(ctx, map[string]any{
    "input": "hello world",
})
// result.Content[0].(TextContent).Text == "hello world"
```

### MockFailingTool

Creates a tool that always returns the given error.

```go
func MockFailingTool(name string, err error) Tool
```

#### Example

```go
failTool := MockFailingTool("fail_tool", fmt.Errorf("test failure"))

_, err := failTool.Execute(ctx, map[string]any{})
// err.Error() == "test failure"
```

### MockResultTool

Creates a tool that returns the given result.

```go
func MockResultTool(name string, result *ToolResult) Tool
```

#### Example

```go
resultTool := MockResultTool("result_tool", &ToolResult{
    Content: []ContentBlock{TextContent{Text: "42"}},
    Details: 42,
})

result, _ := resultTool.Execute(ctx, map[string]any{})
// result.Details == 42
```

## LLM Client Interface

The MockLLMClient implements the LLMClient interface:

```go
type LLMClient interface {
    Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan AgentEvent, error)
}
```

## Complete Test Example

```go
func TestAgentWithMockLLM(t *testing.T) {
    ctx := context.Background()

    // Create mock with canned responses
    mock := &MockLLMClient{
        Responses: []MockResponse{
            {
                Message: NewTextMessage(RoleAssistant, "Test response"),
            },
        },
    }

    // Create loop with mock
    loop := NewLoop(mock)

    // Run the loop
    messages := []AgentMessage{
        NewTextMessage(RoleUser, "Hello"),
    }

    result, err := loop.Run(ctx, messages)
    if err != nil {
        t.Fatal(err)
    }

    // Verify response
    if len(result.Messages) != 2 {
        t.Errorf("Expected 2 messages, got %d", len(result.Messages))
    }
}
```

## Blocking Example

```go
func TestAgentCancellation(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    // Create response that blocks until channel closed
    blockCh := make(chan struct{})
    mock := &MockLLMClient{
        Responses: []MockResponse{
            {
                Message:    NewTextMessage(RoleAssistant, "Delayed"),
                BlockUntil: blockCh,
            },
        },
    }

    // Start streaming (will block)
    eventCh, _ := mock.Stream(ctx, nil, nil)

    // Cancel context
    cancel()

    // Close block channel
    close(blockCh)

    // Verify event channel closes
    for range eventCh {
        t.Error("Should not receive events after cancellation")
    }
}
```

## See Also

- [types.md](types.md) - Core type definitions
- [events.md](events.md) - Event types for mocking
- [loop.md](loop.md) - Loop that uses LLMClient
