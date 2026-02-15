// Package ctx provides context utilities for the gateway.
package ctx

import "context"

// contextKey is a custom type for context keys to prevent collisions.
type contextKey string

// UserIDKey is the context key for user ID.
const UserIDKey contextKey = "user_id"

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
