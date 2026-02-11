# WebSocket Agent Integration

## Overview

The WebSocket gateway provides real-time communication between AI agents and clients through WebSocket connections. Each WebSocket connection is associated with a dedicated agent instance that manages conversation state, tool execution, and event streaming.

### How Agents are Integrated with WebSocket Connections

- **Per-Connection Agent**: Each WebSocket connection creates or retrieves a dedicated agent instance using the session ID
- **Agent Lifecycle Management**: The `AgentManager` handles creation, retrieval, and cleanup of agent instances
- **Event Broadcasting**: Agents publish events that are forwarded to WebSocket clients in real-time
- **State Persistence**: Agent state (conversation history, tools, configuration) persists for the duration of the session

### Per-Connection Agent Lifecycle

1. **Connection**: WebSocket upgrade request creates unique connection and session IDs
2. **Agent Creation**: `AgentManager.GetOrCreateAgent()` creates/retrieves agent for session
3. **Event Subscription**: Client subscribes to agent events for real-time updates
4. **Message Handling**: Incoming prompts are sent to the agent for processing
5. **Event Forwarding**: Agent events are forwarded to the WebSocket client
6. **Cleanup**: On disconnect, agent is unsubscribed and removed from manager

### Event Forwarding to Clients

- **Real-time Streaming**: Agent events are converted to WebSocket events and sent immediately
- **Event Types**: All agent lifecycle, turn, message, and tool events are forwarded
- **Message Pooling**: Efficient message reuse through sync.Pool reduces memory allocation
- **Backpressure Handling**: Non-blocking writes with timeouts prevent connection blocking

## WebSocket Protocol

### Message Format

All WebSocket messages use JSON format with a consistent structure:

```json
{
  "type": "message_type",
  "data": {
    // Message-specific data
  }
}
```

### Message Types

#### Client → Server Messages

**connected**
- Sent after connection establishment
- Contains connection and session information
- Only sent once per connection

```json
{
  "type": "connected",
  "data": {
    "session_id": "uuid",
    "agent_id": "uuid",
    "conn_id": "uuid"
  }
}
```

**prompt**
- Send a prompt to the agent for processing
- Triggers agent execution with the provided text

```json
{
  "type": "prompt",
  "text": "Hello, how are you?"
}
```

#### Server → Client Messages

**agent_start**
- Emitted when agent starts processing a prompt

```json
{
  "type": "agent_start",
  "data": {
    "timestamp": 1234567890
  }
}
```

**agent_end**
- Emitted when agent finishes processing

```json
{
  "type": "agent_end",
  "data": {
    "timestamp": 1234567890,
    "messages": []
  }
}
```

**turn_start**
- Emitted when a new turn (interaction cycle) begins

```json
{
  "type": "turn_start",
  "data": {
    "timestamp": 1234567890
  }
}
```

**turn_end**
- Emitted when a turn completes

```json
{
  "type": "turn_end",
  "data": {
    "timestamp": 1234567890,
    "message": {},
    "toolResults": []
  }
}
```

**message_start**
- Emitted when message generation starts

```json
{
  "type": "message_start",
  "data": {
    "timestamp": 1234567890,
    "message": {
      "role": "assistant",
      "content": []
    }
  }
}
```

**message_update**
- Emitted during streaming message generation with new content

```json
{
  "type": "message_update",
  "data": {
    "timestamp": 1234567890,
    "message": {
      "role": "assistant",
      "content": []
    },
    "delta": "Hello"
  }
}
```

**message_end**
- Emitted when message generation is complete

```json
{
  "type": "message_end",
  "data": {
    "timestamp": 1234567890,
    "message": {
      "role": "assistant",
      "content": []
    }
  }
}
```

**tool_start**
- Emitted when tool execution begins

```json
{
  "type": "tool_start",
  "data": {
    "timestamp": 1234567890,
    "tool_call_id": "call-123",
    "tool_name": "calculator",
    "args": {
      "x": 1,
      "y": 2
    }
  }
}
```

**tool_update**
- Emitted during tool execution with partial results

```json
{
  "type": "tool_update",
  "data": {
    "timestamp": 1234567890,
    "tool_call_id": "call-123",
    "tool_name": "calculator",
    "partial_result": 3
  }
}
```

**tool_end**
- Emitted when tool execution completes

```json
{
  "type": "tool_end",
  "data": {
    "timestamp": 1234567890,
    "tool_call_id": "call-123",
    "tool_name": "calculator",
    "result": 3,
    "is_error": false
  }
}
```

## Connection Flow

### WebSocket Upgrade

1. **Initial Request**: Client sends HTTP GET request to `/ws`
2. **Origin Check**: Server validates Origin header against allowed origins
3. **Connection Limit**: If configured, checks connection limit semaphore
4. **Upgrade**: HTTP upgraded to WebSocket connection
5. **Connection IDs**: Generate unique connection and session IDs

### Agent Creation

1. **Session ID**: Use session ID as agent identifier
2. **AgentManager**: Get or create agent instance
3. **State Initialization**: Agent starts with empty message history
4. **Configuration**: Agent inherits configuration (tools, model, etc.)

### Event Subscription

1. **Subscribe**: Create event channel with agent
2. **Forward Setup**: Start goroutine to forward events to WebSocket
3. **Event Conversion**: Agent events converted to WebSocket events
4. **Non-blocking Writes**: Events queued with timeout handling

### Prompt Handling

1. **Message Parsing**: JSON message parsed for type and content
2. **Prompt Validation**: Check for required text field
3. **Agent Processing**: Send prompt to agent for execution
4. **Event Stream**: Agent events stream back to client in real-time

### Cleanup on Disconnect

1. **Context Cancel**: Cancel all goroutines associated with connection
2. **Agent Unsubscribe**: Remove event subscription
3. **Agent Removal**: Remove agent from AgentManager
4. **Resource Cleanup**: Close connection and return resources to pool

## Message Examples

### Connecting

Client connects and receives connection confirmation:

```
Client → GET /ws HTTP/1.1
Origin: https://example.com

Server → 101 Switching Protocols
[WebSocket upgrade]

Server → {"type": "connected", "data": {"session_id": "550e8400-e29b-41d4-a716-446655440000", "agent_id": "550e8400-e29b-41d4-a716-446655440000", "conn_id": "550e8400-e29b-41d4-a716-446655440001"}}
```

### Sending a Prompt

Client sends a prompt to the agent:

```
Client → {"type": "prompt", "text": "Calculate 2 + 2"}
```

### Receiving Agent Events

Agent processes prompt and streams events:

```
Server → {"type": "agent_start", "data": {"timestamp": 1234567890}}

Server → {"type": "turn_start", "data": {"timestamp": 1234567890}}

Server → {"type": "message_start", "data": {"timestamp": 1234567890, "message": {"role": "assistant", "content": []}}}

Server → {"type": "tool_start", "data": {"timestamp": 1234567890, "tool_call_id": "call-123", "tool_name": "calculator", "args": {"x": 2, "y": 2}}}

Server → {"type": "tool_update", "data": {"timestamp": 1234567890, "tool_call_id": "call-123", "tool_name": "calculator", "partial_result": 4}}

Server → {"type": "tool_end", "data": {"timestamp": 1234567890, "tool_call_id": "call-123", "tool_name": "calculator", "result": 4, "is_error": false}}

Server → {"type": "message_update", "data": {"timestamp": 1234567890, "message": {"role": "assistant", "content": [{"type": "text", "text": ""}]}, "delta": "The answer is 4."}}

Server → {"type": "message_end", "data": {"timestamp": 1234567890, "message": {"role": "assistant", "content": [{"type": "text", "text": "The answer is 4."}]}}

Server → {"type": "turn_end", "data": {"timestamp": 1234567890, "message": {"role": "assistant", "content": [{"type": "text", "text": "The answer is 4."}]}, "toolResults": []}}

Server → {"type": "agent_end", "data": {"timestamp": 1234567890, "messages": [...]}}
```

### Tool Execution Events

Multiple tool calls in sequence:

```
Server → {"type": "tool_start", "data": {"timestamp": 1234567890, "tool_call_id": "call-124", "tool_name": "search", "args": {"query": "weather"}}}

Server → {"type": "tool_update", "data": {"timestamp": 1234567890, "tool_call_id": "call-124", "tool_name": "search", "partial_result": "Sunny"}}}

Server → {"type": "tool_end", "data": {"timestamp": 1234567890, "tool_call_id": "call-124", "tool_name": "search", "result": "Sunny, 72°F", "is_error": false}}

Server → {"type": "tool_start", "data": {"timestamp": 1234567890, "tool_call_id": "call-125", "tool_name": "save", "args": {"data": "Weather: Sunny"}}}

Server → {"type": "tool_end", "data": {"timestamp": 1234567890, "tool_call_id": "call-125", "tool_name": "save", "result": "Saved", "is_error": false}}
```

## Error Handling

### Invalid Messages

- **JSON Parsing Errors**: Malformed JSON messages are logged and ignored
- **Missing Fields**: Required field validation (e.g., missing text in prompt)
- **Unknown Types**: Unknown message types are logged and ignored

### Tool Errors

Tool execution errors are captured and forwarded:

```json
{
  "type": "tool_end",
  "data": {
    "timestamp": 1234567890,
    "tool_call_id": "call-123",
    "tool_name": "calculator",
    "result": null,
    "is_error": true
  }
}
```

### Connection Errors

- **Unexpected Close**: Normal closure (going away, abnormal) triggers cleanup
- **Write Failures**: Writer goroutine reports errors and triggers connection closure
- **Read Timeouts**: Inactivity triggers ping/pong mechanism
- **Write Timeouts**: Non-blocking writes with timeout prevent deadlocks

### Error Recovery

- **Graceful Shutdown**: Context cancellation ensures clean resource cleanup
- **Connection Limits**: Reached capacity returns 503 Service Unavailable
- **Memory Protection**: Message size limits prevent memory exhaustion
- **Resource Cleanup**: All goroutines properly cleaned up on disconnect

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GATEWAY_MAX_CONNS` | 0 | Maximum concurrent connections (0 = unlimited) |
| `GATEWAY_ALLOWED_ORIGINS` | deny all | Comma-separated origin whitelist |

### Connection Limits

- **Semaphore Pattern**: Uses buffered channel for connection limiting
- **Atomic Counter**: Tracks active connections for monitoring
- **Graceful Rejection**: Returns 503 when at capacity

### Origin Validation

- **Strict Checking**: Validates Origin header against allowed origins
- **Development Mode**: "*" allows all origins (use with caution)
- **Same-Origin**: Empty origin allowed for non-browser clients

## Performance Considerations

### Memory Management

- **Message Pooling**: sync.Pool reduces allocation overhead
- **Buffer Limits**: Configurable buffer sizes prevent memory bloat
- **Connection Cleanup**: Proper resource release prevents leaks

### Concurrency

- **Single Writer Pattern**: Dedicated writer goroutine ensures thread safety
- **Non-blocking Operations**: Prevents goroutine deadlocks
- **Event Channels**: Buffered channels handle backpressure

### Scalability

- **Horizontal Scaling**: Stateless design enables load balancing
- **Connection Multiplexing**: Single handler manages multiple connections
- **Efficient Routing**: Event subscription minimizes memory usage

## Best Practices

1. **Always handle connection errors gracefully**
2. **Use proper timeout values for network operations**
3. **Implement proper logging for debugging**
4. **Test with various message sizes and patterns**
5. **Monitor connection metrics for scaling decisions**
6. **Use appropriate origin policies for security**
7. **Implement proper cleanup on disconnect**
8. **Test error scenarios and edge cases**