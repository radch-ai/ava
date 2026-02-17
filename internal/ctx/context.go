// Package ctx provides context utilities for the gateway.
package ctx

import "context"

// contextKey is a custom type for context keys to prevent collisions.
type contextKey string

// UserIDKey is the context key for user ID.
const UserIDKey contextKey = "user_id"

// AgentIDKey is the context key for agent ID.
const AgentIDKey contextKey = "agent_id"

// WithUserID adds a user ID to the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// GetUserIDFromContext retrieves the user ID from the context.
// Returns empty string if not set.
func GetUserIDFromContext(ctx context.Context) string {
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		return userID
	}
	return ""
}

// WithAgentID adds an agent ID to the context.
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, AgentIDKey, agentID)
}

// GetAgentIDFromContext retrieves the agent ID from the context.
// Returns empty string if not set.
func GetAgentIDFromContext(ctx context.Context) string {
	if agentID, ok := ctx.Value(AgentIDKey).(string); ok {
		return agentID
	}
	return ""
}
