package zitadelvalidate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// introspectFunc is a test-only type for mocking the introspection call.
type introspectFunc func(ctx context.Context, token string) (active bool, subject string, expiry time.Time, err error)

// validateWithIntrospector is a test-only helper that extracts the validation logic
// so it can be tested independently of the Zitadel client.
func validateWithIntrospector(ctx context.Context, introspect introspectFunc, token string) (*SessionInfo, error) {
	if token == "" {
		return nil, ErrSessionInvalid
	}

	active, subject, expiry, err := introspect(ctx, token)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("%w: %w", ErrContextCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %w", ErrAuthUnavailable, err)
		}
		return nil, fmt.Errorf("%w: introspection failed: %w", ErrAuthUnavailable, err)
	}

	if !active {
		return nil, ErrSessionInvalid
	}

	// Check expiration explicitly
	if !expiry.IsZero() && time.Now().After(expiry) {
		return nil, ErrSessionExpired
	}

	if subject == "" {
		return nil, fmt.Errorf("%w: missing subject in active token", ErrInvalidAuthResponse)
	}

	return &SessionInfo{
		UserID: subject,
		Expiry: expiry,
	}, nil
}

func TestValidateWithIntrospector(t *testing.T) {
	futureExpiry := time.Now().Add(1 * time.Hour)
	pastExpiry := time.Now().Add(-1 * time.Hour)

	tests := []struct {
		name        string
		introspect  introspectFunc
		token       string
		wantUserID  string
		wantErr     error
		errContains string
		errExact    bool
	}{
		{
			name: "valid session returns user ID",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return true, "user123", futureExpiry, nil
			},
			token:      "valid-token",
			wantUserID: "user123",
			wantErr:    nil,
		},
		{
			name: "valid session with different user ID",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return true, "admin-user-456", futureExpiry, nil
			},
			token:      "admin-token",
			wantUserID: "admin-user-456",
			wantErr:    nil,
		},
		{
			name: "inactive session returns ErrSessionInvalid",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "", time.Time{}, nil
			},
			token:    "inactive-token",
			wantErr:  ErrSessionInvalid,
			errExact: true,
		},
		{
			name: "inactive session with subject still fails",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "user123", futureExpiry, nil
			},
			token:    "inactive-with-subject",
			wantErr:  ErrSessionInvalid,
			errExact: true,
		},
		{
			name: "active session with empty subject returns ErrInvalidAuthResponse",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return true, "", futureExpiry, nil
			},
			token:       "empty-subject-token",
			wantErr:     ErrInvalidAuthResponse,
			errContains: "missing subject",
		},
		{
			name: "expired token returns ErrSessionExpired",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return true, "user123", pastExpiry, nil
			},
			token:    "expired-token",
			wantErr:  ErrSessionExpired,
			errExact: true,
		},
		{
			name: "token with no expiry is accepted",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return true, "user123", time.Time{}, nil
			},
			token:      "no-expiry-token",
			wantUserID: "user123",
			wantErr:    nil,
		},
		{
			name: "introspection error wraps with ErrAuthUnavailable",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "", time.Time{}, errors.New("connection refused")
			},
			token:       "error-token",
			wantErr:     ErrAuthUnavailable,
			errContains: "connection refused",
		},
		{
			name: "network timeout error wraps with ErrAuthUnavailable",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "", time.Time{}, errors.New("network timeout")
			},
			token:       "network-timeout-token",
			wantErr:     ErrAuthUnavailable,
			errContains: "network timeout",
		},
		{
			name: "context canceled wraps with ErrContextCanceled",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "", time.Time{}, context.Canceled
			},
			token:       "canceled-token",
			wantErr:     ErrContextCanceled,
			errContains: "context canceled",
		},
		{
			name: "context deadline exceeded wraps with ErrAuthUnavailable",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "", time.Time{}, context.DeadlineExceeded
			},
			token:       "timeout-token",
			wantErr:     ErrAuthUnavailable,
			errContains: "deadline exceeded",
		},
		{
			name: "wrapped context canceled is detected",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "", time.Time{}, fmt.Errorf("operation failed: %w", context.Canceled)
			},
			token:   "wrapped-canceled-token",
			wantErr: ErrContextCanceled,
		},
		{
			name: "wrapped deadline exceeded is detected",
			introspect: func(_ context.Context, _ string) (bool, string, time.Time, error) {
				return false, "", time.Time{}, fmt.Errorf("operation failed: %w", context.DeadlineExceeded)
			},
			token:       "wrapped-timeout-token",
			wantErr:     ErrAuthUnavailable,
			errContains: "deadline exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionInfo, err := validateWithIntrospector(context.Background(), tt.introspect, tt.token)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}

				if !errors.Is(err, tt.wantErr) {
					t.Errorf("errors.Is(err, wantErr) = false, want true; got err=%v, wantErr=%v", err, tt.wantErr)
				}

				if tt.errExact && err != tt.wantErr {
					t.Errorf("error should be exactly %v, got %v", tt.wantErr, err)
				}

				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error message %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sessionInfo == nil {
				t.Fatal("expected non-nil SessionInfo")
			}
			if sessionInfo.UserID != tt.wantUserID {
				t.Errorf("UserID = %q, want %q", sessionInfo.UserID, tt.wantUserID)
			}
		})
	}
}

func TestErrorDefinitions(t *testing.T) {
	// Verify all error types are distinct and properly formatted
	errorTypes := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrSessionInvalid", ErrSessionInvalid, "session invalid"},
		{"ErrSessionExpired", ErrSessionExpired, "session expired"},
		{"ErrAuthUnavailable", ErrAuthUnavailable, "auth service unavailable"},
		{"ErrInvalidAuthResponse", ErrInvalidAuthResponse, "invalid response from auth service"},
		{"ErrContextCanceled", ErrContextCanceled, "context canceled"},
	}

	for _, tt := range errorTypes {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("%s.Error() = %q, want %q", tt.name, tt.err.Error(), tt.msg)
			}
		})
	}

	// Verify errors are distinct (no two should be equal)
	for i, e1 := range errorTypes {
		for j, e2 := range errorTypes {
			if i < j && e1.err == e2.err {
				t.Errorf("%s and %s are the same error, they should be distinct", e1.name, e2.name)
			}
		}
	}
}

func TestSessionInfo(t *testing.T) {
	t.Run("UserID and Expiry are correctly stored", func(t *testing.T) {
		expiry := time.Now().Add(1 * time.Hour)
		info := &SessionInfo{UserID: "test-user-123", Expiry: expiry}
		if info.UserID != "test-user-123" {
			t.Errorf("UserID = %q, want %q", info.UserID, "test-user-123")
		}
		if info.Expiry != expiry {
			t.Errorf("Expiry = %v, want %v", info.Expiry, expiry)
		}
	})

	t.Run("empty UserID is allowed in struct", func(t *testing.T) {
		info := &SessionInfo{UserID: "", Expiry: time.Time{}}
		if info.UserID != "" {
			t.Errorf("UserID = %q, want empty string", info.UserID)
		}
	})
}

func TestValidateWithIntrospector_InputValidation(t *testing.T) {
	t.Run("empty token fails fast without calling introspect", func(t *testing.T) {
		called := false
		introspect := func(_ context.Context, _ string) (bool, string, time.Time, error) {
			called = true
			return true, "user1", time.Now().Add(1 * time.Hour), nil
		}
		_, err := validateWithIntrospector(context.Background(), introspect, "")
		if err == nil {
			t.Fatal("expected error for empty token")
		}
		if called {
			t.Error("introspect should not be called for empty token")
		}
	})
}

func TestValidateWithIntrospector_TokenVariations(t *testing.T) {
	futureExpiry := time.Now().Add(1 * time.Hour)

	t.Run("long token is passed through", func(t *testing.T) {
		longToken := strings.Repeat("a", 1000)
		called := false
		introspect := func(_ context.Context, token string) (bool, string, time.Time, error) {
			called = true
			if token != longToken {
				t.Errorf("token length = %d, want %d", len(token), len(longToken))
			}
			return true, "user1", futureExpiry, nil
		}
		_, err := validateWithIntrospector(context.Background(), introspect, longToken)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Error("introspect was not called")
		}
	})

	t.Run("jwt-like token is handled", func(t *testing.T) {
		jwtToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
		introspect := func(_ context.Context, token string) (bool, string, time.Time, error) {
			if token != jwtToken {
				t.Errorf("token mismatch")
			}
			return true, "1234567890", futureExpiry, nil
		}
		info, err := validateWithIntrospector(context.Background(), introspect, jwtToken)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.UserID != "1234567890" {
			t.Errorf("UserID = %q, want %q", info.UserID, "1234567890")
		}
	})

	t.Run("opaque token is handled", func(t *testing.T) {
		opaqueToken := "opaque-token-abc123xyz"
		introspect := func(_ context.Context, token string) (bool, string, time.Time, error) {
			if token != opaqueToken {
				t.Errorf("token mismatch")
			}
			return true, "opaque-user", futureExpiry, nil
		}
		info, err := validateWithIntrospector(context.Background(), introspect, opaqueToken)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.UserID != "opaque-user" {
			t.Errorf("UserID = %q, want %q", info.UserID, "opaque-user")
		}
	})
}

func TestValidateWithIntrospector_ContextBehavior(t *testing.T) {
	futureExpiry := time.Now().Add(1 * time.Hour)

	t.Run("context values are accessible in introspect", func(t *testing.T) {
		type ctxKey string
		key := ctxKey("test-key")
		ctx := context.WithValue(context.Background(), key, "test-value")

		introspect := func(ctx context.Context, _ string) (bool, string, time.Time, error) {
			val := ctx.Value(key)
			if val != "test-value" {
				t.Errorf("context value = %v, want %q", val, "test-value")
			}
			return true, "user1", futureExpiry, nil
		}
		_, err := validateWithIntrospector(ctx, introspect, "token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("introspect receives correct token from caller", func(t *testing.T) {
		expectedToken := "my-secure-token"
		introspect := func(_ context.Context, token string) (bool, string, time.Time, error) {
			if token != expectedToken {
				t.Errorf("received token = %q, want %q", token, expectedToken)
			}
			return true, "user1", futureExpiry, nil
		}
		_, err := validateWithIntrospector(context.Background(), introspect, expectedToken)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateWithIntrospector_ErrorWrapping(t *testing.T) {
	t.Run("generic error preserves message in wrap", func(t *testing.T) {
		originalErr := errors.New("original error message")
		introspect := func(_ context.Context, _ string) (bool, string, time.Time, error) {
			return false, "", time.Time{}, originalErr
		}
		_, err := validateWithIntrospector(context.Background(), introspect, "token")

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Should be able to unwrap to original error
		if !errors.Is(err, ErrAuthUnavailable) {
			t.Error("error should wrap ErrAuthUnavailable")
		}

		// Should contain original message
		if !strings.Contains(err.Error(), "original error message") {
			t.Errorf("error message %q should contain original error", err.Error())
		}
	})

	t.Run("context.Canceled can be unwrapped", func(t *testing.T) {
		introspect := func(_ context.Context, _ string) (bool, string, time.Time, error) {
			return false, "", time.Time{}, context.Canceled
		}
		_, err := validateWithIntrospector(context.Background(), introspect, "token")

		if !errors.Is(err, context.Canceled) {
			t.Error("error should wrap context.Canceled")
		}
		if !errors.Is(err, ErrContextCanceled) {
			t.Error("error should wrap ErrContextCanceled")
		}
	})

	t.Run("context.DeadlineExceeded can be unwrapped", func(t *testing.T) {
		introspect := func(_ context.Context, _ string) (bool, string, time.Time, error) {
			return false, "", time.Time{}, context.DeadlineExceeded
		}
		_, err := validateWithIntrospector(context.Background(), introspect, "token")

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Error("error should wrap context.DeadlineExceeded")
		}
		if !errors.Is(err, ErrAuthUnavailable) {
			t.Error("error should wrap ErrAuthUnavailable")
		}
	})
}

// TestIntrospectorType verifies the type exists and has expected structure.
func TestIntrospectorType(t *testing.T) {
	// Verify Introspector type exists and can be instantiated
	introspector := &Introspector{}
	_ = introspector // Type check only - verifies struct exists
}

// TestNewIntrospector validates constructor error handling.
func TestNewIntrospector(t *testing.T) {
	t.Run("empty domain returns error", func(t *testing.T) {
		_, err := NewIntrospector(context.Background(), "", "/path/to/key.json")
		if err == nil {
			t.Error("expected error for empty domain")
		}
		if !strings.Contains(err.Error(), "domain") {
			t.Errorf("error should mention domain, got: %v", err)
		}
	})

	t.Run("empty key path returns error", func(t *testing.T) {
		_, err := NewIntrospector(context.Background(), "example.zitadel.cloud", "")
		if err == nil {
			t.Error("expected error for empty key path")
		}
		if !strings.Contains(err.Error(), "key path") {
			t.Errorf("error should mention key path, got: %v", err)
		}
	})

	t.Run("non-existent key file returns error", func(t *testing.T) {
		_, err := NewIntrospector(context.Background(), "example.zitadel.cloud", "/non/existent/key.json")
		if err == nil {
			t.Error("expected error for non-existent key file")
		}
		if !strings.Contains(err.Error(), "key file") {
			t.Errorf("error should mention key file, got: %v", err)
		}
	})
}

// TestDefaultIntrospectionTimeout verifies the default timeout value.
func TestDefaultIntrospectionTimeout(t *testing.T) {
	if DefaultIntrospectionTimeout != 5*time.Second {
		t.Errorf("DefaultIntrospectionTimeout = %v, want 5s", DefaultIntrospectionTimeout)
	}
}

// TestIntrospectorBuilderMethods tests the fluent builder methods.
func TestIntrospectorBuilderMethods(t *testing.T) {
	t.Run("WithTimeout sets custom timeout", func(t *testing.T) {
		i := &Introspector{}
		result := i.WithTimeout(10 * time.Second)
		if i.timeout != 10*time.Second {
			t.Errorf("timeout = %v, want 10s", i.timeout)
		}
		if result != i {
			t.Error("WithTimeout should return same instance for chaining")
		}
	})

	t.Run("WithLogger sets logger", func(t *testing.T) {
		i := &Introspector{}
		log := zerolog.Nop()
		result := i.WithLogger(log)
		if result != i {
			t.Error("WithLogger should return same instance for chaining")
		}
	})

	t.Run("methods can be chained", func(t *testing.T) {
		i := &Introspector{}
		log := zerolog.Nop()
		result := i.WithLogger(log).WithTimeout(10 * time.Second)
		if result != i {
			t.Error("chaining should return same instance")
		}
		if i.timeout != 10*time.Second {
			t.Errorf("timeout = %v, want 10s", i.timeout)
		}
	})
}

// TestMustNewIntrospector tests the Must variant calls Fatal on error.
// Note: Cannot easily test Fatal behavior as it calls os.Exit(1).
// The MustNewIntrospector function is tested indirectly via NewIntrospector tests.

// TestValidateSession_WithMock tests the ValidateSession method with a mock.
// Note: This is a simplified test since we can't easily mock rs.ResourceServer.
// Full integration tests would require a real Zitadel instance or test server.
func TestValidateSession_EmptyToken(t *testing.T) {
	i := &Introspector{
		log:     zerolog.Nop(),
		timeout: DefaultIntrospectionTimeout,
	}

	t.Run("empty token returns ErrSessionInvalid", func(t *testing.T) {
		_, err := i.ValidateSession(context.Background(), "")
		if err != ErrSessionInvalid {
			t.Errorf("expected ErrSessionInvalid, got: %v", err)
		}
	})
}

// TestSessionInfoExpiry tests that expiry is properly extracted.
func TestSessionInfoExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	future := now.Add(1 * time.Hour)

	// Simulate the protobuf timestamp conversion
	expiryPB := timestamppb.New(future)

	info := &SessionInfo{
		UserID: "user123",
		Expiry: expiryPB.AsTime(),
	}

	// Compare using Equal to handle timezone differences
	if !info.Expiry.Equal(future) {
		t.Errorf("Expiry = %v, want %v", info.Expiry, future)
	}
}

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid https URL", raw: "https://example.zitadel.cloud", wantErr: false},
		{name: "valid http URL", raw: "http://localhost:8080", wantErr: false},
		{name: "valid https URL with path", raw: "https://example.zitadel.cloud/path", wantErr: false},
		{name: "missing scheme rejected", raw: "example.zitadel.cloud", wantErr: true},
		{name: "ftp scheme rejected", raw: "ftp://example.zitadel.cloud", wantErr: true},
		{name: "empty string rejected", raw: "", wantErr: true},
		{name: "whitespace-only rejected", raw: "   ", wantErr: true},
		{name: "javascript scheme rejected", raw: "javascript:alert(1)", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDomain(tt.raw)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateDomain(%q) expected error, got nil", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateDomain(%q) unexpected error: %v", tt.raw, err)
			}
			if err != nil && !strings.Contains(err.Error(), "invalid zitadel domain") {
				t.Errorf("error message should mention invalid zitadel domain, got: %v", err)
			}
		})
	}
}
