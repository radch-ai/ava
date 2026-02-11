# Protocol Specification

This document describes the WebSocket message protocol used for client-gateway communication.

## Client Messages

### Prompt Message

Sends a text prompt to the agent.

```json
{
    "type": "prompt",
    "text": "Hello, agent!"
}
```

**Fields:**
- `type` (required) - Must be `"prompt"`
- `text` (required) - The prompt text to send

## Server Events

All events from server to client share a common structure:

```json
{
    "type": "event_type",
    "data": { /* event-specific data */ }
}
```

### Connection Event

Sent when connection is established.

```json
{
    "type": "connected",
    "data": {
        "session_id": "uuid-string",
        "agent_id": "uuid-string",
        "conn_id": "uuid-string"
    }
}
```

### Agent Lifecycle Events

#### agent_start

Agent has started processing.

```json
{
    "type": "agent_start",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z"
    }
}
```

#### agent_end

Agent has finished processing.

```json
{
    "type": "agent_end",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "messages": [/* message objects */]
    }
}
```

### Turn Events

#### turn_start

A new turn (interaction cycle) has started.

```json
{
    "type": "turn_start",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z"
    }
}
```

#### turn_end

Turn has completed.

```json
{
    "type": "turn_end",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "message": { /* message object */ },
        "toolResults": [/* tool result objects */]
    }
}
```

### Message Events

#### message_start

Agent started generating a message.

```json
{
    "type": "message_start",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "message": {
            "role": "assistant",
            "content": ""
        }
    }
}
```

#### message_update

Streaming update to message content (delta).

```json
{
    "type": "message_update",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "message": {
            "role": "assistant",
            "content": "Hello"
        },
        "delta": "Hello"
    }
}
```

#### message_end

Message generation complete.

```json
{
    "type": "message_end",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "message": {
            "role": "assistant",
            "content": "Hello! How can I help?"
        }
    }
}
```

### Tool Execution Events

#### tool_start

Agent started executing a tool.

```json
{
    "type": "tool_start",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "tool_call_id": "call_123",
        "tool_name": "calculator",
        "args": {
            "expression": "2 + 2"
        }
    }
}
```

#### tool_update

Partial result during tool execution.

```json
{
    "type": "tool_update",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "tool_call_id": "call_123",
        "tool_name": "calculator",
        "partial_result": "4"
    }
}
```

#### tool_end

Tool execution completed.

```json
{
    "type": "tool_end",
    "data": {
        "timestamp": "2024-01-01T00:00:00Z",
        "tool_call_id": "call_123",
        "tool_name": "calculator",
        "result": 4,
        "is_error": false
    }
}
```

## Communication Flow

```
Client                          Gateway                          Agent
  |                               |                                |
  |---------- CONNECT ----------->| |                               |
  |<----- connected event -------| |                               |
  |                               |-----> Create/Get Agent ------->|
  |                               |<--- Subscribe Events ---------|
  |                               |                                |
  |---------- prompt --------->| |-----> Prompt ----------------->|
  |                               |                                |
  |                            agent_start <------------------------|
  |<------ agent_start --------| |                                |
  |                           turn_start <------------------------|
  |<------ turn_start --------| |                                |
  |                        message_start <------------------------|
  |<----- message_start -----| |                                |
  |                       message_update <------------------------|
  |<---- message_update ----| |                                |
  |                       message_update <------------------------|
  |<---- message_update ----| |                                |
  |                         message_end <------------------------|
  |<------ message_end -----| |                                |
  |                             turn_end <----------------------|
  |<------- turn_end -------| |                                |
  |                              agent_end <---------------------|
  |<------ agent_end --------| |                                |
```

## Event Type Mapping

| Agent Event | WebSocket Event Type |
|------------|---------------------|
| `AgentStartEvent` | `agent_start` |
| `AgentEndEvent` | `agent_end` |
| `TurnStartEvent` | `turn_start` |
| `TurnEndEvent` | `turn_end` |
| `MessageStartEvent` | `message_start` |
| `MessageUpdateEvent` | `message_update` |
| `MessageEndEvent` | `message_end` |
| `ToolExecutionStartEvent` | `tool_start` |
| `ToolExecutionUpdateEvent` | `tool_update` |
| `ToolExecutionEndEvent` | `tool_end` |

## Related

- [Handler](handler.md) - WebSocket handler implementing protocol
- [Agent Manager](agent_manager.md) - Agent lifecycle management
