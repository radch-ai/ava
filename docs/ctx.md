# Context Package

Context utilities for passing user ID and agent ID through request context.

## Purpose

The ctx package provides typed context keys for safely storing and retrieving user ID and agent ID from Go context. This prevents collisions with other context values and enables multi-agent routing.

## Types

### contextKey

```go
type contextKey string
```

Custom context key type to prevent key collisions in context.Value().

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `UserIDKey` | contextKey | `"user_id"` | Context key for user ID |
| `AgentIDKey` | contextKey | `"agent_id"` | Context key for agent ID |

## Functions

### User ID

| Function | Description |
|----------|-------------|
| `WithUserID(ctx context.Context, userID string) context.Context` | Adds user ID to context |
| `GetUserIDFromContext(ctx context.Context) string` | Gets user ID from context (returns empty string if not set) |

### Agent ID

| Function | Description |
|----------|-------------|
| `WithAgentID(ctx context.Context, agentID string) context.Context` | Adds agent ID to context |
| `GetAgentIDFromContext(ctx context.Context) string` | Gets agent ID from context (returns empty string if not set) |

## Usage

```go
// Set user ID in context (typically in auth middleware)
ctx := ctx.WithUserID(r.Context(), userID)

// Set agent ID in context (when routing to specific agent)
ctx = ctx.WithAgentID(ctx, agentID)

// Get user ID from context
userID := ctx.GetUserIDFromContext(r.Context())

// Get agent ID from context
agentID := ctx.GetAgentIDFromContext(r.Context())
```

## Multi-Agent Context Pattern

```go
// In WebSocket handler after message routing
func (h *Handler) handleTextMessage(ctx context.Context, data []byte, userID string, ...) {
    var msg struct {
        Type    string `json:"type"`
        AgentID string `json:"agent_id"`
        Text    string `json:"text,omitempty"`
    }

    json.Unmarshal(data, &msg)

    // Add agent ID to context for downstream use
    ctx = ctx.WithAgentID(ctx, msg.AgentID)

    // Get or create agent using composite key
    agentInstance := h.agentManager.GetOrCreateAgentForUser(userID, msg.AgentID)

    // Use agent...
}
```

## File Locations

- [`internal/ctx/context.go`](../internal/ctx/context.go) - Implementation

## Related

- [WebSocket](../websocket.md) - Multi-agent routing using agent_id
- [Agent](../agent/README.md) - Agent runtime with userID and agentID
- [Server Middleware](../server.md) - Authentication middleware that sets userID
