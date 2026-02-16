# Context Package

Context utilities for passing user ID through request context.

## Purpose

The ctx package provides typed context keys for safely storing and retrieving user ID from Go context. This prevents collisions with other context values.

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

## Functions

| Function | Description |
|----------|-------------|
| `WithUserID(ctx context.Context, userID string) context.Context` | Adds user ID to context |
| `GetUserIDFromContext(ctx context.Context) string` | Gets user ID from context (returns empty string if not set) |

## Usage

```go
// Set user ID in context
ctx := context.WithValue(r.Context(), ctx.UserIDKey, userID)

// Get user ID from context
userID := ctx.GetUserIDFromContext(r.Context())
```

## File Locations

- [`internal/ctx/ctx.go`](../internal/ctx/ctx.go) - Implementation
