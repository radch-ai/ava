package ctx

import (
	"context"
	"testing"
)

func TestWithUserID(t *testing.T) {
	tests := []struct {
		name   string
		userID string
	}{
		{
			name:   "simple user ID",
			userID: "user123",
		},
		{
			name:   "empty user ID",
			userID: "",
		},
		{
			name:   "UUID format",
			userID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:   "long user ID",
			userID: "very-long-user-id-with-many-characters-1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx := WithUserID(ctx, tt.userID)

			// Verify original context is unchanged
			if GetUserIDFromContext(ctx) != "" {
				t.Error("original context should not have user ID")
			}

			// Verify new context has user ID
			if got := GetUserIDFromContext(newCtx); got != tt.userID {
				t.Errorf("GetUserIDFromContext() = %q, want %q", got, tt.userID)
			}
		})
	}
}

func TestGetUserIDFromContext(t *testing.T) {
	tests := []struct {
		name      string
		setupCtx  func() context.Context
		wantValue string
	}{
		{
			name: "context with user ID",
			setupCtx: func() context.Context {
				return WithUserID(context.Background(), "test-user")
			},
			wantValue: "test-user",
		},
		{
			name: "context without user ID",
			setupCtx: func() context.Context {
				return context.Background()
			},
			wantValue: "",
		},
		{
			name: "context with wrong type value",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), UserIDKey, 12345)
			},
			wantValue: "",
		},
		{
			name: "context with nil value",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), UserIDKey, nil)
			},
			wantValue: "",
		},
		{
			name: "nested context with user ID",
			setupCtx: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, "other-key", "other-value") //nolint:staticcheck // test: intentionally using string key
				ctx = WithUserID(ctx, "nested-user")
				ctx = context.WithValue(ctx, "another-key", "another-value") //nolint:staticcheck // test: intentionally using string key
				return ctx
			},
			wantValue: "nested-user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			if got := GetUserIDFromContext(ctx); got != tt.wantValue {
				t.Errorf("GetUserIDFromContext() = %q, want %q", got, tt.wantValue)
			}
		})
	}
}

func TestContextKeyUniqueness(t *testing.T) {
	// Verify that context keys don't collide with other string keys
	ctx := context.Background()
	ctx = context.WithValue(ctx, "user_id", "string-key-value") //nolint:staticcheck // test: intentionally using string key
	ctx = WithUserID(ctx, "typed-key-value")

	// Both values should coexist
	stringKeyValue, _ := ctx.Value("user_id").(string)
	typedKeyValue := GetUserIDFromContext(ctx)

	if stringKeyValue != "string-key-value" {
		t.Errorf("string key value = %q, want %q", stringKeyValue, "string-key-value")
	}
	if typedKeyValue != "typed-key-value" {
		t.Errorf("typed key value = %q, want %q", typedKeyValue, "typed-key-value")
	}
}
