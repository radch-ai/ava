# Agent Framework

The agent framework provides a unified interface for AI agent creation, management, and execution. It handles tool execution, state management, event-based communication, and LLM integration with support for multiple providers.

## Overview

- **Unified Agent Interface**: Consistent API for different types of AI agents
- **Tool Execution**: Built-in tool registry and execution framework
- **Event System**: Real-time event streaming for client communication
- **State Management**: Persistent agent state across messages
- **LLM Integration**: Unified interface for multiple LLM providers (OpenAI, Anthropic, Google)
- **Content Blocks**: Support for text, images, and tool calls in messages
- **Streaming**: Real-time message streaming with event notifications

## Documentation

| Document | Description |
|----------|-------------|
| [types.md](types.md) | Core types: Role, ContentBlock, AgentMessage, Model, ThinkingLevel, Tool, ToolResult |
| [agent.md](agent.md) | Agent struct, NewAgent, Subscribe, Unsubscribe, State, Prompt |
| [events.md](events.md) | All 10 event types for real-time monitoring |
| [loop.md](loop.md) | Loop struct, Run, ExtractToolCalls, AddToolResults |
| [tool.go.md](tool.go.md) | ExecuteToolCall, FindToolByName, ToolExecutionError |
| [mock.md](mock.md) | MockLLMClient and test helpers for unit testing |
| [tools/](tools/) | Tools subpackage with example tools and registry |

## Quick Start

```go
// Create agent
agent := NewAgent(AgentOptions{
    SystemPrompt: "You are a helpful assistant that can use tools to answer questions.",
    SessionID:    "session-123",
})

// Add tools
state := agent.State()
state.Tools = append(state.Tools, ExampleEchoTool(), ExampleMathTool())

// Subscribe to events
subID, events := agent.Subscribe()
defer agent.Unsubscribe(subID)

// Process message
err := agent.Prompt(ctx, "What's 2 + 2?")
if err != nil {
    log.Fatal(err)
}

// Handle events
for event := range events {
    switch e := event.(type) {
    case MessageStartEvent:
        fmt.Printf("Assistant: %s", e.Message.Content)
    case ToolExecutionStartEvent:
        fmt.Printf("Calling tool: %s\n", e.ToolName)
    case ToolExecutionEndEvent:
        fmt.Printf("Tool result: %v\n", e.Result)
    }
}
```

## Architecture

The agent framework consists of several key components:

1. **Agent** - Core agent runtime with state management and event broadcasting
2. **Loop** - Tool calling loop for processing LLM responses
3. **Events** - Event system for real-time monitoring
4. **Tools** - Tool execution framework with registry
5. **Types** - Core data structures for messages, content, and configuration

## References

- [WebSocket Handler](../websocket.md) - WebSocket communication protocol
- [LLM Clients](../llm.md) - LLM client implementations and providers
