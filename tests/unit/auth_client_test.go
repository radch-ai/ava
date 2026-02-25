package unit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/auth"
)

func TestNewAuthClient(t *testing.T) {
	baseURL := "http://zetidal:8081"
	logger := struct{}{} // mock logger

	client := auth.NewAuthClient(baseURL, logger)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// Can't access private fields directly, but we can test through behavior
}

func TestNewAuthClientDefaults(t *testing.T) {
	// Test with empty baseURL (should use default)
	client := auth.NewAuthClient("", nil)
	if client == nil {
		t.Fatal("expected non-nil client with defaults")
	}

	// Test with valid baseURL
	client2 := auth.NewAuthClient("http://custom:8080", nil)
	if client2 == nil {
		t.Fatal("expected non-nil client with custom URL")
	}
}

func TestAuthClientValidateSession_Success(t *testing.T) {
	// Create a test server that simulates successful auth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify session cookie
		cookie, err := r.Cookie("session")
		if err != nil {
			t.Errorf("expected session cookie, got error: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if cookie.Value != "valid-session-token" {
			t.Errorf("expected session cookie value 'valid-session-token', got '%s'", cookie.Value)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Return successful response
		expiresAt := time.Now().Add(1 * time.Hour).Unix()
		w.Header().Set("Content-Type", "application/json")
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    "new-session-token",
			Path:     "/",
			MaxAge:   3600,
			HttpOnly: true,
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authenticated": true,
			"user_id":       "user-123",
			"expires_at":    expiresAt,
		})
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	sessionInfo, err := client.ValidateSession(ctx, "valid-session-token")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if sessionInfo.UserID != "user-123" {
		t.Errorf("expected user_id 'user-123', got '%s'", sessionInfo.UserID)
	}

	if sessionInfo.SetCookie == nil {
		t.Error("expected SetCookie to be set for sliding expiration")
	} else if sessionInfo.SetCookie.Value != "new-session-token" {
		t.Errorf("expected new session token, got '%s'", sessionInfo.SetCookie.Value)
	}

	if sessionInfo.ExpiresAt.IsZero() {
		t.Error("expected ExpiresAt to be set")
	}
}

func TestAuthClientValidateSession_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	_, err := client.ValidateSession(ctx, "invalid-token")

	if !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("expected ErrSessionInvalid, got: %v", err)
	}
}

func TestAuthClientValidateSession_Timeout(t *testing.T) {
	// Create a server that delays longer than the context deadline
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	// Use context with deadline since client no longer has default timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.ValidateSession(ctx, "any-token")

	if !errors.Is(err, auth.ErrZetidalUnavailable) {
		t.Errorf("expected ErrZetidalUnavailable for timeout, got: %v", err)
	}
}

func TestAuthClientValidateSession_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	_, err := client.ValidateSession(ctx, "any-token")

	if !errors.Is(err, auth.ErrContextCanceled) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation error, got: %v", err)
	}
}

func TestAuthClientValidateSession_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	_, err := client.ValidateSession(ctx, "valid-session-token")

	if !errors.Is(err, auth.ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got: %v", err)
	}
}

func TestAuthClientValidateSession_MissingUserID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authenticated": true,
			"user_id":       "", // missing user_id
		})
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	_, err := client.ValidateSession(ctx, "valid-session-token")

	if !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("expected ErrSessionInvalid when user_id missing, got: %v", err)
	}
}

func TestAuthClientValidateSession_NonAuthenticatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authenticated": false,
			"user_id":       "",
		})
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	_, err := client.ValidateSession(ctx, "valid-session-token")

	if !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("expected ErrSessionInvalid when authenticated=false, got: %v", err)
	}
}

func TestAuthClientValidateSession_ServiceUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	_, err := client.ValidateSession(ctx, "any-token")

	if !errors.Is(err, auth.ErrZetidalUnavailable) {
		t.Errorf("expected ErrAuthUnavailable for 503, got: %v", err)
	}
}

func TestAuthClientValidateSession_SuccessWithoutSetCookie(t *testing.T) {
	// Test successful validation without sliding expiration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if cookie.Value != "valid-session" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Return response WITHOUT Set-Cookie header
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authenticated": true,
			"user_id":       "user-456",
			// No expires_at
		})
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	sessionInfo, err := client.ValidateSession(ctx, "valid-session")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if sessionInfo.UserID != "user-456" {
		t.Errorf("expected user_id 'user-456', got '%s'", sessionInfo.UserID)
	}

	if sessionInfo.SetCookie != nil {
		t.Error("expected SetCookie to be nil when no cookie header returned")
	}

	if !sessionInfo.ExpiresAt.IsZero() {
		t.Error("expected ExpiresAt to be zero when not provided")
	}
}

func TestAuthClientValidateSession_NetworkError(t *testing.T) {
	// Create a client with an invalid URL that will cause a network error
	client := auth.NewAuthClient("http://invalid-host-that-does-not-exist:9999", nil)
	ctx := context.Background()

	_, err := client.ValidateSession(ctx, "any-token")

	if !errors.Is(err, auth.ErrZetidalUnavailable) {
		t.Errorf("expected ErrAuthUnavailable for network error, got: %v", err)
	}
}

func TestAuthClientValidateSession_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418
	}))
	defer server.Close()

	client := auth.NewAuthClient(server.URL, nil)
	ctx := context.Background()

	_, err := client.ValidateSession(ctx, "any-token")

	if !errors.Is(err, auth.ErrZetidalUnavailable) {
		t.Errorf("expected ErrAuthUnavailable for unexpected status, got: %v", err)
	}

	if !strings.Contains(err.Error(), "418") {
		t.Errorf("expected error to mention status code 418, got: %v", err)
	}
}
