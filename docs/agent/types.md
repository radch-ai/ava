# internal/agent/types

Agent types and event definitions.

## Description

The types package defines all core types for the agent runtime including messages, content blocks, roles, agent state, and events.

## Type Hierarchy

```
AgentType
├── Role (enum)
├── ContentBlock (interface)
│   ├── TextContent
│   ├── ImageContent
│   └── ToolCallContent
├── AgentMessage (struct)
├── Tool (struct)
├── Model (struct)
├── ThinkingLevel (enum)
└── AgentState (struct)

AgentEvent (interface)
├── AgentStartEvent
├── AgentEndEvent
├── TurnStartEvent
├── TurnEndEvent
├── MessageStartEvent
├── MessageUpdateEvent
├── MessageEndEvent
├── ToolExecutionStartEvent
├── ToolExecutionUpdateEvent
├── ToolExecutionEndEvent
└── TokenUsageEvent
```

## Message Flow

```
                       NewTextMessage()
                              │
                              ▼
                    Create message
                    with Role & Text
                    Content
                             │
                             ▼
                    Set timestamp
                    to current time
                             │
                             ▼
                    Return
                    AgentMessage
```

## Types

| Name | Kind | Location | Description |
|------|------|----------|-------------|
| Role | type | [types.go#L40](../internal/agent/types.go#L40) | user, assistant, toolResult (enum) |
| ContentBlock | interface | [types.go#L54](../internal/agent/types.go#L54) | Interface for content blocks |
| TextContent | struct | [types.go#L60](../internal/agent/types.go#L60) | Plain text content |
| ImageContent | struct | [types.go#L67](../internal/agent/types.go#L67) | Base64 image content |
| ToolCallContent | struct | [types.go#L75](../internal/agent/types.go#L75) | Tool invocation content |
| AgentMessage | struct | [types.go#L154](../internal/agent/types.go#L154) | Message with role and content |
| NewTextMessage | func | [types.go#L224](../internal/agent/types.go#L224) | Creates text message |
| NewToolCallMessage | func | [types.go#L234](../internal/agent/types.go#L234) | Creates tool call message |
| NewToolResultMessage | func | [types.go#L243](../internal/agent/types.go#L243) | Creates tool result message |
| ThinkingLevel | type | [types.go#L254](../internal/agent/types.go#L254) | Thinking token budget levels (enum) |
| Model | struct | [types.go#L281](../internal/agent/types.go#L281) | LLM model configuration |
| Tool | struct | [types.go#L289](../internal/agent/types.go#L289) | Tool definition |
| AgentState | struct | [types.go#L324](../internal/agent/types.go#L324) | Agent runtime state |
| AgentEvent | interface | [events.go#L24](../internal/agent/events.go#L24) | Base event interface |
| AgentStartEvent | struct | [events.go#L30](../internal/agent/events.go#L30) | Agent started |
| AgentEndEvent | struct | [events.go#L37](../internal/agent/events.go#L37) | Agent ended |
| TurnStartEvent | struct | [events.go#L45](../internal/agent/events.go#L45) | Conversation turn started |
| TurnEndEvent | struct | [events.go#L52](../internal/agent/events.go#L52) | Conversation turn ended |
| MessageStartEvent | struct | [events.go#L61](../internal/agent/events.go#L61) | Message streaming started |
| MessageUpdateEvent | struct | [events.go#L69](../internal/agent/events.go#L69) | Message content updated |
| MessageEndEvent | struct | [events.go#L78](../internal/agent/events.go#L78) | Message streaming ended |
| ToolExecutionStartEvent | struct | [events.go#L86](../internal/agent/events.go#L86) | Tool execution started |
| ToolExecutionUpdateEvent | struct | [events.go#L96](../internal/agent/events.go#L96) | Tool execution updated |
| ToolExecutionEndEvent | struct | [events.go#L106](../internal/agent/events.go#L106) | Tool execution ended |
| TokenUsageEvent | struct | [events.go#L117](../internal/agent/events.go#L117) | Token usage reported |

## Roles

| Role | String Value | Description |
|------|--------------|-------------|
| `RoleUser` | `user` | Message from human user |
| `RoleAssistant` | `assistant` | Message from AI agent |
| `RoleToolResult` | `toolResult` | Result from tool execution |

## Thinking Levels

| Level | Description |
|-------|-------------|
| `ThinkingLevelOff` | Thinking disabled |
| `ThinkingLevelMinimal` | Minimal thinking (1000 tokens) |
| `ThinkingLevelLow` | Low thinking (3000 tokens) |
| `ThinkingLevelMedium` | Medium thinking (12000 tokens) |
| `ThinkingLevelHigh` | High thinking (24000 tokens) |

## Event Hierarchy

```
AgentEvent (interface)
├── AgentStartEvent
├── AgentEndEvent
├── TurnStartEvent
├── TurnEndEvent
├── MessageStartEvent
├── MessageUpdateEvent
├── MessageEndEvent
├── ToolExecutionStartEvent
├── ToolExecutionUpdateEvent
├── ToolExecutionEndEvent
└── TokenUsageEvent
```
